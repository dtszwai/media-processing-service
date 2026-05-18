// Package main runs the media-service Connect HTTP API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	authapp "github.com/dtszwai/media-processing-service/backend/internal/app/auth"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	redisratelimit "github.com/dtszwai/media-processing-service/backend/internal/infra/ratelimit"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/middleware"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

const serviceName = "media-service-api"

var buildVersion = "dev"

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

func main() {
	rt := runtime.Init(serviceName)
	logger := rt.Logger
	appCfg := rt.Cfg
	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if err := validateJWTSecretForAuth(appCfg.API.AuthEnforcement, jwtSecret); err != nil {
		logger.Error("auth enforcement requires JWT_SECRET with at least 32 bytes of entropy", "err", err)
		os.Exit(1)
	}

	bootCtx, bootCancel := context.WithTimeout(context.Background(), 60*time.Second)
	awsResources, bootErr := bootstrap.Wire(bootCtx, appCfg)
	bootCancel()
	if bootErr != nil {
		logger.Warn("aws bootstrap failed; api will continue serving health only", "err", bootErr)
	}

	ctx, stop := runtime.SignalCtx()
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Service: serviceName, Version: buildVersion})
	})
	mux.HandleFunc("GET /readyz", readyzHandler(bootErr))

	if awsResources != nil {
		registerConnectAPIs(mux, awsResources, jwtSecret)
		logger.Info("connect apis registered")
		// Outbox relays live in the standalone outbox-relay binary — running
		// them here would tie media + cleanup + generation stream draining to
		// the API process's health. The compose generation-worker is the only
		// supported drainer for the per-tier generation queues locally.
		logger.Info("media api registered", "topic", awsResources.MediaTopicARN, "bucket", awsResources.Bucket, "table", awsResources.Table)
	}

	authCfg := middleware.AuthConfig{
		JWTSecret:   jwtSecret,
		Enforcement: appCfg.API.AuthEnforcement,
		PublicPaths: []string{
			"/mediaservice.auth.v1.AuthService/Login",
			"/mediaservice.auth.v1.AuthService/Register",
			"/mediaservice.auth.v1.AuthService/Refresh",
			"/healthz",
			"/readyz",
		},
	}
	if awsResources != nil && awsResources.APIKeys != nil {
		authCfg.APIKeyAuth = apiKeyLookup{keys: awsResources.APIKeys}
	}
	rlOpts := middleware.RateLimitOptions{
		RequestsPerMinute: 600,
		Burst:             60,
	}
	if redisAddr := os.Getenv("REDIS_ADDR"); redisAddr != "" {
		redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
		defer redisClient.Close()
		rlOpts.Store = redisratelimit.NewRedisStore(redisClient)
		rlOpts.FailOpenOnStoreError = true
		logger.Info("redis rate limiter configured", "addr", redisAddr)
	}
	corsCfg := middleware.CORSConfigFromEnv(appCfg.API.CORSOrigins)

	// Chain order (outer → inner): CORS, request-id, authentication,
	// tenant-scope, suspension, rate-limit, handler. Rate-limit sits AFTER auth so
	// authenticated traffic buckets per tenant + API key — not per source IP —
	// and AFTER tenant-scope so a rejected cross-tenant request doesn't
	// consume the victim tenant's budget.
	root := middleware.CORSMiddleware(corsCfg)(
		withRequestID(
			middleware.AuthMiddleware(authCfg)(
				middleware.TenantScopeFromPath(
					middleware.TenantSuspensionMiddleware(tenantSuspensionCheckerFromResources(awsResources))(
						middleware.RateLimitMiddleware(rlOpts)(
							middleware.QuotaMeterMiddleware(quotaMeterFromResources(awsResources))(mux),
						),
					),
				),
			),
		),
	)

	srv := &http.Server{
		Addr:              appCfg.API.Addr,
		Handler:           otelhttp.NewHandler(root, "media.api"),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		logger.Info("api listening", "addr", appCfg.API.Addr, "version", buildVersion, "auth_enforcement", authCfg.Enforcement)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server failed", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown initiated")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "err", err)
	}
	_ = rt.Shutdown(shutdownCtx)
	logger.Info("shutdown complete")
}

func validateJWTSecretForAuth(enforcement bool, secret []byte) error {
	if !enforcement {
		return nil
	}
	return jwtauth.ValidateHS256Secret(secret)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func readyzHandler(bootErr error) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if bootErr != nil {
			writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "not_ready", Service: serviceName, Version: buildVersion})
			return
		}
		writeJSON(w, http.StatusOK, healthResponse{Status: "ready", Service: serviceName, Version: buildVersion})
	}
}

func withRequestID(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rid := r.Header.Get("X-Request-Id")
		if rid == "" {
			rid = randid.New()
		}
		w.Header().Set("X-Request-Id", rid)
		h.ServeHTTP(w, r)
	})
}

func quotaMeterFromResources(a *bootstrap.AWS) middleware.RequestQuotaMeter {
	if a == nil {
		return nil
	}
	return a.QuotaMeter
}

func tenantSuspensionCheckerFromResources(a *bootstrap.AWS) middleware.TenantSuspensionChecker {
	if a == nil {
		return nil
	}
	return a.TenantAdmin
}

// apiKeyLookup adapts the DDB-backed APIKeys repo to the
// middleware.APIKeyAuthenticator interface. Kept in cmd/api so the transport
// layer doesn't need to import infra adapters.
type apiKeyLookup struct {
	keys *authapp.DDBAPIKeys
}

func (a apiKeyLookup) Authenticate(ctx context.Context, raw string) (jwtauth.Principal, error) {
	k, err := a.keys.LookupByRaw(ctx, raw)
	if err != nil {
		return jwtauth.Principal{}, jwtauth.ErrUnauthenticated
	}
	return jwtauth.Principal{
		TenantID: k.TenantID,
		UserID:   k.UserID,
		APIKeyID: k.ID,
		// API keys grant the same baseline role as a logged-in user; admin
		// promotion is JWT-only on purpose so machine credentials can never
		// self-elevate.
		Roles: []jwtauth.Role{jwtauth.RoleUser},
	}, nil
}

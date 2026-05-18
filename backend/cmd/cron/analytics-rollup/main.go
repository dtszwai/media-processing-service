// Package main runs the analytics top-N rollup. Defaults to UTC today;
// override via MSG_ROLLUP_DATE=YYYYMMDD or EventBridge payload.
//
// Dual-mode: AWS_LAMBDA_FUNCTION_NAME set → cron Lambda. Otherwise one-shot.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	"github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
)

const serviceName = "analytics-rollup"

// appCfg is loaded once on cold start so warm Lambda invocations skip the env
// reload. Env values backing the config don't change between invocations.
var appCfg app.Config

type rollupEvent struct {
	Type string `json:"type,omitempty"`
	Date string `json:"date,omitempty"`
}

func main() {
	rt := runtime.Init(serviceName)
	defer rt.FlushTelemetry()
	appCfg = rt.Cfg

	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(handleLambda)
		return
	}
	if err := runOnce(os.Getenv("MSG_ROLLUP_DATE")); err != nil {
		rt.Logger.Error("rollup", "service", serviceName, "err", err)
		os.Exit(2)
	}
}

func handleLambda(ctx context.Context, evt rollupEvent) (any, error) {
	slog.InfoContext(ctx, "invoked", "service", serviceName, "type", evt.Type, "date", evt.Date)
	res, err := bootstrap.Wire(ctx, appCfg)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	day, err := parseDate(evt.Date)
	if err != nil {
		return nil, err
	}
	if err := analytics.NewRollupService(res.KV).Run(ctx, day); err != nil {
		return nil, fmt.Errorf("rollup: %w", err)
	}
	return map[string]string{"status": "ok", "date": day.Format("20060102")}, nil
}

func runOnce(dateStr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := bootstrap.Wire(ctx, appCfg)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	day, err := parseDate(dateStr)
	if err != nil {
		return err
	}
	return analytics.NewRollupService(res.KV).Run(ctx, day)
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Now().UTC(), nil
	}
	t, err := time.ParseInLocation("20060102", s, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid date %q (want YYYYMMDD): %w", s, err)
	}
	return t, nil
}

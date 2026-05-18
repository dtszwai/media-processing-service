// Package auth adapts app/auth onto the Connect transport surface.
package auth

import (
	"context"
	"errors"
	"strings"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	authapp "github.com/dtszwai/media-processing-service/backend/internal/app/auth"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	authpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/auth/v1"
)

const scopeAPIKeysManageTenant = "api_keys:manage:tenant"

type Server struct {
	svc *authapp.Service
}

func NewServer(svc *authapp.Service) *Server {
	return &Server{svc: svc}
}

func (s *Server) Register(ctx context.Context, req *connect.Request[authpb.RegisterRequest]) (*connect.Response[authpb.RegisterResponse], error) {
	sess, err := s.svc.Register(ctx, req.Msg.GetEmail(), req.Msg.GetPassword())
	if err != nil {
		switch {
		case errors.Is(err, authapp.ErrPasswordTooShort):
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("password must be at least 8 characters"))
		case errors.Is(err, authapp.ErrInvalidCredentials):
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("email and password required"))
		case errors.Is(err, authapp.ErrEmailTaken):
			return nil, connect.NewError(connect.CodeAlreadyExists, errors.New("email already registered"))
		default:
			return nil, connect.NewError(connect.CodeInternal, errors.New("register: "+err.Error()))
		}
	}
	return connect.NewResponse(&authpb.RegisterResponse{
		Token:        sess.AccessToken,
		RefreshToken: sess.RefreshToken,
		TenantId:     sess.TenantID,
		UserId:       sess.UserID,
		ExpiresIn:    sess.ExpiresIn,
	}), nil
}

func (s *Server) Login(ctx context.Context, req *connect.Request[authpb.LoginRequest]) (*connect.Response[authpb.LoginResponse], error) {
	sess, err := s.svc.Login(ctx, req.Msg.GetEmail(), req.Msg.GetPassword())
	if err != nil {
		if errors.Is(err, authapp.ErrInvalidCredentials) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("invalid credentials"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("login: "+err.Error()))
	}
	return connect.NewResponse(&authpb.LoginResponse{
		Token:        sess.AccessToken,
		RefreshToken: sess.RefreshToken,
		TenantId:     sess.TenantID,
		UserId:       sess.UserID,
		ExpiresIn:    sess.ExpiresIn,
	}), nil
}

func (s *Server) Refresh(ctx context.Context, req *connect.Request[authpb.RefreshRequest]) (*connect.Response[authpb.RefreshResponse], error) {
	sess, err := s.svc.Refresh(ctx, req.Msg.GetRefreshToken())
	if err != nil {
		if errors.Is(err, authapp.ErrInvalidCredentials) {
			return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("refresh token invalid"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("refresh: "+err.Error()))
	}
	return connect.NewResponse(&authpb.RefreshResponse{
		Token:        sess.AccessToken,
		RefreshToken: sess.RefreshToken,
		TenantId:     sess.TenantID,
		UserId:       sess.UserID,
		ExpiresIn:    sess.ExpiresIn,
	}), nil
}

func (s *Server) GetMe(ctx context.Context, _ *connect.Request[authpb.GetMeRequest]) (*connect.Response[authpb.GetMeResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	u, err := s.svc.GetUser(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, authapp.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("load user: "+err.Error()))
	}
	roles := make([]string, len(u.Roles))
	for i, r := range u.Roles {
		roles[i] = string(r)
	}
	return connect.NewResponse(&authpb.GetMeResponse{
		TenantId: u.TenantID,
		UserId:   u.ID,
		Email:    u.Email,
		Roles:    roles,
	}), nil
}

func (s *Server) CreateAPIKey(ctx context.Context, req *connect.Request[authpb.CreateAPIKeyRequest]) (*connect.Response[authpb.CreateAPIKeyResponse], error) {
	principal, err := requireAPIKeyManagement(ctx, false)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetName()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name required"))
	}
	rec, raw, err := s.svc.CreateAPIKey(ctx, principal.TenantID, principal.UserID, req.Msg.GetName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("create key: "+err.Error()))
	}
	return connect.NewResponse(&authpb.CreateAPIKeyResponse{
		Key: &authpb.APIKey{
			KeyId:     rec.ID,
			RawKey:    raw,
			Name:      rec.Name,
			CreatedAt: timestamppb.New(rec.CreatedAt),
		},
	}), nil
}

func (s *Server) ListAPIKeys(ctx context.Context, _ *connect.Request[authpb.ListAPIKeysRequest]) (*connect.Response[authpb.ListAPIKeysResponse], error) {
	principal, err := requireAPIKeyManagement(ctx, true)
	if err != nil {
		return nil, err
	}
	recs, err := s.svc.ListAPIKeys(ctx, principal.TenantID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("list keys: "+err.Error()))
	}
	out := make([]*authpb.APIKey, len(recs))
	for i, r := range recs {
		out[i] = &authpb.APIKey{
			KeyId:     r.ID,
			Name:      r.Name,
			CreatedAt: timestamppb.New(r.CreatedAt),
		}
	}
	return connect.NewResponse(&authpb.ListAPIKeysResponse{Keys: out}), nil
}

func (s *Server) DeleteAPIKey(ctx context.Context, req *connect.Request[authpb.DeleteAPIKeyRequest]) (*connect.Response[authpb.DeleteAPIKeyResponse], error) {
	principal, err := requireAPIKeyManagement(ctx, true)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetKeyId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("key_id required"))
	}
	if err := s.svc.DeleteAPIKeyAsUser(ctx, principal.TenantID, principal.UserID, req.Msg.GetKeyId()); err != nil {
		if errors.Is(err, authapp.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("key not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, errors.New("delete key: "+err.Error()))
	}
	return connect.NewResponse(&authpb.DeleteAPIKeyResponse{}), nil
}

func requireAPIKeyManagement(ctx context.Context, allowAPIKeyPrincipal bool) (jwtauth.Principal, error) {
	p, err := jwtauth.FromContext(ctx)
	if err != nil || p.TenantID == "" {
		return jwtauth.Principal{}, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	if !allowAPIKeyPrincipal && p.APIKeyID != "" {
		return jwtauth.Principal{}, connect.NewError(connect.CodePermissionDenied, errors.New("api-key principals cannot create api keys"))
	}
	if p.HasRole(jwtauth.RoleAdmin) || p.HasPlatformAdmin() || p.HasScope(scopeAPIKeysManageTenant) {
		return p, nil
	}
	return jwtauth.Principal{}, connect.NewError(connect.CodePermissionDenied, errors.New("api-key management scope required"))
}

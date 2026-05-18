package admin

import (
	"errors"
	"strings"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func tenantIDForAdminRequest(claims *jwtauth.Claims, requested string, required bool) (string, error) {
	if claims == nil {
		return "", connect.NewError(connect.CodeUnauthenticated, errors.New("missing claims"))
	}
	requested = strings.TrimSpace(requested)
	if requested == "" {
		if required || claims.TenantID == "" {
			return "", connect.NewError(connect.CodeInvalidArgument, errors.New("tenant_id required"))
		}
		return claims.TenantID, nil
	}
	if !claims.HasPlatformAdmin() && requested != claims.TenantID {
		return "", connect.NewError(connect.CodePermissionDenied, errors.New("cross-tenant admin operation denied"))
	}
	return requested, nil
}

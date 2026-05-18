package media

import (
	"context"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
)

func (s *Service) GetVisible(ctx context.Context, p Principal, mediaID string) (*media.Media, error) {
	m, err := s.Repo.GetMedia(ctx, p.TenantID, mediaID)
	if err != nil {
		return nil, err
	}
	if err := authorizeRead(p, *m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) GetMutable(ctx context.Context, p Principal, mediaID string) (*media.Media, error) {
	m, err := s.Repo.GetMedia(ctx, p.TenantID, mediaID)
	if err != nil {
		return nil, err
	}
	if err := authorizeMutate(p, *m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *Service) ListAssetsVisible(ctx context.Context, p Principal, mediaID string) ([]media.Asset, error) {
	if _, err := s.GetVisible(ctx, p, mediaID); err != nil {
		return nil, err
	}
	return s.Repo.ListAssets(ctx, p.TenantID, mediaID)
}

func authorizeRead(p Principal, m media.Media) error {
	return authorize(p, m, accessRead)
}

func authorizeMutate(p Principal, m media.Media) error {
	return authorize(p, m, accessMutate)
}

type accessMode int

const (
	accessRead accessMode = iota
	accessMutate
)

func authorize(p Principal, m media.Media, mode accessMode) error {
	if p.TenantID == "" || p.TenantID != m.TenantID {
		return ErrForbidden
	}
	grants := []string{"role:ADMIN"}
	switch mode {
	case accessRead:
		grants = append(grants, "scope:media:read:any", "scope:media:read:tenant")
	case accessMutate:
		grants = append(grants, "scope:media:write:any", "scope:media:write:tenant")
	}
	if principalSatisfies(p, grants...) {
		return nil
	}
	if mode == accessRead && resolveVisibility(m) == media.VisibilityTenantShared {
		return nil
	}
	if p.UserID != "" && p.UserID == m.OwnerUserID {
		return nil
	}
	return ErrForbidden
}

// resolveVisibility folds the historical "" → owner/tenant default in one
// place so the access decision and any caller computing the same default
// agree.
func resolveVisibility(m media.Media) media.Visibility {
	if m.Visibility != "" {
		return m.Visibility
	}
	if m.OwnerUserID != "" {
		return media.VisibilityOwnerPrivate
	}
	return media.VisibilityTenantShared
}

// principalSatisfies tests a mixed list of "role:X" or "scope:Y" grants
// against the Principal. Any single match wins.
func principalSatisfies(p Principal, anyOf ...string) bool {
	for _, item := range anyOf {
		switch {
		case strings.HasPrefix(item, "role:"):
			if principalHasRole(p, item[len("role:"):]) {
				return true
			}
		case strings.HasPrefix(item, "scope:"):
			if principalHasScope(p, item[len("scope:"):]) {
				return true
			}
		}
	}
	return false
}

func principalHasRole(p Principal, role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func principalHasScope(p Principal, scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

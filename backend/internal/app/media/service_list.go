package media

import "context"

// listLimitMin is the default page size when opts.Limit is not set.
const listLimitMin = 50

// listLimitMax is the maximum page size; larger values are clamped.
const listLimitMax = 100

// ListByTenant returns a paginated tenant media feed, newest first. Limit
// clamping is done here so the Repository implementation receives a
// well-bounded value regardless of what the caller supplies.
func (s *Service) ListByTenant(ctx context.Context, tenantID string, opts ListOpts) (ListPage, error) {
	if opts.Limit <= 0 {
		opts.Limit = listLimitMin
	}
	if opts.Limit > listLimitMax {
		opts.Limit = listLimitMax
	}
	return s.Repo.ListByTenant(ctx, tenantID, opts)
}

func (s *Service) ListByPrincipal(ctx context.Context, p Principal, opts ListOpts) (ListPage, error) {
	page, err := s.ListByTenant(ctx, p.TenantID, opts)
	if err != nil {
		return ListPage{}, err
	}
	filtered := page.Items[:0]
	for _, item := range page.Items {
		if authorizeRead(p, item) == nil {
			filtered = append(filtered, item)
		}
	}
	page.Items = filtered
	return page, nil
}

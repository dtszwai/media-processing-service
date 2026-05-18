package analytics

import "context"

// Reader abstracts DDB reads needed by the Connect handlers so handlers are
// testable without a real DynamoDB client.
type Reader interface {
	// GetTopEntries returns the pre-computed TOP entries for (tenantID, period).
	// period is one of the Period constants (DAILY, WEEKLY, MONTHLY, ROLLING_12M).
	GetTopEntries(ctx context.Context, tenantID string, period Period) ([]TopEntry, error)

	// SumViewCountRange sums the 16 VIEW counter shards for (tenantID, mediaID)
	// across the given YYYYMMDD day strings.
	SumViewCountRange(ctx context.Context, tenantID, mediaID string, days []string) (int64, error)

	// SumDownloadCountRange sums the 16 DOWNLOAD counter shards for
	// (tenantID, mediaID) across the given YYYYMMDD day strings.
	SumDownloadCountRange(ctx context.Context, tenantID, mediaID string, days []string) (int64, error)

	// SumDownloadsByFormat returns total download count and a per-format
	// breakdown by summing DOWNLOAD shards for the tenant across the given days.
	// mediaIDs limits the scan to known candidates; pass nil to skip.
	SumDownloadsByFormat(ctx context.Context, tenantID string, mediaIDs []string, days []string) (total int64, byFormat map[string]int64, err error)
}

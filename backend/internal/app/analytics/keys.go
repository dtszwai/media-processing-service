package analytics

// TopPK partitions a per-tenant top-media result row. One row per
// (tenant, period) — overwritten on each rollup run.
func TopPK(tenantID, period string) string {
	return "TOP#" + tenantID + "#" + period
}

// TopSK is the fixed sort key. Each partition holds exactly one result row.
const TopSK = "ENTRIES"

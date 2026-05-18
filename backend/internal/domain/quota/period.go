package quota

import "time"

// PeriodDaily returns yyyyMMdd in UTC. Provided as a helper so call sites
// stay consistent — drifting the period format silently fragments a
// reservoir into one row per accidentally-different period string.
func PeriodDaily(t time.Time) string { return t.UTC().Format("20060102") }

// PeriodMonthly returns yyyyMM in UTC.
func PeriodMonthly(t time.Time) string { return t.UTC().Format("200601") }

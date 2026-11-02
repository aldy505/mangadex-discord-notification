package scheduler

import "time"

// advanceWatermark returns the subscription watermark moved forward over the
// items delivered so far, never past failedAt. Items sharing the failed item's
// timestamp are left behind it so a retry cannot lose them: a repeat is better
// than a chapter that never arrives.
func advanceWatermark(watermark int64, delivered []int64, failedAt *time.Time) int64 {
	newest := watermark

	for _, timestamp := range delivered {
		if failedAt != nil && timestamp >= failedAt.UnixMilli() {
			continue
		}
		if timestamp > newest {
			newest = timestamp
		}
	}

	return newest
}

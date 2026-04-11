// Package reports — exported time-range parser for use by other packages.
package reports

import (
	"net/http"
	"time"
)

// ParseTimeRange is exported so other packages (screenshots, silence) can reuse it.
func ParseTimeRange(r *http.Request) (time.Time, time.Time) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -defaultLookbackDays)

	if s := r.URL.Query().Get("from"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			from = t
		}
	}
	if s := r.URL.Query().Get("to"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			to = t
		}
	}
	return from, to
}

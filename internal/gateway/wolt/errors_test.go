package wolt

import (
	"net/http"
	"testing"
	"time"
)

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 5, 21, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"absent header", "", 0},
		{"seconds form", "12", 12 * time.Second},
		{"zero seconds", "0", 0},
		{"negative seconds", "-5", 0},
		{"http-date future", now.Add(30 * time.Second).UTC().Format(http.TimeFormat), 30 * time.Second},
		{"http-date past", now.Add(-time.Minute).UTC().Format(http.TimeFormat), 0},
		{"garbage", "soon-ish", 0},
		{"whitespace", "  15  ", 15 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.Header{}
			if c.header != "" {
				h.Set("Retry-After", c.header)
			}
			got := parseRetryAfter(h, now)
			if c.name == "http-date future" {
				// HTTP-date has 1-second resolution; accept ±1s drift.
				delta := got - c.want
				if delta < -time.Second || delta > time.Second {
					t.Fatalf("want ~%s, got %s", c.want, got)
				}
				return
			}
			if got != c.want {
				t.Fatalf("want %s, got %s", c.want, got)
			}
		})
	}
}

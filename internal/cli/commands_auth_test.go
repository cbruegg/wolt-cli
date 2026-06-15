package cli

import (
	"strings"
	"testing"
	"time"
)

// now is the reference instant used across these tests. paid_until_date and
// end_date values below are offset from it so "active" is unambiguous.
var woltPlusNow = time.Date(2026, time.June, 15, 12, 0, 0, 0, time.UTC)

func unixOffset(d time.Duration) float64 {
	return float64(woltPlusNow.Add(d).Unix())
}

func TestWoltPlusActiveWithFuturePaidUntil(t *testing.T) {
	payload := map[string]any{
		"subscriptions": []any{
			map[string]any{
				"plan":            map[string]any{"country": "FIN", "name": "Wolt+"},
				"end_date":        nil,
				"paid_until_date": unixOffset(30 * 24 * time.Hour),
			},
		},
	}

	active, known := woltPlusActive(payload, woltPlusNow)
	if !known {
		t.Fatal("expected wolt plus status to be known")
	}
	if !active {
		t.Fatalf("expected active=true for a subscription paid into the future, got %v", active)
	}
}

func TestWoltPlusActiveWithExpiredSubscription(t *testing.T) {
	payload := map[string]any{
		"subscriptions": []any{
			map[string]any{
				"plan":            map[string]any{"country": "POL", "name": "Wolt+"},
				"end_date":        unixOffset(-60 * 24 * time.Hour),
				"paid_until_date": unixOffset(-60 * 24 * time.Hour),
			},
		},
	}

	active, known := woltPlusActive(payload, woltPlusNow)
	if !known {
		t.Fatal("expected wolt plus status to be known")
	}
	if active {
		t.Fatalf("expected active=false for an expired subscription, got %v", active)
	}
}

func TestWoltPlusActiveWithMixedSubscriptions(t *testing.T) {
	// Mirrors the real account observed via Chrome DevTools: one expired (POL)
	// plus one active (FIN) subscription. Any active entry wins.
	payload := map[string]any{
		"subscriptions": []any{
			map[string]any{
				"plan":            map[string]any{"country": "POL"},
				"end_date":        unixOffset(-60 * 24 * time.Hour),
				"paid_until_date": unixOffset(-60 * 24 * time.Hour),
			},
			map[string]any{
				"plan":            map[string]any{"country": "FIN"},
				"end_date":        nil,
				"paid_until_date": unixOffset(300 * 24 * time.Hour),
			},
		},
	}

	active, known := woltPlusActive(payload, woltPlusNow)
	if !known || !active {
		t.Fatalf("expected known=true active=true for mixed subscriptions, got known=%v active=%v", known, active)
	}
}

func TestWoltPlusActiveWithOpenEndedSubscription(t *testing.T) {
	// No paid_until_date and a null end_date: an open-ended plan that has started.
	payload := map[string]any{
		"subscriptions": []any{
			map[string]any{
				"plan":       map[string]any{"country": "FIN"},
				"start_date": unixOffset(-10 * 24 * time.Hour),
				"end_date":   nil,
			},
		},
	}

	active, known := woltPlusActive(payload, woltPlusNow)
	if !known || !active {
		t.Fatalf("expected known=true active=true for open-ended subscription, got known=%v active=%v", known, active)
	}
}

func TestWoltPlusActiveWithEmptyListIsKnownFalse(t *testing.T) {
	active, known := woltPlusActive(map[string]any{"subscriptions": []any{}}, woltPlusNow)
	if !known {
		t.Fatal("expected an empty subscriptions list to be a definitive 'not subscribed'")
	}
	if active {
		t.Fatalf("expected active=false for empty subscriptions, got %v", active)
	}
}

func TestWoltPlusActiveWithoutSubscriptionsKeyIsUnknown(t *testing.T) {
	if _, known := woltPlusActive(map[string]any{}, woltPlusNow); known {
		t.Fatal("expected missing subscriptions key to yield unknown status")
	}
	if _, known := woltPlusActive(nil, woltPlusNow); known {
		t.Fatal("expected nil payload to yield unknown status")
	}
}

func TestBuildAuthStatusTableIncludesWoltPlusSubscriber(t *testing.T) {
	table := buildAuthStatusTable(map[string]any{
		"authenticated":        true,
		"wolt_plus_subscriber": true,
		"user_id":              "user-1",
		"country":              "FIN",
		"session_expires_at":   "2026-02-20T12:00:00Z",
	})

	if !strings.Contains(normalizeTableWhitespace(table), "Wolt+ subscriber yes") {
		t.Fatalf("expected table to include Wolt+ row, got:\n%s", table)
	}
}

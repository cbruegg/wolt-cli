package statssync

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
	_ "modernc.org/sqlite"
)

func TestSyncCreatesSchemaAndInsertsOrders(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")

	res, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond, // keep tests fast
		Now:       fixedClock(),
	})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	if res.OrdersScanned != 2 {
		t.Fatalf("expected 2 orders scanned, got %d", res.OrdersScanned)
	}
	if res.DetailsFetched != 2 {
		t.Fatalf("expected 2 details fetched, got %d", res.DetailsFetched)
	}
	if res.InsertedOrders != 2 {
		t.Fatalf("expected 2 inserted, got %d", res.InsertedOrders)
	}
	if res.UpdatedOrders != 0 {
		t.Fatalf("expected 0 updated on cold start, got %d", res.UpdatedOrders)
	}
	if res.Mode != "full" {
		t.Fatalf("expected mode=full on cold start, got %q", res.Mode)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open verify db: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Schema-level invariants.
	for _, table := range []string{"users", "sync_state", "sync_runs", "order_catalog", "orders", "order_items", "order_item_option_values", "order_payments"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatalf("schema lookup %s: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("expected table %s to exist", table)
		}
	}

	// User row exists and is keyed by slug of email.
	var (
		userID    string
		userLabel string
	)
	if err := db.QueryRow(`SELECT id, label FROM users`).Scan(&userID, &userLabel); err != nil {
		t.Fatalf("read user: %v", err)
	}
	if userID != "user-example-com" {
		t.Fatalf("expected slugged user id, got %q", userID)
	}
	if userLabel != "user" {
		t.Fatalf("expected user label 'user', got %q", userLabel)
	}

	// orders rows
	var ordersCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM orders WHERE user_id=?`, userID).Scan(&ordersCount); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	if ordersCount != 2 {
		t.Fatalf("expected 2 orders, got %d", ordersCount)
	}

	// items + payments
	var itemsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM order_items WHERE user_id=?`, userID).Scan(&itemsCount); err != nil {
		t.Fatalf("count items: %v", err)
	}
	if itemsCount != 3 {
		t.Fatalf("expected 3 item rows (1+2), got %d", itemsCount)
	}

	// derived fields: order_local_date for the second order
	var date string
	if err := db.QueryRow(`SELECT order_local_date FROM orders WHERE purchase_id='p2'`).Scan(&date); err != nil {
		t.Fatalf("read date p2: %v", err)
	}
	if date != "2026-05-21" {
		t.Fatalf("expected date 2026-05-21, got %q", date)
	}

	// sync_runs has exactly one row capturing this run
	var runs int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sync_runs`).Scan(&runs); err != nil {
		t.Fatalf("count sync_runs: %v", err)
	}
	if runs != 1 {
		t.Fatalf("expected 1 sync_runs row, got %d", runs)
	}
}

func TestSyncIncrementalSkipsKnownOrders(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")

	if _, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
	}); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	client.resetCounts()
	res, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
	})
	if err != nil {
		t.Fatalf("incremental Sync: %v", err)
	}
	if res.Mode != "incremental" {
		t.Fatalf("expected mode=incremental, got %q", res.Mode)
	}
	if res.DetailsFetched != 0 {
		t.Fatalf("expected 0 details on warm rerun, got %d", res.DetailsFetched)
	}
	// One page fetched so we can detect the known purchase; no further calls.
	if client.listCalls != 1 {
		t.Fatalf("expected 1 list call on warm rerun, got %d", client.listCalls)
	}
	if client.purchaseCalls != 0 {
		t.Fatalf("expected 0 purchase calls on warm rerun, got %d", client.purchaseCalls)
	}
	if res.StopReason != "known_purchase" {
		t.Fatalf("expected stop reason known_purchase, got %q", res.StopReason)
	}
}

func TestSyncForceFullRefetchesDetails(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")

	if _, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
	}); err != nil {
		t.Fatalf("first Sync: %v", err)
	}

	client.resetCounts()
	res, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		ForceFull: true,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
	})
	if err != nil {
		t.Fatalf("force-full Sync: %v", err)
	}
	if res.Mode != "full" {
		t.Fatalf("expected mode=full, got %q", res.Mode)
	}
	if res.DetailsFetched != 2 {
		t.Fatalf("expected 2 details refetched, got %d", res.DetailsFetched)
	}
	if res.UpdatedOrders != 2 {
		t.Fatalf("expected 2 updates on force-full rerun, got %d", res.UpdatedOrders)
	}
}

func TestSyncRecoversFrom429DuringDetailPhase(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	client.purchaseFailures = map[string]*purchaseFailureSpec{
		"p1": {Remaining: 2, Status: 429, RetryAfter: 2 * time.Second},
	}
	sleeper := &recordingSleeper{}
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")

	res, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		sleep:     sleeper.sleep,
		backoff:   &backoffPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: 60 * time.Second},
	})
	if err != nil {
		t.Fatalf("Sync should recover from 429, got %v", err)
	}
	if res.DetailsFetched != 2 {
		t.Fatalf("expected 2 details after recovery, got %d", res.DetailsFetched)
	}
	if res.InsertedOrders != 2 {
		t.Fatalf("expected 2 inserts after recovery, got %d", res.InsertedOrders)
	}
	if client.purchaseFailures["p1"].Remaining != 0 {
		t.Fatalf("expected fake to have exhausted the 429 budget, %d remaining", client.purchaseFailures["p1"].Remaining)
	}

	// Two retries on p1 → exactly two backoff sleeps honoring Retry-After.
	// Inter-call pacing sleeps (RateLimit=1ms) are also recorded; count only
	// the >=Retry-After ones.
	retrySleeps := 0
	for _, d := range sleeper.durations {
		if d >= 2*time.Second {
			retrySleeps++
		}
	}
	if retrySleeps != 2 {
		t.Fatalf("expected 2 retry sleeps of 2s, got %v", sleeper.durations)
	}
}

func TestSyncSurfacesResumableHintWhen429PersistsThroughBackoff(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	client.purchaseFailures = map[string]*purchaseFailureSpec{
		"p1": {Remaining: 1000, Status: 429},
	}
	sleeper := &recordingSleeper{}
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")

	_, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		sleep:     sleeper.sleep,
		backoff:   &backoffPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 4 * time.Second},
	})
	if err == nil {
		t.Fatal("expected Sync to surface persistent rate-limit error")
	}
	if !strings.Contains(err.Error(), "rerun \"wolt stats\"") {
		t.Fatalf("expected resumable-rerun hint in error, got %q", err.Error())
	}
}

func TestSyncRefreshesAccessTokenOn401AndPersistsRotation(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	client.rejectAccessToken = "expired-wt"
	client.nextAccessToken = "fresh-wt"
	client.nextRefreshToken = "rotated-rt"

	var rotated []woltgateway.AuthContext
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")

	res, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		Auth:      woltgateway.AuthContext{WToken: "expired-wt", RefreshToken: "old-rt"},
		Refresher: client.Refresher,
		OnAuthRotated: func(updated woltgateway.AuthContext) error {
			rotated = append(rotated, updated)
			return nil
		},
		sleep:   (&recordingSleeper{}).sleep,
		backoff: &backoffPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 10 * time.Second},
	})
	if err != nil {
		t.Fatalf("Sync should recover from 401, got %v", err)
	}
	if res.DetailsFetched != 2 {
		t.Fatalf("expected 2 details fetched after refresh, got %d", res.DetailsFetched)
	}
	if client.refreshCalls != 1 {
		t.Fatalf("expected exactly 1 refresh call, got %d", client.refreshCalls)
	}
	if len(rotated) != 1 {
		t.Fatalf("expected OnAuthRotated invoked once, got %d", len(rotated))
	}
	if rotated[0].WToken != "fresh-wt" || rotated[0].RefreshToken != "rotated-rt" {
		t.Fatalf("OnAuthRotated saw unexpected context: %+v", rotated[0])
	}
}

func TestSyncSurfaces401WhenRefresherFails(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	client.rejectAccessToken = "expired-wt"
	client.refreshErr = errors.New("refresh token revoked")

	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")
	_, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		Auth:      woltgateway.AuthContext{WToken: "expired-wt", RefreshToken: "old-rt"},
		Refresher: client.Refresher,
		sleep:     (&recordingSleeper{}).sleep,
		backoff:   &backoffPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: 4 * time.Second},
	})
	if err == nil {
		t.Fatal("expected Sync to surface the 401 + refresh failure")
	}
	if !strings.Contains(err.Error(), "refresh token revoked") {
		t.Fatalf("expected refresher error in surfaced message, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected mention of original 401, got %q", err.Error())
	}
}

func TestSyncWithoutRefresherSurfaces401Directly(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	client.rejectAccessToken = "expired-wt"

	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")
	_, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		PageSize:  50,
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		Auth:      woltgateway.AuthContext{WToken: "expired-wt", RefreshToken: "old-rt"},
		sleep:     (&recordingSleeper{}).sleep,
		backoff:   &backoffPolicy{MaxAttempts: 2, BaseDelay: time.Second, MaxDelay: 4 * time.Second},
	})
	if err == nil {
		t.Fatal("expected 401 to surface when no Refresher is wired")
	}
	if client.refreshCalls != 0 {
		t.Fatalf("no refresher → no refresh calls; got %d", client.refreshCalls)
	}
}

func TestExtractMinorHandlesShapes(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want int
	}{
		{"raw int", 1234, 1234},
		{"raw float", 12.0, 12},
		{"map amount int", map[string]any{"amount": 999.0}, 999},
		{"nested map", map[string]any{"amount": map[string]any{"amount": 42.0}}, 42},
		{"unrelated map", map[string]any{"label": "x"}, 0},
		{"nil", nil, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := extractMinor(c.in); got != c.want {
				t.Fatalf("extractMinor(%v): want %d, got %d", c.in, c.want, got)
			}
		})
	}
}

func TestSlugifyMatchesNode(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user@example.com", "user-example-com"},
		{"  Mixed.Case+Tag@DOMAIN.io ", "mixed-case-tag-domain-io"},
		{"unicode-örjan@x.io", "unicode-rjan-x-io"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := slugify(c.in); got != c.want {
				t.Fatalf("slugify(%q): want %q, got %q", c.in, c.want, got)
			}
		})
	}
}

func TestParseLocalDateTimeRoundTrip(t *testing.T) {
	got := parseLocalDateTime("21/05/2026, 14:30")
	if !got.Valid {
		t.Fatal("expected valid result")
	}
	if got.Date != "2026-05-21" {
		t.Fatalf("Date: %q", got.Date)
	}
	if got.Month != "2026-05" {
		t.Fatalf("Month: %q", got.Month)
	}
	if got.Datetime != "2026-05-21T14:30:00" {
		t.Fatalf("Datetime: %q", got.Datetime)
	}
	if got.Hour != 14 {
		t.Fatalf("Hour: %d", got.Hour)
	}
	// 2026-05-21 is a Thursday → weekday 4
	if got.Weekday != 4 {
		t.Fatalf("Weekday: %d", got.Weekday)
	}
}

func TestParseLocalDateTimeRejectsGarbage(t *testing.T) {
	bad := parseLocalDateTime("yesterday")
	if bad.Valid {
		t.Fatal("expected invalid for garbage input")
	}
}

func TestCatalogStopDecisionDetectsKnownPurchase(t *testing.T) {
	known := map[string]struct{}{"p1": {}}
	res := catalogStopDecision([]any{
		map[string]any{"purchase_id": "p9", "payment_time_ts": 200},
		map[string]any{"purchase_id": "p1", "payment_time_ts": 100},
	}, known, 50)
	if res != "known_purchase" {
		t.Fatalf("expected known_purchase, got %q", res)
	}
}

func TestCatalogStopDecisionDetectsCheckpoint(t *testing.T) {
	known := map[string]struct{}{"p1": {}}
	res := catalogStopDecision([]any{
		map[string]any{"purchase_id": "p9", "payment_time_ts": 30},
	}, known, 50)
	if res != "checkpoint_reached" {
		t.Fatalf("expected checkpoint_reached, got %q", res)
	}
}

// ----- test fixtures -----

func twoOrderCorpus() []fakePage {
	return []fakePage{
		{
			Orders: []map[string]any{
				{
					"purchase_id":     "p1",
					"payment_time_ts": 1700000000,
					"currency":        "EUR",
					"status":          "delivered",
					"received_at":     "2026-05-20T10:00:00Z",
					"items_summary":   "Pizza Margherita, Coke",
					"venue_name":      "Pizzeria",
				},
				{
					"purchase_id":     "p2",
					"payment_time_ts": 1700100000,
					"currency":        "EUR",
					"status":          "delivered",
					"received_at":     "2026-05-21T14:30:00Z",
					"items_summary":   "Ramen",
					"venue_name":      "Ramen House",
				},
			},
		},
	}
}

type fakePage struct {
	Orders    []map[string]any
	NextToken string
}

type fakeClient struct {
	pages         []fakePage
	mu            sync.Mutex
	listCalls     int
	purchaseCalls int
	refreshCalls  int
	// purchaseFailures lets a test inject N transient failures for a given
	// purchase ID before the canned success payload is returned. Used to
	// exercise the 429-retry path without forking the existing corpus.
	purchaseFailures map[string]*purchaseFailureSpec
	// rejectAccessToken, when non-empty, makes OrderHistoryPurchase return
	// 401 whenever it sees this token. Exercises the 401-then-refresh path.
	rejectAccessToken string
	// refreshErr, when non-nil, is what the Refresher closure returns.
	// Otherwise the Refresher swaps the access token to nextAccessToken
	// (and refresh token to nextRefreshToken if set) and returns success.
	refreshErr        error
	nextAccessToken   string
	nextRefreshToken  string
}

func (f *fakeClient) Refresher(_ context.Context, refreshToken string, _ woltgateway.AuthContext) (woltgateway.TokenRefreshResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refreshCalls++
	if f.refreshErr != nil {
		return woltgateway.TokenRefreshResult{}, f.refreshErr
	}
	return woltgateway.TokenRefreshResult{
		AccessToken:  f.nextAccessToken,
		RefreshToken: f.nextRefreshToken,
	}, nil
}

type purchaseFailureSpec struct {
	Remaining  int
	Status     int
	RetryAfter time.Duration
}

func newFakeClient(pages []fakePage) *fakeClient {
	return &fakeClient{pages: pages}
}

func (f *fakeClient) resetCounts() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listCalls = 0
	f.purchaseCalls = 0
}

func (f *fakeClient) OrderHistory(_ context.Context, _ woltgateway.AuthContext, opts woltgateway.OrderHistoryOptions) (map[string]any, error) {
	f.mu.Lock()
	f.listCalls++
	idx := 0
	if opts.PageToken != "" {
		for i, p := range f.pages {
			if p.NextToken == opts.PageToken {
				idx = i + 1
				break
			}
		}
	}
	if idx >= len(f.pages) {
		f.mu.Unlock()
		return map[string]any{"orders": []any{}}, nil
	}
	p := f.pages[idx]
	f.mu.Unlock()

	out := make([]any, 0, len(p.Orders))
	for _, o := range p.Orders {
		out = append(out, o)
	}
	payload := map[string]any{"orders": out}
	if p.NextToken != "" {
		payload["next_page_token"] = p.NextToken
	}
	return payload, nil
}

func (f *fakeClient) OrderHistoryPurchase(_ context.Context, purchaseID string, auth woltgateway.AuthContext) (map[string]any, error) {
	f.mu.Lock()
	f.purchaseCalls++
	if spec, ok := f.purchaseFailures[purchaseID]; ok && spec.Remaining > 0 {
		spec.Remaining--
		f.mu.Unlock()
		return nil, &woltgateway.UpstreamRequestError{
			Method:     "GET",
			URL:        "https://consumer-api.wolt.com/order-tracking-api/v1/order_history/purchase/" + purchaseID,
			StatusCode: spec.Status,
			RetryAfter: spec.RetryAfter,
		}
	}
	// Reject any access token the test marked as expired. Lets the
	// 401-refresh test path force exactly one 401 before refresh.
	if f.rejectAccessToken != "" && auth.WToken == f.rejectAccessToken {
		f.mu.Unlock()
		return nil, &woltgateway.UpstreamRequestError{
			Method:     "GET",
			URL:        "https://consumer-api.wolt.com/order-tracking-api/v1/order_history/purchase/" + purchaseID,
			StatusCode: 401,
		}
	}
	f.mu.Unlock()
	switch purchaseID {
	case "p1":
		return map[string]any{
			"order_number":  "WLT-1",
			"status":        "delivered",
			"creation_time": "20/05/2026, 10:00",
			"delivery_time": "20/05/2026, 10:30",
			"currency":      "EUR",
			"totals": map[string]any{
				"total":       map[string]any{"amount": 1599.0},
				"subtotal":    map[string]any{"amount": 1399.0},
				"items":       map[string]any{"amount": 1399.0},
				"delivery":    map[string]any{"amount": 100.0},
				"service_fee": map[string]any{"amount": 100.0},
			},
			"venue": map[string]any{"id": "v1", "name": "Pizzeria", "country": "FIN"},
			"items": []any{
				map[string]any{"id": "i1", "name": "Pizza Margherita", "count": 1.0, "price": map[string]any{"amount": 999.0}, "line_total": map[string]any{"amount": 999.0}},
			},
			"payments": []any{
				map[string]any{"name": "Mastercard ••••1234", "amount": map[string]any{"amount": 1599.0}, "method_type": "card", "provider": "stripe"},
			},
		}, nil
	case "p2":
		return map[string]any{
			"order_number":  "WLT-2",
			"status":        "delivered",
			"creation_time": "21/05/2026, 14:30",
			"delivery_time": "21/05/2026, 15:10",
			"currency":      "EUR",
			"totals": map[string]any{
				"total":       map[string]any{"amount": 2499.0},
				"subtotal":    map[string]any{"amount": 2099.0},
				"items":       map[string]any{"amount": 2099.0},
				"delivery":    map[string]any{"amount": 200.0},
				"service_fee": map[string]any{"amount": 200.0},
			},
			"venue": map[string]any{"id": "v2", "name": "Ramen House", "country": "FIN"},
			"items": []any{
				map[string]any{"id": "i2", "name": "Tonkotsu", "count": 1.0, "price": map[string]any{"amount": 1499.0}, "line_total": map[string]any{"amount": 1499.0}},
				map[string]any{"id": "i3", "name": "Gyoza", "count": 1.0, "price": map[string]any{"amount": 600.0}, "line_total": map[string]any{"amount": 600.0}},
			},
			"payments": []any{
				map[string]any{"name": "Wolt+ credits", "amount": map[string]any{"amount": 2499.0}, "method_type": "credits", "provider": "internal"},
			},
		}, nil
	}
	return nil, nil
}

func fixedClock() func() time.Time {
	t := time.Date(2026, 5, 21, 21, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

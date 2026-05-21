// Package statssync synchronises a Wolt account's order history into the
// SQLite database the wolt-stats dashboard reads. It is a pure-Go port of
// wolt-stats/scripts/sync-wolt-history.mjs + wolt-sync-db.mjs, sharing the
// same schema and stop-decision semantics so the resulting file is
// byte-compatible with the Node implementation.
package statssync

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

// DefaultPageSize matches sync-wolt-history.mjs:40 and the Wolt API's max.
const DefaultPageSize = 50

// DefaultRateLimit matches scripts/lib/wolt-sync-runtime.mjs:3.
const DefaultRateLimit = 650 * time.Millisecond

// WoltClient is the slice of the wolt gateway statssync needs. The CLI
// passes deps.Wolt directly; tests pass a fake.
type WoltClient interface {
	OrderHistory(ctx context.Context, auth woltgateway.AuthContext, opts woltgateway.OrderHistoryOptions) (map[string]any, error)
	OrderHistoryPurchase(ctx context.Context, purchaseID string, auth woltgateway.AuthContext) (map[string]any, error)
}

// Options controls a single Sync run.
type Options struct {
	DBPath      string
	UserEmail   string
	UserID      string // optional override; defaults to slug of UserEmail
	UserLabel   string // optional display label; defaults to email's local part
	ProfileName string // for sync_runs.profile_name; defaults to "default"
	Auth        woltgateway.AuthContext
	PageSize    int
	ForceFull   bool
	RateLimit   time.Duration // sleep between Wolt calls; defaults to DefaultRateLimit
	Now         func() time.Time
}

// Result summarises a Sync run. Field names match the JSON envelope the
// orchestrator emits.
type Result struct {
	Mode              string `json:"mode"`
	PagesFetched      int    `json:"pages_fetched"`
	OrdersScanned     int    `json:"orders_scanned"`
	DetailsFetched    int    `json:"details_fetched"`
	InsertedOrders    int    `json:"inserted_orders"`
	UpdatedOrders     int    `json:"updated_orders"`
	CatalogCount      int    `json:"catalog_count"`
	DetailCount       int    `json:"detail_count"`
	StopReason        string `json:"stop_reason,omitempty"`
	ReachedHistoryEnd bool   `json:"reached_history_end"`
	DurationMs        int64  `json:"duration_ms"`
}

// Sync runs the catalog + detail phases, returning a Result describing
// what changed. Sync writes incrementally; if ctx is cancelled mid-run the
// already-committed transactions remain on disk and a subsequent call
// resumes from there.
func Sync(ctx context.Context, client WoltClient, opts Options) (Result, error) {
	if client == nil {
		return Result{}, errors.New("statssync: WoltClient is required")
	}
	email := strings.ToLower(strings.TrimSpace(opts.UserEmail))
	if email == "" {
		return Result{}, errors.New("statssync: UserEmail is required")
	}
	userID := strings.TrimSpace(opts.UserID)
	if userID == "" {
		userID = slugify(email)
	}
	if userID == "" {
		return Result{}, errors.New("statssync: could not derive user id from email")
	}
	label := strings.TrimSpace(opts.UserLabel)
	if label == "" {
		if at := strings.IndexByte(email, '@'); at > 0 {
			label = email[:at]
		} else {
			label = email
		}
	}
	profileName := strings.TrimSpace(opts.ProfileName)
	if profileName == "" {
		profileName = "default"
	}
	pageSize := opts.PageSize
	if pageSize <= 0 || pageSize > DefaultPageSize {
		pageSize = DefaultPageSize
	}
	rateLimit := opts.RateLimit
	if rateLimit <= 0 {
		rateLimit = DefaultRateLimit
	}
	clock := opts.Now
	if clock == nil {
		clock = time.Now
	}

	db, err := openStore(ctx, opts.DBPath)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = db.Close() }()

	startedAt := clock().UTC()
	startedISO := startedAt.Format(time.RFC3339)

	if err := withTx(ctx, db, func(tx *sql.Tx) error {
		return upsertUser(ctx, tx, userID, email, label, startedAt)
	}); err != nil {
		return Result{}, fmt.Errorf("statssync: upsert user: %w", err)
	}

	knownCount, err := getCatalogCount(ctx, db, userID)
	if err != nil {
		return Result{}, fmt.Errorf("statssync: catalog count: %w", err)
	}
	newestKnown, err := getNewestCatalogPaymentTimeTs(ctx, db, userID)
	if err != nil {
		return Result{}, fmt.Errorf("statssync: newest payment ts: %w", err)
	}
	mode := resolveCatalogScanMode(opts.ForceFull, knownCount, newestKnown)
	knownIDs := map[string]struct{}{}
	if mode == "incremental" {
		knownIDs, err = loadKnownPurchaseIDs(ctx, db, userID)
		if err != nil {
			return Result{}, fmt.Errorf("statssync: load known ids: %w", err)
		}
	}

	catalog, err := runCatalogPhase(ctx, client, db, opts.Auth, catalogParams{
		UserID:               userID,
		PageSize:             pageSize,
		Mode:                 mode,
		KnownPurchaseIDs:     knownIDs,
		NewestKnownPaymentTs: newestKnown,
		RateLimit:            rateLimit,
		Clock:                clock,
	})
	if err != nil {
		return Result{}, err
	}

	detail, err := runDetailPhase(ctx, client, db, opts.Auth, detailParams{
		UserID:    userID,
		ForceFull: opts.ForceFull,
		RateLimit: rateLimit,
		Clock:     clock,
	})
	if err != nil {
		return Result{}, err
	}

	finishedAt := clock().UTC()
	catalogCount, _ := getCatalogCount(ctx, db, userID)
	detailCount, _ := getDetailCount(ctx, db, userID)

	if err := withTx(ctx, db, func(tx *sql.Tx) error {
		newestPaymentTs, txErr := tx.QueryContext(ctx, `SELECT COALESCE(MAX(payment_time_ts), 0) FROM order_catalog WHERE user_id = ?`, userID)
		if txErr != nil {
			return txErr
		}
		var newest int
		for newestPaymentTs.Next() {
			var v sql.NullInt64
			if scanErr := newestPaymentTs.Scan(&v); scanErr != nil {
				_ = newestPaymentTs.Close()
				return scanErr
			}
			if v.Valid {
				newest = int(v.Int64)
			}
		}
		_ = newestPaymentTs.Close()

		if err := updateSyncState(ctx, tx, userID, syncStateUpdate{
			NewestPaymentTimeTs:   newest,
			FullBackfillCompleted: catalogCount > 0 && detailCount >= catalogCount,
			CatalogOrderCount:     catalogCount,
			DetailOrderCount:      detailCount,
			ExpectedOrderCount:    sql.NullInt64{},
			StartedAt:             startedISO,
		}, finishedAt); err != nil {
			return err
		}
		return insertSyncRun(ctx, tx, userID, profileName, runRecord{
			StartedAt:         startedISO,
			FinishedAt:        finishedAt.Format(time.RFC3339),
			PagesFetched:      catalog.PagesFetched,
			OrdersScanned:     catalog.OrdersScanned,
			DetailsFetched:    detail.Fetched,
			InsertedOrders:    detail.Inserted,
			UpdatedOrders:     detail.Updated,
			ReachedHistoryEnd: catalog.StopReason == "",
		})
	}); err != nil {
		return Result{}, fmt.Errorf("statssync: finalize sync state: %w", err)
	}

	return Result{
		Mode:              mode,
		PagesFetched:      catalog.PagesFetched,
		OrdersScanned:     catalog.OrdersScanned,
		DetailsFetched:    detail.Fetched,
		InsertedOrders:    detail.Inserted,
		UpdatedOrders:     detail.Updated,
		CatalogCount:      catalogCount,
		DetailCount:       detailCount,
		StopReason:        catalog.StopReason,
		ReachedHistoryEnd: catalog.StopReason == "",
		DurationMs:        finishedAt.Sub(startedAt).Milliseconds(),
	}, nil
}

func resolveCatalogScanMode(forceFull bool, knownCount, newestKnownTs int) string {
	if forceFull {
		return "full"
	}
	if knownCount <= 0 || newestKnownTs <= 0 {
		return "full"
	}
	return "incremental"
}

type catalogParams struct {
	UserID               string
	PageSize             int
	Mode                 string
	KnownPurchaseIDs     map[string]struct{}
	NewestKnownPaymentTs int
	RateLimit            time.Duration
	Clock                func() time.Time
}

type catalogResult struct {
	PagesFetched  int
	OrdersScanned int
	StopReason    string
}

func runCatalogPhase(ctx context.Context, client WoltClient, db *sql.DB, auth woltgateway.AuthContext, p catalogParams) (catalogResult, error) {
	var (
		result        catalogResult
		nextPageToken string
		seenTokens    = map[string]struct{}{}
	)
	incremental := p.Mode == "incremental"

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := sleepCtx(ctx, p.RateLimit); err != nil {
			return result, err
		}
		page, err := client.OrderHistory(ctx, auth, woltgateway.OrderHistoryOptions{
			Limit:     p.PageSize,
			PageToken: nextPageToken,
		})
		if err != nil {
			return result, fmt.Errorf("statssync: order history list: %w", err)
		}
		summaries := asSlice(page["orders"])
		if len(summaries) == 0 {
			break
		}
		result.PagesFetched++
		result.OrdersScanned += len(summaries)

		now := p.Clock().UTC()
		if err := withTx(ctx, db, func(tx *sql.Tx) error {
			for _, raw := range summaries {
				summary := asMap(raw)
				purchaseID := strings.TrimSpace(asString(summary["purchase_id"]))
				if purchaseID == "" {
					continue
				}
				if err := upsertOrderCatalogEntry(ctx, tx, p.UserID, purchaseID, summary, now); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return result, fmt.Errorf("statssync: persist catalog page: %w", err)
		}

		stop := ""
		if incremental {
			stop = catalogStopDecision(summaries, p.KnownPurchaseIDs, p.NewestKnownPaymentTs)
		}
		if stop != "" {
			result.StopReason = stop
			break
		}
		pageToken := strings.TrimSpace(asString(page["next_page_token"]))
		if pageToken == "" {
			break
		}
		if _, seen := seenTokens[pageToken]; seen {
			return result, fmt.Errorf("statssync: pagination repeated next_page_token %q", pageToken)
		}
		seenTokens[pageToken] = struct{}{}
		nextPageToken = pageToken
	}
	return result, nil
}

// catalogStopDecision mirrors getCatalogStopDecision in
// wolt-stats/scripts/lib/wolt-sync-catalog.mjs. It returns an empty string
// when the loop should continue, or one of {"known_purchase",
// "checkpoint_reached"} when it should stop after the current page.
func catalogStopDecision(summaries []any, known map[string]struct{}, newestKnownTs int) string {
	if len(summaries) == 0 || newestKnownTs <= 0 {
		return ""
	}
	for _, raw := range summaries {
		summary := asMap(raw)
		id := strings.TrimSpace(asString(summary["purchase_id"]))
		if id == "" {
			continue
		}
		if _, ok := known[id]; ok {
			return "known_purchase"
		}
	}
	for _, raw := range summaries {
		summary := asMap(raw)
		ts := asInt(summary["payment_time_ts"])
		if ts > 0 && ts < newestKnownTs {
			return "checkpoint_reached"
		}
	}
	return ""
}

type detailParams struct {
	UserID    string
	ForceFull bool
	RateLimit time.Duration
	Clock     func() time.Time
}

type detailResult struct {
	Queued   int
	Fetched  int
	Inserted int
	Updated  int
}

func runDetailPhase(ctx context.Context, client WoltClient, db *sql.DB, auth woltgateway.AuthContext, p detailParams) (detailResult, error) {
	queue, err := loadDetailQueue(ctx, db, p.UserID, p.ForceFull)
	if err != nil {
		return detailResult{}, err
	}
	result := detailResult{Queued: len(queue)}
	if len(queue) == 0 {
		return result, nil
	}
	existing, err := loadKnownDetailedOrders(ctx, db, p.UserID)
	if err != nil {
		return detailResult{}, err
	}

	for _, entry := range queue {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := sleepCtx(ctx, p.RateLimit); err != nil {
			return result, err
		}
		detail, err := client.OrderHistoryPurchase(ctx, entry.PurchaseID, auth)
		if err != nil {
			return result, fmt.Errorf("statssync: order detail %s: %w", entry.PurchaseID, err)
		}
		now := p.Clock().UTC()
		if err := withTx(ctx, db, func(tx *sql.Tx) error {
			return upsertOrderBundle(ctx, tx, p.UserID, entry.PurchaseID, entry.Summary, detail, now)
		}); err != nil {
			return result, fmt.Errorf("statssync: persist detail %s: %w", entry.PurchaseID, err)
		}
		result.Fetched++
		if _, ok := existing[entry.PurchaseID]; ok {
			result.Updated++
		} else {
			result.Inserted++
			existing[entry.PurchaseID] = struct{}{}
		}
	}
	return result, nil
}

func loadKnownDetailedOrders(ctx context.Context, db *sql.DB, userID string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT purchase_id FROM orders WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]struct{})
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = struct{}{}
	}
	return out, rows.Err()
}

func withTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// --- parse helpers ---

type localDateTime struct {
	Date     string
	Datetime string
	Month    string
	Weekday  int
	Hour     int
	Valid    bool
}

var localDateTimePattern = regexp.MustCompile(`^(\d{2})/(\d{2})/(\d{4}),\s*(\d{2}):(\d{2})$`)

// parseLocalDateTime mirrors parseLocalDateTime in
// wolt-stats/scripts/lib/wolt-sync-db.mjs:812. Wolt's creation_time is the
// user's local clock in dd/mm/yyyy, hh:mm — not a wire timestamp — so this
// is the documented parser.
func parseLocalDateTime(raw string) localDateTime {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return localDateTime{}
	}
	m := localDateTimePattern.FindStringSubmatch(raw)
	if m == nil {
		return localDateTime{}
	}
	day, month, year, hour, minute := m[1], m[2], m[3], m[4], m[5]
	date := year + "-" + month + "-" + day
	hourInt, _ := strconv.Atoi(hour)
	dayInt, _ := strconv.Atoi(day)
	monthInt, _ := strconv.Atoi(month)
	yearInt, _ := strconv.Atoi(year)
	weekday := int(time.Date(yearInt, time.Month(monthInt), dayInt, 12, 0, 0, 0, time.UTC).Weekday())
	return localDateTime{
		Date:     date,
		Datetime: date + "T" + hour + ":" + minute + ":00",
		Month:    year + "-" + month,
		Weekday:  weekday,
		Hour:     hourInt,
		Valid:    true,
	}
}

// slugify mirrors the Node `slugify` so a given email maps to the same
// canonical user_id regardless of which implementation wrote the row.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	prevDash := false
	for _, r := range s {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" && s != "" {
		// Fall back to a stable hash when the input has zero ASCII alnum
		// (rare — e.g., all-Cyrillic email local-parts that survive Wolt).
		sum := sha1.Sum([]byte(s))
		out = "u" + hex.EncodeToString(sum[:6])
	}
	return out
}

func extractMinor(candidate any) int {
	if candidate == nil {
		return 0
	}
	switch v := candidate.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case map[string]any:
		if amt, ok := v["amount"]; ok {
			switch a := amt.(type) {
			case int:
				return a
			case int64:
				return int(a)
			case float64:
				return int(a)
			case map[string]any:
				return extractMinor(a)
			}
		}
		if value, ok := v["value"]; ok {
			if m, ok := value.(map[string]any); ok {
				if amt, ok := m["amount"]; ok {
					if n, ok := amt.(float64); ok {
						return int(n)
					}
				}
			}
		}
		if lt, ok := v["line_total"]; ok {
			if m, ok := lt.(map[string]any); ok {
				if amt, ok := m["amount"]; ok {
					if n, ok := amt.(float64); ok {
						return int(n)
					}
				}
			}
		}
	}
	return 0
}

func sumCollectionAmounts(value any) int {
	total := 0
	for _, raw := range asSlice(value) {
		total += extractMinor(raw)
	}
	return total
}

func asMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func asSlice(value any) []any {
	if value == nil {
		return nil
	}
	if s, ok := value.([]any); ok {
		return s
	}
	return nil
}

func asString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 0
}

func asIntOr(value any, fallback int) int {
	if value == nil {
		return fallback
	}
	if n := asInt(value); n != 0 {
		return n
	}
	if _, ok := value.(float64); ok {
		return asInt(value)
	}
	return fallback
}

func nullableString(value any) interface{} {
	if value == nil {
		return nil
	}
	s := strings.TrimSpace(asString(value))
	if s == "" {
		return nil
	}
	return s
}

func nullableStringFromMap(m map[string]any, key string) interface{} {
	if m == nil {
		return nil
	}
	return nullableString(m[key])
}

func nullableTime(s string) interface{} {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func stringOrEmpty(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

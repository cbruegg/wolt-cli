package statssync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // sqlite driver
)

// schemaSQL mirrors the schema in
// wolt-stats/scripts/lib/wolt-sync-db.mjs:26-170. The wolt-stats dashboard
// SELECTs against these table/column names verbatim, so this is the contract
// — any divergence breaks the dashboard's queries.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    label TEXT,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_state (
    user_id TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    newest_payment_time_ts INTEGER NOT NULL DEFAULT 0,
    full_backfill_completed INTEGER NOT NULL DEFAULT 0,
    resume_page_token TEXT,
    resume_page_number INTEGER NOT NULL DEFAULT 1,
    catalog_order_count INTEGER NOT NULL DEFAULT 0,
    detail_order_count INTEGER NOT NULL DEFAULT 0,
    expected_order_count INTEGER,
    last_sync_started_at TEXT,
    last_sync_finished_at TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sync_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    profile_name TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL,
    pages_fetched INTEGER NOT NULL DEFAULT 0,
    orders_scanned INTEGER NOT NULL DEFAULT 0,
    details_fetched INTEGER NOT NULL DEFAULT 0,
    inserted_orders INTEGER NOT NULL DEFAULT 0,
    updated_orders INTEGER NOT NULL DEFAULT 0,
    reached_history_end INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS order_catalog (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purchase_id TEXT NOT NULL,
    payment_time_ts INTEGER NOT NULL,
    currency TEXT,
    status TEXT,
    received_at_raw TEXT,
    items_summary TEXT,
    venue_name TEXT,
    summary_json TEXT NOT NULL,
    discovered_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    PRIMARY KEY (user_id, purchase_id)
);

CREATE TABLE IF NOT EXISTS orders (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    purchase_id TEXT NOT NULL,
    order_number TEXT,
    status TEXT NOT NULL,
    payment_time_ts INTEGER NOT NULL,
    order_local_datetime TEXT,
    order_local_date TEXT,
    order_local_month TEXT,
    order_local_weekday INTEGER,
    order_local_hour INTEGER,
    creation_time_raw TEXT,
    delivery_time_raw TEXT,
    received_at_raw TEXT,
    currency TEXT NOT NULL,
    total_amount_minor INTEGER NOT NULL,
    subtotal_minor INTEGER NOT NULL DEFAULT 0,
    items_amount_minor INTEGER NOT NULL DEFAULT 0,
    delivery_fee_minor INTEGER NOT NULL DEFAULT 0,
    service_fee_minor INTEGER NOT NULL DEFAULT 0,
    fees_minor INTEGER NOT NULL DEFAULT 0,
    credits_minor INTEGER NOT NULL DEFAULT 0,
    tokens_minor INTEGER NOT NULL DEFAULT 0,
    discount_amount_minor INTEGER NOT NULL DEFAULT 0,
    surcharge_amount_minor INTEGER NOT NULL DEFAULT 0,
    delivery_method TEXT,
    delivery_city TEXT,
    delivery_alias TEXT,
    venue_id TEXT,
    venue_name TEXT,
    venue_country TEXT,
    venue_address TEXT,
    venue_product_line TEXT,
    items_summary TEXT,
    payment_provider TEXT,
    payment_method_type TEXT,
    payment_method_name TEXT,
    raw_json TEXT NOT NULL,
    synced_at TEXT NOT NULL,
    PRIMARY KEY (user_id, purchase_id)
);

CREATE TABLE IF NOT EXISTS order_items (
    user_id TEXT NOT NULL,
    purchase_id TEXT NOT NULL,
    item_index INTEGER NOT NULL,
    item_id TEXT,
    item_name TEXT NOT NULL,
    quantity INTEGER NOT NULL,
    unit_price_minor INTEGER NOT NULL DEFAULT 0,
    line_total_minor INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, purchase_id, item_index),
    FOREIGN KEY (user_id, purchase_id) REFERENCES orders(user_id, purchase_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS order_item_option_values (
    user_id TEXT NOT NULL,
    purchase_id TEXT NOT NULL,
    item_index INTEGER NOT NULL,
    option_index INTEGER NOT NULL,
    value_index INTEGER NOT NULL,
    option_group_id TEXT,
    option_group_name TEXT,
    option_value_id TEXT,
    option_value_name TEXT,
    quantity INTEGER NOT NULL DEFAULT 1,
    price_minor INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (user_id, purchase_id, item_index, option_index, value_index),
    FOREIGN KEY (user_id, purchase_id, item_index) REFERENCES order_items(user_id, purchase_id, item_index) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS order_payments (
    user_id TEXT NOT NULL,
    purchase_id TEXT NOT NULL,
    payment_index INTEGER NOT NULL,
    method_id TEXT,
    provider TEXT,
    method_type TEXT,
    method_name TEXT,
    amount_minor INTEGER NOT NULL DEFAULT 0,
    payment_time_raw TEXT,
    PRIMARY KEY (user_id, purchase_id, payment_index),
    FOREIGN KEY (user_id, purchase_id) REFERENCES orders(user_id, purchase_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_order_catalog_user_payment_ts ON order_catalog(user_id, payment_time_ts);
CREATE INDEX IF NOT EXISTS idx_orders_user_date ON orders(user_id, order_local_date);
CREATE INDEX IF NOT EXISTS idx_orders_user_currency ON orders(user_id, currency);
CREATE INDEX IF NOT EXISTS idx_orders_user_country ON orders(user_id, venue_country);
CREATE INDEX IF NOT EXISTS idx_orders_user_payment_ts ON orders(user_id, payment_time_ts DESC);
CREATE INDEX IF NOT EXISTS idx_order_items_user_name ON order_items(user_id, item_name);
`

// openStore opens the SQLite file at the given path, ensures the schema
// exists, and returns the *sql.DB. The caller owns Close.
func openStore(ctx context.Context, dbPath string) (*sql.DB, error) {
	if strings.TrimSpace(dbPath) == "" {
		return nil, fmt.Errorf("statssync: db path is required")
	}
	// modernc.org/sqlite uses "_pragma" connection-string knobs. Enable
	// foreign keys (matches the Node code's PRAGMA foreign_keys = ON) and
	// busy timeout so concurrent reads from the dashboard during a sync
	// don't error immediately.
	dsn := dbPath + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("statssync: open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("statssync: ping db: %w", err)
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("statssync: create schema: %w", err)
	}
	return db, nil
}

// upsertUser writes the canonical user row.
func upsertUser(ctx context.Context, tx *sql.Tx, userID, email, label string, now time.Time) error {
	iso := now.UTC().Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO users (id, email, label, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			email = excluded.email,
			label = excluded.label,
			updated_at = excluded.updated_at
	`, userID, email, label, iso, iso)
	return err
}

// upsertOrderCatalogEntry persists a single summary row from the listing
// endpoint. summaryJSON is the raw payload for resume/debugging.
func upsertOrderCatalogEntry(ctx context.Context, tx *sql.Tx, userID, purchaseID string, summary map[string]any, now time.Time) error {
	if strings.TrimSpace(purchaseID) == "" {
		return fmt.Errorf("statssync: catalog summary has no purchase_id")
	}
	iso := now.UTC().Format(time.RFC3339)
	rawJSON, err := json.Marshal(summary)
	if err != nil {
		return fmt.Errorf("statssync: marshal summary json: %w", err)
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO order_catalog (
			user_id, purchase_id, payment_time_ts, currency, status,
			received_at_raw, items_summary, venue_name, summary_json,
			discovered_at, last_seen_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, purchase_id) DO UPDATE SET
			payment_time_ts = excluded.payment_time_ts,
			currency = excluded.currency,
			status = excluded.status,
			received_at_raw = excluded.received_at_raw,
			items_summary = excluded.items_summary,
			venue_name = excluded.venue_name,
			summary_json = excluded.summary_json,
			last_seen_at = excluded.last_seen_at
	`,
		userID,
		purchaseID,
		asInt(summary["payment_time_ts"]),
		nullableString(summary["currency"]),
		nullableString(summary["status"]),
		nullableString(summary["received_at"]),
		nullableString(summary["items_summary"]),
		nullableString(summary["venue_name"]),
		string(rawJSON),
		iso,
		iso,
	)
	return err
}

// upsertOrderBundle writes the detail-phase data for a single purchase.
// It mirrors the Node script's upsertOrderBundle: it INSERT/UPSERTs the
// orders row, then replaces all child rows (items, options, payments).
func upsertOrderBundle(ctx context.Context, tx *sql.Tx, userID, purchaseID string, summary, detail map[string]any, now time.Time) error {
	totals := asMap(detail["totals"])
	payments := asSlice(detail["payments"])
	var primaryPayment map[string]any
	if len(payments) > 0 {
		primaryPayment = asMap(payments[0])
	}
	creation := nullableString(detail["creation_time"])
	if creation == nil {
		creation = nullableString(summary["received_at"])
	}
	local := parseLocalDateTime(stringOrEmpty(creation))
	var weekdayVal interface{}
	var hourVal interface{}
	if local.Valid {
		weekdayVal = local.Weekday
		hourVal = local.Hour
	}

	itemSummary := nullableString(summary["items_summary"])
	if itemSummary == nil {
		names := []string{}
		for _, raw := range asSlice(detail["items"]) {
			item := asMap(raw)
			name := strings.TrimSpace(asString(item["name"]))
			if name != "" {
				names = append(names, name)
			}
		}
		if joined := strings.Join(names, ", "); joined != "" {
			itemSummary = &joined
		}
	}

	currency := asString(detail["currency"])
	if strings.TrimSpace(currency) == "" {
		currency = "EUR"
	}
	status := strings.TrimSpace(asString(detail["status"]))
	if status == "" {
		status = strings.TrimSpace(asString(summary["status"]))
	}
	if status == "" {
		status = "unknown"
	}

	rawJSON, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("statssync: marshal detail json: %w", err)
	}
	iso := now.UTC().Format(time.RFC3339)

	venue := asMap(detail["venue"])
	delivery := asMap(detail["delivery"])

	venueName := nullableString(venue["name"])
	if venueName == nil {
		venueName = nullableString(summary["venue_name"])
	}

	deliveryFee := extractMinor(totals["delivery"])
	serviceFee := extractMinor(totals["service_fee"])

	_, err = tx.ExecContext(ctx, `
		INSERT INTO orders (
			user_id, purchase_id, order_number, status, payment_time_ts,
			order_local_datetime, order_local_date, order_local_month,
			order_local_weekday, order_local_hour, creation_time_raw,
			delivery_time_raw, received_at_raw, currency, total_amount_minor,
			subtotal_minor, items_amount_minor, delivery_fee_minor,
			service_fee_minor, fees_minor, credits_minor, tokens_minor,
			discount_amount_minor, surcharge_amount_minor, delivery_method,
			delivery_city, delivery_alias, venue_id, venue_name,
			venue_country, venue_address, venue_product_line, items_summary,
			payment_provider, payment_method_type, payment_method_name,
			raw_json, synced_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, purchase_id) DO UPDATE SET
			order_number = excluded.order_number,
			status = excluded.status,
			payment_time_ts = excluded.payment_time_ts,
			order_local_datetime = excluded.order_local_datetime,
			order_local_date = excluded.order_local_date,
			order_local_month = excluded.order_local_month,
			order_local_weekday = excluded.order_local_weekday,
			order_local_hour = excluded.order_local_hour,
			creation_time_raw = excluded.creation_time_raw,
			delivery_time_raw = excluded.delivery_time_raw,
			received_at_raw = excluded.received_at_raw,
			currency = excluded.currency,
			total_amount_minor = excluded.total_amount_minor,
			subtotal_minor = excluded.subtotal_minor,
			items_amount_minor = excluded.items_amount_minor,
			delivery_fee_minor = excluded.delivery_fee_minor,
			service_fee_minor = excluded.service_fee_minor,
			fees_minor = excluded.fees_minor,
			credits_minor = excluded.credits_minor,
			tokens_minor = excluded.tokens_minor,
			discount_amount_minor = excluded.discount_amount_minor,
			surcharge_amount_minor = excluded.surcharge_amount_minor,
			delivery_method = excluded.delivery_method,
			delivery_city = excluded.delivery_city,
			delivery_alias = excluded.delivery_alias,
			venue_id = excluded.venue_id,
			venue_name = excluded.venue_name,
			venue_country = excluded.venue_country,
			venue_address = excluded.venue_address,
			venue_product_line = excluded.venue_product_line,
			items_summary = excluded.items_summary,
			payment_provider = excluded.payment_provider,
			payment_method_type = excluded.payment_method_type,
			payment_method_name = excluded.payment_method_name,
			raw_json = excluded.raw_json,
			synced_at = excluded.synced_at
	`,
		userID,
		purchaseID,
		nullableString(detail["order_number"]),
		status,
		asInt(summary["payment_time_ts"]),
		nullableTime(local.Datetime),
		nullableTime(local.Date),
		nullableTime(local.Month),
		weekdayVal,
		hourVal,
		creation,
		nullableString(detail["delivery_time"]),
		nullableString(summary["received_at"]),
		currency,
		extractMinor(totals["total"]),
		extractMinor(totals["subtotal"]),
		extractMinor(totals["items"]),
		deliveryFee,
		serviceFee,
		deliveryFee+serviceFee,
		extractMinor(totals["credits"]),
		extractMinor(totals["tokens"]),
		sumCollectionAmounts(detail["discounts"]),
		sumCollectionAmounts(detail["surcharges"]),
		nullableString(detail["delivery_method"]),
		nullableString(delivery["city"]),
		nullableString(delivery["alias"]),
		nullableString(venue["id"]),
		venueName,
		nullableString(venue["country"]),
		nullableString(venue["address"]),
		nullableString(venue["product_line"]),
		itemSummary,
		nullableStringFromMap(primaryPayment, "provider"),
		nullableStringFromMap(primaryPayment, "method_type"),
		nullableStringFromMap(primaryPayment, "name"),
		string(rawJSON),
		iso,
	)
	if err != nil {
		return fmt.Errorf("statssync: upsert orders: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM order_item_option_values WHERE user_id = ? AND purchase_id = ?`, userID, purchaseID); err != nil {
		return fmt.Errorf("statssync: clear option values: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM order_items WHERE user_id = ? AND purchase_id = ?`, userID, purchaseID); err != nil {
		return fmt.Errorf("statssync: clear items: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM order_payments WHERE user_id = ? AND purchase_id = ?`, userID, purchaseID); err != nil {
		return fmt.Errorf("statssync: clear payments: %w", err)
	}

	for paymentIndex, raw := range asSlice(detail["payments"]) {
		payment := asMap(raw)
		_, err := tx.ExecContext(ctx, `
			INSERT INTO order_payments (
				user_id, purchase_id, payment_index, method_id, provider,
				method_type, method_name, amount_minor, payment_time_raw
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
			userID,
			purchaseID,
			paymentIndex,
			nullableString(payment["method_id"]),
			nullableString(payment["provider"]),
			nullableString(payment["method_type"]),
			nullableString(payment["name"]),
			extractMinor(payment["amount"]),
			nullableString(payment["payment_time"]),
		)
		if err != nil {
			return fmt.Errorf("statssync: insert payment %d: %w", paymentIndex, err)
		}
	}

	for itemIndex, raw := range asSlice(detail["items"]) {
		item := asMap(raw)
		itemName := strings.TrimSpace(asString(item["name"]))
		if itemName == "" {
			itemName = "Unknown item"
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO order_items (
				user_id, purchase_id, item_index, item_id, item_name,
				quantity, unit_price_minor, line_total_minor
			)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`,
			userID,
			purchaseID,
			itemIndex,
			nullableString(item["id"]),
			itemName,
			asInt(item["count"]),
			extractMinor(item["price"]),
			extractMinor(item["line_total"]),
		)
		if err != nil {
			return fmt.Errorf("statssync: insert item %d: %w", itemIndex, err)
		}
		for optionIndex, rawOption := range asSlice(item["options"]) {
			option := asMap(rawOption)
			for valueIndex, rawValue := range asSlice(option["values"]) {
				value := asMap(rawValue)
				_, err := tx.ExecContext(ctx, `
					INSERT INTO order_item_option_values (
						user_id, purchase_id, item_index, option_index,
						value_index, option_group_id, option_group_name,
						option_value_id, option_value_name, quantity,
						price_minor
					)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
				`,
					userID,
					purchaseID,
					itemIndex,
					optionIndex,
					valueIndex,
					nullableString(option["id"]),
					nullableString(option["name"]),
					nullableString(value["id"]),
					nullableString(value["name"]),
					asIntOr(value["count"], 1),
					extractMinor(value["price"]),
				)
				if err != nil {
					return fmt.Errorf("statssync: insert option value: %w", err)
				}
			}
		}
	}
	return nil
}

func updateSyncState(ctx context.Context, tx *sql.Tx, userID string, st syncStateUpdate, now time.Time) error {
	iso := now.UTC().Format(time.RFC3339)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sync_state (
			user_id, newest_payment_time_ts, full_backfill_completed,
			resume_page_token, resume_page_number, catalog_order_count,
			detail_order_count, expected_order_count,
			last_sync_started_at, last_sync_finished_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			newest_payment_time_ts = excluded.newest_payment_time_ts,
			full_backfill_completed = excluded.full_backfill_completed,
			resume_page_token = excluded.resume_page_token,
			resume_page_number = excluded.resume_page_number,
			catalog_order_count = excluded.catalog_order_count,
			detail_order_count = excluded.detail_order_count,
			expected_order_count = excluded.expected_order_count,
			last_sync_started_at = excluded.last_sync_started_at,
			last_sync_finished_at = excluded.last_sync_finished_at,
			updated_at = excluded.updated_at
	`,
		userID,
		st.NewestPaymentTimeTs,
		boolToInt(st.FullBackfillCompleted),
		nil,
		1,
		st.CatalogOrderCount,
		st.DetailOrderCount,
		st.ExpectedOrderCount,
		st.StartedAt,
		iso,
		iso,
	)
	return err
}

type syncStateUpdate struct {
	NewestPaymentTimeTs   int
	FullBackfillCompleted bool
	CatalogOrderCount     int
	DetailOrderCount      int
	ExpectedOrderCount    sql.NullInt64
	StartedAt             string
}

func insertSyncRun(ctx context.Context, tx *sql.Tx, userID, profileName string, run runRecord) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO sync_runs (
			user_id, profile_name, started_at, finished_at,
			pages_fetched, orders_scanned, details_fetched,
			inserted_orders, updated_orders, reached_history_end
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		userID,
		profileName,
		run.StartedAt,
		run.FinishedAt,
		run.PagesFetched,
		run.OrdersScanned,
		run.DetailsFetched,
		run.InsertedOrders,
		run.UpdatedOrders,
		boolToInt(run.ReachedHistoryEnd),
	)
	return err
}

type runRecord struct {
	StartedAt         string
	FinishedAt        string
	PagesFetched      int
	OrdersScanned     int
	DetailsFetched    int
	InsertedOrders    int
	UpdatedOrders     int
	ReachedHistoryEnd bool
}

func getCatalogCount(ctx context.Context, db *sql.DB, userID string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_catalog WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

func getDetailCount(ctx context.Context, db *sql.DB, userID string) (int, error) {
	var count int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

func getNewestCatalogPaymentTimeTs(ctx context.Context, db *sql.DB, userID string) (int, error) {
	var v sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(payment_time_ts), 0) FROM order_catalog WHERE user_id = ?`, userID).Scan(&v)
	if err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return int(v.Int64), nil
}

func loadKnownPurchaseIDs(ctx context.Context, db *sql.DB, userID string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx, `SELECT purchase_id FROM order_catalog WHERE user_id = ?`, userID)
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

type catalogQueueEntry struct {
	PaymentTimeTs int
	PurchaseID    string
	Summary       map[string]any
}

func loadDetailQueue(ctx context.Context, db *sql.DB, userID string, forceFull bool) ([]catalogQueueEntry, error) {
	var query string
	if forceFull {
		query = `SELECT purchase_id, payment_time_ts, summary_json
			FROM order_catalog
			WHERE user_id = ?
			ORDER BY payment_time_ts ASC, purchase_id ASC`
	} else {
		query = `SELECT c.purchase_id, c.payment_time_ts, c.summary_json
			FROM order_catalog c
			LEFT JOIN orders o
				ON o.user_id = c.user_id
				AND o.purchase_id = c.purchase_id
			WHERE c.user_id = ?
				AND o.purchase_id IS NULL
			ORDER BY c.payment_time_ts ASC, c.purchase_id ASC`
	}
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []catalogQueueEntry{}
	for rows.Next() {
		var (
			purchaseID    string
			paymentTimeTs int
			summaryJSON   string
		)
		if err := rows.Scan(&purchaseID, &paymentTimeTs, &summaryJSON); err != nil {
			return nil, err
		}
		var summary map[string]any
		if strings.TrimSpace(summaryJSON) != "" {
			_ = json.Unmarshal([]byte(summaryJSON), &summary)
		}
		out = append(out, catalogQueueEntry{
			PaymentTimeTs: paymentTimeTs,
			PurchaseID:    purchaseID,
			Summary:       summary,
		})
	}
	return out, rows.Err()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

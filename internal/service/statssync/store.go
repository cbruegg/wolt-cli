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
	if err := repairLegacyZeroAmounts(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("statssync: repair legacy amounts: %w", err)
	}
	return db, nil
}

// repairLegacyZeroAmounts heals the post-upgrade case where an older
// statssync wrote orders rows with total_amount_minor=0 because it was
// reading from a non-existent nested totals.* map instead of Wolt's flat
// top-level *_price fields. Idempotent — only touches rows where the
// stored amount is zero but the raw_json carries a positive total_price.
// Costs one UPDATE statement when no legacy rows exist (the WHERE filter
// matches nothing); a few hundred ms on a fully-broken DB of ~1k rows.
func repairLegacyZeroAmounts(ctx context.Context, db *sql.DB) error {
	// Amount columns + payment provider/type. WHERE matches only rows that
	// the buggy version wrote zero into; a fresh sync targets nothing.
	if _, err := db.ExecContext(ctx, `
		UPDATE orders SET
		  total_amount_minor = COALESCE(json_extract(raw_json, '$.total_price'),
		                                json_extract(raw_json, '$.totals.total.amount'),    total_amount_minor),
		  subtotal_minor     = COALESCE(json_extract(raw_json, '$.subtotal'),
		                                json_extract(raw_json, '$.totals.subtotal.amount'), subtotal_minor),
		  items_amount_minor = COALESCE(json_extract(raw_json, '$.items_price'),
		                                json_extract(raw_json, '$.totals.items.amount'),    items_amount_minor),
		  delivery_fee_minor = COALESCE(json_extract(raw_json, '$.delivery_price'),
		                                json_extract(raw_json, '$.totals.delivery.amount'), delivery_fee_minor),
		  service_fee_minor  = COALESCE(json_extract(raw_json, '$.service_fee'),
		                                json_extract(raw_json, '$.totals.service_fee.amount'), service_fee_minor),
		  credits_minor      = COALESCE(json_extract(raw_json, '$.credits'),
		                                json_extract(raw_json, '$.totals.credits.amount'),  credits_minor),
		  tokens_minor       = COALESCE(json_extract(raw_json, '$.tokens'),
		                                json_extract(raw_json, '$.totals.tokens.amount'),   tokens_minor),
		  payment_provider   = COALESCE(json_extract(raw_json, '$.payments[0].method.provider'),
		                                json_extract(raw_json, '$.payments[0].provider'),   payment_provider),
		  payment_method_type= COALESCE(json_extract(raw_json, '$.payments[0].method.type'),
		                                json_extract(raw_json, '$.payments[0].method_type'), payment_method_type)
		WHERE total_amount_minor = 0
		  AND COALESCE(json_extract(raw_json, '$.total_price'), 0) > 0
	`); err != nil {
		return err
	}
	// Venue + delivery descriptive columns. Backfill whenever the existing
	// column is NULL/blank but raw_json has the value — covers both the
	// "I just upgraded" path (everything blank) and the "partial fix"
	// path (amounts fixed by an earlier migration, descriptors not).
	if _, err := db.ExecContext(ctx, `
		UPDATE orders SET
		  venue_id           = COALESCE(NULLIF(venue_id, ''),
		                                json_extract(raw_json, '$.venue_id'),
		                                json_extract(raw_json, '$.venue.id')),
		  venue_product_line = COALESCE(NULLIF(venue_product_line, ''),
		                                json_extract(raw_json, '$.venue_product_line'),
		                                json_extract(raw_json, '$.venue.product_line')),
		  venue_country      = COALESCE(NULLIF(venue_country, ''),
		                                json_extract(raw_json, '$.venue_country'),
		                                json_extract(raw_json, '$.venue.country')),
		  venue_address      = COALESCE(NULLIF(venue_address, ''),
		                                json_extract(raw_json, '$.venue_full_address'),
		                                json_extract(raw_json, '$.venue_address'),
		                                json_extract(raw_json, '$.venue.address')),
		  delivery_city      = COALESCE(NULLIF(delivery_city, ''),
		                                json_extract(raw_json, '$.delivery_location.city'),
		                                json_extract(raw_json, '$.delivery.city')),
		  delivery_alias     = COALESCE(NULLIF(delivery_alias, ''),
		                                json_extract(raw_json, '$.delivery_location.alias'),
		                                json_extract(raw_json, '$.delivery.alias'))
		WHERE venue_id IS NULL OR venue_id = ''
		   OR venue_product_line IS NULL OR venue_product_line = ''
		   OR delivery_city IS NULL OR delivery_city = ''
	`); err != nil {
		return err
	}
	// order_payments rows have provider / method_type / method_id all NULL
	// in pre-fix data because the code read flat payment.provider /
	// payment.method_type / payment.method_id, but Wolt nests them under
	// payment.method.{provider,type,id}. Backfill from raw_json using
	// the stored payment_index — works for the primary payment AND any
	// secondary ones (split tenders).
	if _, err := db.ExecContext(ctx, `
		UPDATE order_payments
		SET
		  provider = COALESCE(
		    NULLIF(provider, ''),
		    (SELECT json_extract(o.raw_json, '$.payments[' || order_payments.payment_index || '].method.provider')
		       FROM orders o WHERE o.user_id = order_payments.user_id AND o.purchase_id = order_payments.purchase_id),
		    (SELECT json_extract(o.raw_json, '$.payments[' || order_payments.payment_index || '].provider')
		       FROM orders o WHERE o.user_id = order_payments.user_id AND o.purchase_id = order_payments.purchase_id)
		  ),
		  method_type = COALESCE(
		    NULLIF(method_type, ''),
		    (SELECT json_extract(o.raw_json, '$.payments[' || order_payments.payment_index || '].method.type')
		       FROM orders o WHERE o.user_id = order_payments.user_id AND o.purchase_id = order_payments.purchase_id),
		    (SELECT json_extract(o.raw_json, '$.payments[' || order_payments.payment_index || '].method_type')
		       FROM orders o WHERE o.user_id = order_payments.user_id AND o.purchase_id = order_payments.purchase_id)
		  ),
		  method_id = COALESCE(
		    NULLIF(method_id, ''),
		    (SELECT json_extract(o.raw_json, '$.payments[' || order_payments.payment_index || '].method.id')
		       FROM orders o WHERE o.user_id = order_payments.user_id AND o.purchase_id = order_payments.purchase_id),
		    (SELECT json_extract(o.raw_json, '$.payments[' || order_payments.payment_index || '].method_id')
		       FROM orders o WHERE o.user_id = order_payments.user_id AND o.purchase_id = order_payments.purchase_id)
		  )
		WHERE provider IS NULL OR provider = ''
		   OR method_type IS NULL OR method_type = ''
		   OR method_id IS NULL OR method_id = ''
	`); err != nil {
		return err
	}
	// order_items.line_total_minor was always 0 in pre-fix rows because
	// the code read item.line_total (a field Wolt never returns) instead
	// of item.end_amount. Backfill from orders.raw_json using the stored
	// item_index. Falls back to unit_price * quantity when the JSON path
	// also misses (e.g., synthetic / partial rows).
	if _, err := db.ExecContext(ctx, `
		UPDATE order_items
		SET line_total_minor = COALESCE(
		  NULLIF(line_total_minor, 0),
		  (SELECT json_extract(o.raw_json, '$.items[' || order_items.item_index || '].end_amount')
		     FROM orders o
		     WHERE o.user_id = order_items.user_id
		       AND o.purchase_id = order_items.purchase_id),
		  (SELECT json_extract(o.raw_json, '$.items[' || order_items.item_index || '].line_total')
		     FROM orders o
		     WHERE o.user_id = order_items.user_id
		       AND o.purchase_id = order_items.purchase_id),
		  unit_price_minor * COALESCE(NULLIF(quantity, 0), 1),
		  0
		)
		WHERE line_total_minor = 0
	`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `UPDATE orders SET fees_minor = delivery_fee_minor + service_fee_minor WHERE fees_minor <> delivery_fee_minor + service_fee_minor`)
	return err
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

	// Wolt returns most fields flat at the top level of detail —
	// venue_{id,name,country,product_line,address,full_address} and
	// money fields as plain integers — while a few delivery-address fields
	// live under detail.delivery_location.{city,alias,street,apartment}.
	// The nested venue.* / delivery.* / totals.* shape that older code
	// expected only ever existed in our test fixtures. Read flat first,
	// then fall back to nested for backward compatibility with fixtures.
	venue := asMap(detail["venue"])
	delivery := asMap(detail["delivery"])
	deliveryLocation := asMap(detail["delivery_location"])

	venueName := firstNonNilString([]map[string]any{detail, venue, summary}, "venue_name", "name")

	deliveryFee := extractDetailMinor(detail, "delivery_price", "delivery")
	serviceFee := extractDetailMinor(detail, "service_fee", "service_fee")

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
		extractDetailMinor(detail, "total_price", "total"),
		extractDetailMinor(detail, "subtotal", "subtotal"),
		extractDetailMinor(detail, "items_price", "items"),
		deliveryFee,
		serviceFee,
		deliveryFee+serviceFee,
		extractDetailMinor(detail, "credits", "credits"),
		extractDetailMinor(detail, "tokens", "tokens"),
		sumCollectionAmounts(detail["discounts"]),
		sumCollectionAmounts(detail["surcharges"]),
		nullableString(detail["delivery_method"]),
		firstNonNilString([]map[string]any{deliveryLocation, delivery}, "city"),
		firstNonNilString([]map[string]any{deliveryLocation, delivery}, "alias"),
		firstNonNilString([]map[string]any{detail, venue}, "venue_id", "id"),
		venueName,
		firstNonNilString([]map[string]any{detail, venue}, "venue_country", "country"),
		firstNonNilString([]map[string]any{detail, venue}, "venue_full_address", "venue_address", "address"),
		firstNonNilString([]map[string]any{detail, venue}, "venue_product_line", "product_line"),
		itemSummary,
		paymentProvider(primaryPayment),
		paymentMethodType(primaryPayment),
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
			paymentMethodID(payment),
			paymentProvider(payment),
			paymentMethodType(payment),
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
			itemLineTotal(item),
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

// paymentProvider reads payment.method.provider with a fallback to the
// flat payment.provider that older fixtures use. Wolt's real payload
// nests it under method, e.g. payment.method.{provider: "edenred"}.
func paymentProvider(payment map[string]any) interface{} {
	if payment == nil {
		return nil
	}
	if method := asMap(payment["method"]); method != nil {
		if v := nullableString(method["provider"]); v != nil {
			return v
		}
	}
	return nullableString(payment["provider"])
}

// paymentMethodType reads payment.method.type with a fallback to the
// flat payment.method_type.
func paymentMethodType(payment map[string]any) interface{} {
	if payment == nil {
		return nil
	}
	if method := asMap(payment["method"]); method != nil {
		if v := nullableString(method["type"]); v != nil {
			return v
		}
	}
	return nullableString(payment["method_type"])
}

// paymentMethodID reads payment.method.id with a fallback to the flat
// payment.method_id.
func paymentMethodID(payment map[string]any) interface{} {
	if payment == nil {
		return nil
	}
	if method := asMap(payment["method"]); method != nil {
		if v := nullableString(method["id"]); v != nil {
			return v
		}
	}
	return nullableString(payment["method_id"])
}

// itemLineTotal picks the line-total field Wolt actually populates:
// "end_amount" in the real payload, falling back to "line_total" used
// by the legacy fixtures. Both are flat integers in minor units.
func itemLineTotal(item map[string]any) int {
	if item == nil {
		return 0
	}
	if n := extractMinor(item["end_amount"]); n != 0 {
		return n
	}
	return extractMinor(item["line_total"])
}

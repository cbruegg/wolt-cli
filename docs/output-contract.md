# Output contract

Stable shape for machine-readable responses (`--format json` / `--format yaml`).
Table output is shown by default and is not part of this contract.

## Envelope

```json
{
  "meta": {
    "request_id": "req_01j0zdq8q6k7y8d6w2g0y9p4m7",
    "generated_at": "2026-02-19T20:45:09Z",
    "profile": "default",
    "locale": "en-FI"
  },
  "data": {},
  "warnings": []
}
```

YAML uses the same fields. `warnings[]` carries non-fatal upstream issues
(fallback used, address inferred from account, etc.). `data` can be
`null` on errors.

### Errors

```json
{
  "meta": { },
  "data": null,
  "warnings": [],
  "error": {
    "code": "WOLT_AUTH_REQUIRED",
    "message": "Authentication is required. Run \"wolt login\" first.",
    "details": { }
  }
}
```

Stable error codes:

| Code | Meaning |
|---|---|
| `WOLT_AUTH_REQUIRED` | The endpoint requires a logged-in session. |
| `WOLT_INVALID_ARGUMENT` | Flag combination is invalid (e.g. `--lat` without `--lon`). |
| `WOLT_UPSTREAM_ERROR` | Upstream returned a non-success status. `--verbose` adds the URL and status code to the message. |

## Conventions

- IDs are strings (`venue_id`, `item_id`, `basket_id`, `purchase_id`).
- Money: `amount` in minor units (cents) plus an optional `formatted_amount` for display.
- Time: ISO-8601 UTC for CLI-generated stamps; upstream-provided strings are passed through verbatim alongside parsed equivalents when available.
- Booleans are never serialised as strings.

## Schemas

### `wolt status` — Status

```
authenticated: bool
user_id: string
country: string
session_expires_at: string|null  (ISO-8601 UTC)
wolt_plus_subscriber: bool
token_preview?: string           (--verbose only)
cookie_count?: int               (--verbose only)
```

### `wolt account` — ProfileSummary

```
user_id: string
name: string
email_masked: string
phone_masked: string
country: string
```

### `wolt account addresses` — AddressList

```
addresses[]: { address_id, label, street, is_default }
profile_default_address_id: string
```

### `wolt account addresses links` — AddressLinks

```
address_id: string
links: { address_link, entrance_link, coordinates_link }
```

### `wolt account payments` — PaymentMethodList

```
methods[]: { method_id, type, label, is_default, is_available_for_checkout }
```

### `wolt account orders` — OrderHistoryList

```
orders[]: {
  purchase_id, received_at, status, venue_name,
  total_amount, is_active, items_summary,
  payment_time_ts, main_image, main_image_blurhash
}
count: int
next_page_token?: string
status_filter?: string
```

### `wolt account order <id>` — OrderHistoryDetail

```
order_id, status, currency
venue: { id, name, address, phone, country, product_line }
totals: { items, delivery, service_fee, subtotal, credits, tokens, total }
  (each value is { amount, formatted_amount })
items[]: { id, name, count, price, line_total, options }
payments[]: { name, amount, method_type, method_id, provider, payment_time }
delivery: { alias, address, city, comment }

# Optional
order_number, creation_time, delivery_time, delivery_method
discounts[]: { title, amount }
surcharges[]: { title, amount }
```

### `wolt venues` — VenueSearchResult

```
query?: string
total: int
items[]: {
  venue_id, slug, name, address,
  rating, delivery_estimate, delivery_fee,
  price_range, price_range_scale,
  promotions[], wolt_plus
}

# Pagination
count, offset, limit, total_pages, next_offset, page
```

Promotions get enriched with dynamic campaign banners when the upstream
dynamic endpoint is available.

### `wolt venues categories` — CategoryList

```
categories[]: { id, name, slug }
```

### `wolt venue <slug>` — VenueDetail

```
venue_id, slug, name, address, currency
rating, delivery_methods, order_minimum
```

If the restaurant detail endpoint is unavailable, the CLI falls back to
the static venue payload and adds a warning.

### `wolt venue categories <slug>` — VenueCategoryList

```
venue_id: string
loading_strategy: string
categories[]: { id, slug, name, parent_slug, level, leaf, item_refs_count }
```

### `wolt venue menu <slug>` (without `--query`) — VenueMenu

```
venue_id, wolt_plus
categories[]
items[]: { item_id, name, base_price, discounts }

# Optional
items[].original_price          (campaign-adjusted)
items[].option_group_ids        (--include-options)
count, offset, limit, total_pages, next_offset, page, sort
```

### `wolt venue menu <slug> --query <text>` — VenueItemSearchResult

```
venue_id, venue_slug, query, total
items[]: { item_id, name, category, base_price, discounts, is_sold_out }

# Optional
items[].original_price          (upstream pre-discount amount)
items[].option_group_ids        (--include-options)
count, offset, limit, total_pages, next_offset, page, sort
```

When upstream omits the currency, it is normalized from venue metadata.
When `original_price` is present without a promo label, the CLI derives
a synthetic discount (e.g. `21% off`).

### `wolt venue hours <slug>` — VenueHours

```
venue_id: string
timezone: string
opening_windows[]
```

### `wolt venue item <venue> <item-id>` — ItemDetail

```
item_id, venue_id, name, description, price
option_groups[]
upsell_items[]
```

`price.formatted_amount` is normalized from venue metadata when upstream
omits the currency. `upsell_items[].price` follows the same rule.

### `wolt cart` — CartState

```
basket_id, venue_id, venue_name, venue_slug
selection, currency, total_items
lines[]: {
  line_id, item_id, name, count,
  options[], price, line_total
}
subtotal, fees, total
```

### `wolt cart add|remove|clear` — CartMutationResult

```
mutation: "add" | "remove" | "clear"
total_items: int
total: { amount, formatted_amount }

# Per mutation
add:    basket_id, venue_id, line_id, item_name, item_price, item_currency
remove: basket_id, venue_id, line_id, removed_count
clear:  basket_ids[], cleared_baskets
```

### `wolt checkout` — CheckoutPreview

```
basket_id, venue_id, venue_name, venue_slug
selection
payable_amount
checkout_rows[]
delivery_configs[]
offers
tip_config
```

Preview-only. The CLI never calls the order placement endpoint.

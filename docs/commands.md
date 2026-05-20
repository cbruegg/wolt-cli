# Commands

`wolt-cli` exposes eight top-level commands. Every leaf command supports
the same machine output (`--format table|json|yaml`) and the same global
flags listed at the bottom of this page.

| Command | Purpose |
|---|---|
| `wolt login` | Save a Wolt account locally via Chrome cookies or manual tokens. |
| `wolt logout` | Remove saved credentials. |
| `wolt status` | Probe whether the saved account is still authenticated. |
| `wolt account` | Show account profile, orders, addresses, payments, favorites. |
| `wolt feed` | Browse the home-page-style discovery feed grouped by section. |
| `wolt venues` | Browse or search nearby venues as a flat list. |
| `wolt venue` | Inspect one venue: details, menu, hours, items, categories. |
| `wolt cart` | Read or mutate the saved basket draft. |
| `wolt checkout` | Preview the checkout payload (no order placement). |

---

## `wolt login`

```console
wolt login                                  # opens managed Chrome, extracts cookies
wolt login --wtoken <token> --wrtoken <rt>  # manual tokens
wolt login --cookie "__wtoken=<token>"      # cookie-style auth (repeatable)
```

Without token flags, a managed Chrome window is opened against
`http://127.0.0.1:9222` (see `scripts/start-chrome.sh`). After a successful
sign-in on `https://wolt.com/login`, Wolt cookies are extracted via the
Chrome DevTools Protocol, normalized into `wtoken` / `wrefresh_token`,
and saved with `0600` permissions to:

- `$WOLT_CONFIG_PATH` if set, else `~/.wolt/.wolt-config.json`

`--wtoken` accepts raw JWT, `Bearer <jwt>`, JSON `accessToken` payloads,
URL-encoded payloads, and cookie blobs (`__wtoken=<jwt>`). When upstream
returns `401` later, the CLI auto-rotates the access token with the saved
refresh token and persists the new pair.

## `wolt logout`

```console
wolt logout
```

Clears `wtoken`, `wrefresh_token`, `cookies[]`, and the local
`wolt_address_id` pointer from the saved config. Preserves location
preferences. Does not call any Wolt endpoint.

## `wolt status`

```console
wolt status [--verbose]
```

Calls `GET /v1/user/me`. Returns `authenticated`, `user_id`, `country`,
`session_expires_at`, `wolt_plus_subscriber`. Without credentials, returns
`authenticated=false` with a `no auth credentials provided` warning.
`--verbose` adds a token preview and the upstream HTTP trace.

## `wolt account`

```console
wolt account                                # ProfileSummary
wolt account addresses                      # list saved Wolt addresses
wolt account addresses add --address "..." --lat .. --lon ..
wolt account addresses update <id> --address "..." --lat .. --lon ..
wolt account addresses remove <id>
wolt account addresses use <id>             # set local default pointer
wolt account addresses links [id]           # Google Maps validation URLs
wolt account orders [--limit 1-50] [--page-token <t>] [--status <s>]
wolt account order <purchase-id>            # one order detail
wolt account payments [--mask-sensitive]
wolt account favorites
wolt account favorites add <venue|slug|url>
wolt account favorites remove <venue|slug|url>
```

All `account *` subcommands require a logged-in session. Address mutation
endpoints write to `https://restaurant-api.wolt.com/v2/delivery/info`.

## `wolt feed`

```console
wolt feed [--section-limit <n>] [--per-section <n>]
          [--query <text>]
          [--address "<text>" | --lat <f> --lon <f>]
```

Renders the same section structure you see on wolt.com — "Popular near
you", "Order again", "Fastest delivery", "Top-rated", etc. — with
marketing context per row (tagline, top discount offer, rating, ETA).
One upstream call, no per-venue enrichment, sub-3-second.

Each row in JSON carries `name`, `slug`, `tagline`, `top_offer`,
`rating`, `delivery_estimate`, `delivery_fee`, `price_range`,
`promotions`, `wolt_plus`, plus `venue_id` for chaining into `cart add`
or `venue menu`. The default table shows the action-relevant columns
truncated to fit (≤32 chars for tagline, ≤26 for top offer).

`--per-section` caps the per-section rows shown in the table (default 6);
JSON keeps the full upstream slice. `--query` filters venues by name,
tagline, top-offer, or slug across all sections; empty sections drop out.

## `wolt venues`

```console
wolt venues [--query <text>]
            [--type restaurant|grocery|pharmacy|retail]
            [--category <slug>]
            [--sort recommended|distance|rating|delivery_price|delivery_time]
            [--open-now] [--wolt-plus] [--promotions-only]
            [--min-rating <float>] [--max-delivery-fee <minor>]
            [--limit <n>] [--offset <n> | --page <n>]
            [--enrich]
wolt venues categories                       # nearby discovery categories
```

`--query` filters by venue name or slug. Without `--query`, returns
nearby venues. Default table is 8 columns: Venue, Slug, Tagline,
Top offer, Rating, Delivery, Fee, Wolt+ — the tagline (Wolt
`short_description`) and top discount offer come from the same payload,
no extra HTTP. JSON keeps the full payload including `address`,
`promotions`, `price_range_scale`.

**Speed**: by default `venues` does not hit per-venue promotion or
Wolt+ endpoints — one upstream call, sub-second response. Pass
`--enrich` to fetch dynamic campaign banners and resolve Wolt+ for
venues whose flag is missing from the feed payload (slower; bounded by
internal budgets). `--promotions-only` implies `--enrich`.

## `wolt venue`

```console
wolt venue <venue> [--include hours,tags,rating,fees]
wolt venue menu <venue> [--query <text>] [--category <slug>]
                        [--include-options] [--full-catalog]
                        [--sort recommended|price|name]
                        [--min-price <minor>] [--max-price <minor>]
                        [--hide-sold-out] [--discounts-only]
                        [--limit <n>] [--offset <n> | --page <n>]
wolt venue categories <venue>
wolt venue hours <venue> [--timezone <iana>]
wolt venue item <venue> <item-id|url>
wolt venue item <wolt-item-url>              # one-arg: venue read from URL
```

`<venue>` accepts a slug, a 24-char Mongo ObjectID, or a Wolt URL
(e.g. `https://wolt.com/en/fin/helsinki/venue/<slug>`). The CLI extracts
the slug, looks up the venue id when needed, and surfaces both
`venue_id` and `venue_slug` in JSON output.

`venue menu` without `--query` returns the full menu (`VenueMenu`). With
`--query`, it returns a venue-scoped item search (`VenueItemSearchResult`)
— preferred for large marketplace catalogs.

`venue item <venue> <item-id>` shows item detail; `--include-options`
on `venue menu` exposes option-group IDs you can pass to `cart add --option`.

## `wolt cart`

```console
wolt cart [--venue-id <id>] [--details]
wolt cart count
wolt cart add <venue> <item-id|url>
              [--count <n>]
              [--option <group=value[:count]>...]
              [--allow-substitutions]
              [--name <text>] [--price <minor>]
              [--currency <code>]
              [--venue-slug <slug>]
wolt cart add <wolt-item-url>                # one-arg: venue read from URL
wolt cart add <venue> --query "<item name>"  # resolves to item id via menu search
wolt cart remove <item-id|url> [--count <n>] [--all] [--venue-id <id>]
wolt cart clear [--venue-id <id>] [--all]
```

`<item-id>` on `cart add` and `cart remove` accepts either a 24-char
Mongo ObjectID or a Wolt item URL of the form
`https://wolt.com/<locale>/<country>/<city>/venue/<slug>/itemid-<id>`
(`menuitem-<id>` and the same URL with `?itemid=<id>` also work). The
slug embedded in the URL is reused for venue resolution when you
haven't passed one explicitly — `cart add` accepts the URL as a single
argument (no separate `<venue>`) since it carries both pieces.

`cart add --query "<text>"` is a one-shot path that calls the same
assortment item search `venue menu --query` uses, requires a single
match, and errors with a "did you mean…" list when more than one item
matches. Exact-name matches always beat substring hits.

The basket lives in your Wolt account (same draft you see in the Wolt
sidebar). Mutations call `POST /order-xp/v1/baskets` and the bulk-delete
endpoint; no payment or delivery is dispatched from this CLI.

`--option` accepts both IDs and case-insensitive names:

```console
wolt cart add huuva-food-court-niittykumpu 689efcc0dbe125482d2fecb2 \
  --option "Drink=Cola" --option "Side=Fries" --count 1
```

If `--count` is omitted, the API treats the call as "add one of this
line." `--all` on remove/clear targets every basket the user holds.

## `wolt checkout`

```console
wolt checkout [--delivery-mode standard|priority|schedule]
              [--tip <minor>] [--promo-code <id>]
              [--venue-id <id>]
              [--address "<text>" | --lat <f> --lon <f>]
```

Preview-only. Calls `POST /order-xp/web/v2/pages/checkout` and returns
the projected payable amount, checkout rows, delivery configs, and tip
config. Location overrides affect the preview only — real orders use
the Wolt-saved default delivery address.

There is no order-placement command. To place an order, finish in the
Wolt app or web UI.

---

## Global flags

Available on every leaf command:

- `--format table|json|yaml` (default `table`)
- `--address <text>` — temporary address override; cannot combine with `--lat`/`--lon`
- `--locale <bcp-47>` (default `en-FI`)
- `--no-color`
- `--verbose` — prints the upstream HTTP request trace and detailed error envelopes

## On-disk caches

- `~/.wolt/.wolt-config.json` (`0600`) — the saved account.
- `~/.wolt/.wolt-slug-cache.json` (`0600`) — venue slug → id + static-page payload cache, 24 h TTL. Eliminates the ~200–500 ms static-page lookup on repeated `cart add`, `venue menu`, `venue item`, and `checkout` flows against the same venue. Wiped automatically by `wolt logout`. Override the path with `WOLT_SLUG_CACHE_PATH`.

Location-aware commands additionally accept `--lat <float>` and `--lon <float>`
(must be supplied together). If no override is given, the address attached
to the logged-in Wolt account is used.

## Output contract

JSON / YAML responses are wrapped in a stable envelope
(`meta`, `data`, `warnings`, optional `error`). See
[`output-contract.md`](./output-contract.md) for the full schema reference.

## Roadmap

Planned ergonomics improvements (item name / URL resolution, etc.) are
tracked in [`roadmap.md`](./roadmap.md).

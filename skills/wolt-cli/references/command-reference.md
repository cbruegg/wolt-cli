# Command Reference

## Invocation

Tool repository: https://github.com/mekedron/wolt-cli

Open the repository for setup/build details, then use the local binary:

```bash
wolt <group> <command> [flags]
```

Leaf commands share global flags unless noted:

- `--format table|json|yaml`
- `--address "<text>"`
- `--locale <bcp47>`
- `--no-color`
- `--verbose`

`login` is the only credential setup command. It can open managed Chrome or accept manual token flags.

## Root Groups

- `login`
- `logout`
- `status`
- `account`
- `feed`
- `top`
- `venues`
- `venue`
- `cart`
- `checkout`

## Login

- `wolt login`
- `wolt login [--wtoken ...] [--wrtoken ...] [--cookie ...]`
- `wolt logout`
- `wolt status`

## Feed

- `wolt feed [--section-limit <n>] [--per-section <n>] [--query <text>] [--summary] [--show-highlights[=bool]] [--address ... | --lat ... --lon ...]`

Mirrors the wolt.com home page: section-grouped venues with tagline + top discount offer per row. One upstream call, sub-3-second. Sections carry a `kind: "venues" | "brands"` discriminant — brand carousels (Popular stores, Restaurant categories, …) render as a single-line summary. Use `--summary` to collapse the whole feed into one line per section. `--show-highlights` defaults to auto (render iff at least one row has `menu_highlights[]`). `--query` matches against brand names too.

## Top

- `wolt top [N] [--limit <n>] [--offset <n> | --page <n>] [--query <text>] [--wolt-plus] [--show-highlights[=bool]] [--address ... | --lat ... --lon ...]`

Flattens every `kind=venues` section of the discovery feed into a single ranked table, dedupes by `venue_id` preserving upstream order, and trims to N (default 10). The "what should I order right now" shortcut. Same row shape as `wolt venues`.

## Venues

- `wolt venues [--query <text>] [--sort ...] [--type ...] [--category ...] [--open-now] [--wolt-plus] [--promotions-only] [--min-rating <float>] [--max-delivery-fee <minor>] [--enrich] [--show-highlights[=bool]] [--limit <n>] [--offset <n> | --page <n>] [--address ... | --lat ... --lon ...]`

By default `venues` skips per-venue promotion/Wolt+ enrichment (single upstream call, sub-second). Add `--enrich` to fetch dynamic campaign banners and resolve missing Wolt+ flags (slower; capped by internal budget). `--promotions-only` implies `--enrich`. `--sort` accepts both `delivery_time`/`delivery_price` and the hyphenated `delivery-time`/`delivery-price` forms.
- `wolt venues categories [--limit <n>] [--offset <n> | --page <n>] [--address ... | --lat ... --lon ...]`

## Venue

`<venue>` accepts slug, 24-char Mongo ObjectID, or a Wolt URL.

- `wolt venue <venue> [--include hours,tags,rating,fees] [--address ...]`
- `wolt venue categories <venue> [--limit <n>] [--offset <n> | --page <n>]`
- `wolt venue menu <venue> [--query <text>] [--category <slug>] [--full-catalog] [--include-options] [--sort recommended|price|name] [--min-price <minor>] [--max-price <minor>] [--hide-sold-out] [--discounts-only] [--limit <n>] [--offset <n> | --page <n>]`
- `wolt venue hours <venue> [--timezone <iana>] [--address ...]` — falls back to opening windows derived from the static venue payload when the structured restaurant endpoint returns 410.
- `wolt venue item <venue> <item-id|url>` (or `wolt venue item <wolt-item-url>` for the single-arg form)

`venue menu` without `--query` returns the full menu; with `--query` it returns a venue-scoped item search (preferred for large marketplace catalogs). `venue item` includes option metadata so option group/value names can be passed straight to `cart add --option`.

## Cart

- `wolt cart count`
- `wolt cart [--venue-id <id>] [--details] [--address ... | --lat ... --lon ...]`
- `wolt cart add <venue> <item-id|url> [--count <n>] [--option <group=value[:count]> ...] [--allow-substitutions] [--name ...] [--price ...] [--currency ...] [--venue-slug <slug>] [--lat ... --lon ...]`
- `wolt cart add <wolt-item-url>` (single-arg: venue slug read from the URL)
- `wolt cart add <venue> --query "<item name>"` (resolves a unique item by name via the venue menu search; errors on ambiguous matches)
- `wolt cart remove <item-id|url> [--count <n>] [--all] [--venue-id <id>] [--address ... | --lat ... --lon ...]`
- `wolt cart clear [--venue-id <id>] [--all] [--address ... | --lat ... --lon ...]`

`<venue>` accepts slug, hex ID, or Wolt URL (same as `venue`). `<item-id>` on `cart add`/`cart remove` and `venue item` accepts a 24-char Mongo ObjectID or a Wolt item URL (`.../venue/<slug>/itemid-<id>`, `menuitem-<id>`, or `?itemid=<id>`). `--option` accepts both IDs and case-insensitive names (e.g. `--option "Drink=Cola"`). If multiple baskets exist and no `--venue-id` is passed, commands select the first basket.

## Checkout

- `wolt checkout [--delivery-mode standard|priority|schedule] [--tip <minor-units>] [--promo-code <id>] [--venue-id <id>] [--address ... | --lat ... --lon ...]`

Preview only. No final order placement.

## Account

- `wolt account [--include personal,settings]`
- `wolt account orders [--limit 1-50] [--page-token <token>] [--status <value>]`
- `wolt account order <purchase-id>`
- `wolt account payments [--label <contains>] [--mask-sensitive]`
- `wolt account addresses [--active-only]`
- `wolt account addresses add --address ... --lat ... --lon ... [--type ...] [--label ...] [--alias ...] [--detail key=value ...] [--set-default-profile]`
- `wolt account addresses update <address-id> --address ... --lat ... --lon ... [--type ...] [--label ...] [--alias ...] [--detail key=value ...] [--set-default-profile]`
- `wolt account addresses remove <address-id>`
- `wolt account addresses use <address-id>`
- `wolt account addresses links [address-id]`
- `wolt account favorites [--limit <n>] [--offset <n> | --page <n>] [--address ... | --lat ... --lon ...]`
- `wolt account favorites add <venue-id-or-slug>`
- `wolt account favorites remove <venue-id-or-slug>`

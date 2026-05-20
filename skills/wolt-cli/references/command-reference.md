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
- `venues`
- `venue`
- `cart`
- `checkout`

## Login

- `wolt login`
- `wolt login [--wtoken ...] [--wrtoken ...] [--cookie ...]`
- `wolt logout`
- `wolt status`

## Venues

- `wolt venues [--query <text>] [--sort ...] [--type ...] [--category ...] [--open-now] [--wolt-plus] [--promotions-only] [--min-rating <float>] [--max-delivery-fee <minor>] [--enrich] [--limit <n>] [--offset <n> | --page <n>] [--address ... | --lat ... --lon ...]`

By default `venues` skips per-venue promotion/Wolt+ enrichment (single upstream call, sub-second). Add `--enrich` to fetch dynamic campaign banners and resolve missing Wolt+ flags (slower; capped by internal budget). `--promotions-only` implies `--enrich`.
- `wolt venues categories [--address ... | --lat ... --lon ...]`

## Venue

`<venue>` accepts slug, 24-char Mongo ObjectID, or a Wolt URL.

- `wolt venue <venue> [--include hours,tags,rating,fees] [--address ...]`
- `wolt venue categories <venue>`
- `wolt venue menu <venue> [--query <text>] [--category <slug>] [--full-catalog] [--include-options] [--sort recommended|price|name] [--min-price <minor>] [--max-price <minor>] [--hide-sold-out] [--discounts-only] [--limit <n>] [--offset <n> | --page <n>]`
- `wolt venue hours <venue> [--timezone <iana>] [--address ...]`
- `wolt venue item <venue> <item-id>`

`venue menu` without `--query` returns the full menu; with `--query` it returns a venue-scoped item search (preferred for large marketplace catalogs). `venue item` includes option metadata so option group/value names can be passed straight to `cart add --option`.

## Cart

- `wolt cart count`
- `wolt cart [--venue-id <id>] [--details] [--address ... | --lat ... --lon ...]`
- `wolt cart add <venue> <item-id> [--count <n>] [--option <group=value[:count]> ...] [--allow-substitutions] [--name ...] [--price ...] [--currency ...] [--venue-slug <slug>] [--lat ... --lon ...]`
- `wolt cart remove <item-id> [--count <n>] [--all] [--venue-id <id>] [--address ... | --lat ... --lon ...]`
- `wolt cart clear [--venue-id <id>] [--all] [--address ... | --lat ... --lon ...]`

`<venue>` on `cart add` accepts slug, hex ID, or Wolt URL (same as `venue`). `--option` accepts both IDs and case-insensitive names (e.g. `--option "Drink=Cola"`). If multiple baskets exist and no `--venue-id` is passed, commands select the first basket.

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
- `wolt account favorites [--address ... | --lat ... --lon ...]`
- `wolt account favorites add <venue-id-or-slug>`
- `wolt account favorites remove <venue-id-or-slug>`

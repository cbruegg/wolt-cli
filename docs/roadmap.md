# Roadmap

Ideas and features that aren't built yet but make sense for the
single-account, action-oriented direction the CLI took during the
simplification pass. Listed roughly in usefulness order. None of these
are scheduled — open an issue or send a PR if you want one prioritized.

## ~~Resolve items the same way we resolve venues~~ — shipped

`wolt cart add`, `wolt cart remove`, and `wolt venue item` now accept a
24-char Mongo ObjectID **or** a Wolt item URL of the form
`.../venue/<slug>/itemid-<id>` (`menuitem-<id>` and `?itemid=<id>` also
work). When the URL carries the venue slug, the venue argument benefits
from it for free.

`wolt cart add <venue> --query "<name>"` resolves an item by name via
the venue assortment search (same endpoint `wolt venue menu --query`
uses) and accepts only a unique match. Ambiguous queries return a
"matched N items in <venue>" error with the top five candidate
`Name (item-id)` pairs. Exact-name matches always beat substring hits.

Single-arg URL form ships too: `wolt cart add <wolt-item-url>` and
`wolt venue item <wolt-item-url>` accept just the URL since it carries
both the slug and the item id. Falls back to the explicit
`<venue> <item-id>` shape when the single arg is not a URL with both
parts.

Still open from this work:

- **Local cache of `slug → id`** so repeated `--query` and URL-driven
  flows skip the static venue lookup that resolves the venue id.

## Discovery enrichment beyond tagline + top offer

`wolt feed` and `wolt venues` already surface `tagline` (from upstream
`short_description`) and `top_offer` (preferring discount-variant
promos). What's still missing from the same payload that we could
surface without extra HTTP:

- **Menu preview items** when populated (`venue_preview_items`) — most
  useful for sponsored / featured rows, often shows a flagship dish.
- **`badges_v2`** with icons — currently we only read the legacy
  `badges` array; the newer payload field carries iconographic hints
  ("coupon-fill", "wolt-plus") we could render as ASCII prefixes.
- **Brand carousels** ("Popular stores", "Brands" sections on the home
  page) — currently `feed` skips non-venue sections; could render them
  as a compact "Brands: K-Market · Musti ja Mirri · ..." line.
- **Grocery deals** section — distinct shape (product-card carousel
  with old/new prices), would need its own row template.

## Smarter `venue menu` discovery

- **`--show-options` flag on `venue menu`** that prints the option matrix
  inline for each row (currently you need a separate `venue item` call
  per item).
- **`venue menu --category-tree`** that prints the category hierarchy
  as a single tree instead of requiring two calls
  (`venue categories` then `venue menu --category`).

## Place orders (gated, opt-in)

Right now the CLI is read-only for purchasing on purpose. Adding a
`wolt checkout place` command would require:

- Explicit `--i-really-want-to-pay` style confirmation.
- A locked-in delivery address (no `--lat`/`--lon`/`--address` overrides).
- A separate config flag to enable the command at all (off by default).
- A dry-run test harness that intercepts the placement call.

Until that scaffold exists, the CLI stops at `checkout` preview.

## `wolt cart add` from a recent order

```
wolt account order <purchase-id> --re-add
```

Re-create a basket from a historical order: walk `items[]` from
`OrderHistoryDetail` and call `cart add` for each line with matching
options. Useful for re-ordering the same meal without scrolling the app.

## Shell completions and `wolt help` polish

- Generate bash / zsh / fish completions via cobra and ship them with
  the Homebrew tap formula.
- Make `wolt help <command>` open the section of `docs/commands.md`
  matching the requested command, for richer offline reference.

## Better default location handling

- Auto-detect when the saved Wolt account address is stale (the venue
  search returns "no results in your area") and prompt the user with a
  one-liner to update via `wolt account addresses use <id>`.
- `wolt venues --here` that geocodes the current OS location instead of
  needing `--address`.

## Multi-account opt-in (only if it has a real use case)

The simplification removed multi-profile support deliberately — most
users have one Wolt account. If a real workflow requires more (e.g.
testing tenants), reintroduce it as **explicit `wolt --account <name>
…`** rather than a hidden global flag, with the existing
`internal/config/store.go` migration preserved for backwards-compat.

## Tests we still want

These aren't features but they belong on the roadmap:

- An e2e test that drives the real CDP login path using a recorded
  WebSocket cassette (today we mock the protocol but not a captured
  real-world cookie payload).
- A `cart add` test that exercises `--option` resolution by **name**
  end-to-end (`resolveOptionValueToken` is unit-tested in helpers but
  the full command path with mocked Wolt API isn't).
- Snapshot tests for table output so column-trim regressions surface.

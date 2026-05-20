# Roadmap

Ideas and features that aren't built yet but make sense for the
single-account, action-oriented direction the CLI took during the
simplification pass. Listed roughly in usefulness order. None of these
are scheduled — open an issue or send a PR if you want one prioritized.

## Resolve items the same way we resolve venues

Today `wolt cart add <venue> <item-id>` requires a 24-char Mongo hex ID.
The venue argument already accepts a slug, an ID, or a full Wolt URL
(`internal/cli/venue_reference.go`). Items should follow suit:

- **Item URLs.** Parse `https://wolt.com/<locale>/<country>/<city>/venue/<slug>/itemid-<id>` and extract `<id>`. Pure string work, no extra API call.
- **`--query` for cart add.** Skip the menu lookup step:

  ```
  wolt cart add huuva-food-court-niittykumpu --query "double cheese smash"
  ```

  Resolve the unique item by name from the same menu endpoint
  `wolt venue menu` already hits. Error with a "did you mean…" list when
  ambiguous; refuse to guess.
- **Symmetric input for `cart remove` and `venue item`.** Once item URL/name resolution exists, expose it in every command that currently demands a raw item ID.

Open questions: how to disambiguate name collisions (Wolt sometimes has
two identical item names under different categories); whether to cache
the resolved ID locally to avoid repeated menu fetches.

## `cart add --from-url <wolt-item-url>`

A one-shot flow for the most common workflow ("I found something I want
in the app, give me the CLI command"). Parse the URL, derive both venue
and item, and add. With explicit per-flag overrides for count and
options:

```
wolt cart add --from-url https://wolt.com/.../venue/<slug>/itemid-<id> \
  --option "Drink=Cola" --count 2
```

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

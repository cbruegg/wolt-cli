# Discovery enrichment beyond tagline + top offer

Detailed plan for surfacing the fields we already fetch in the same
`Sections()` call that powers `wolt feed` and `wolt venues`. No extra
HTTP — only widening what we render from the existing payload.

See `docs/roadmap.md` ("Discovery enrichment beyond tagline + top
offer") for the high-level entry.

## What we already pull

- `tagline` (from upstream `short_description` /
  `short_description_v2.value`).
- `top_offer` (preferring discount-variant entries from
  `promotions[]`).

Both are rendered as columns on `wolt venues` rows and `wolt feed`
sections.

## What we still drop on the floor

### 1. `venue_preview_items`

Flagship dish + price per sponsored/featured row, e.g.
"Cheeseburger pizza 19,90 €".

- Add a `menu_highlights[]` field to venue rows in
  `BuildDiscoveryFeed` and `BuildVenueSearchResult`.
- Default table: append one column **Highlights** (truncated).
- Cleanest visual win, ships first.

### 2. `badges_v2`

Newer badge format that carries an icon name
(`coupon-fill`, `bike`, `wolt-plus`) and a variant. Today we only
read the legacy `badges` array.

- Surface as ASCII glyphs in the **Venue** cell, e.g.
  `◯ Wolt+`, `% 20% off`, `⚡ Fast`.
- Falls back gracefully when the field is empty — no extra column.

### 3. Brand carousels

`feed` currently skips sections whose items have no venue block —
that drops "Popular stores", "Brands", and similar carousels.

- Render them as a single one-line row per section:
  `Brands: K-Market · Musti ja Mirri · …`.
- Same one-line treatment for any future non-venue carousel.

### 4. Grocery deals section — deferred (no upstream sample)

Speculative when written; the live `/v1/pages/front` endpoint that
`wolt feed` consumes does **not** carry a product-card grocery-deals
section in any sampled city (Helsinki, Warsaw, Krakow, Berlin, Prague,
Athens, Tokyo, May 2026). The payload-wide template enum is
`{hero, medium, square-title-bottom, venue, quicklink}` and the keys
`old_price` / `new_price` / `original_price` / `discounted_price`
don't appear anywhere in the response.

Hypotheses for where the section actually lives:
- A different endpoint — possibly `/v1/pages/grocery-deals` or a
  Wolt-Market-specific page (the brand carousel at
  `woltmarket-popular-brands:helsinki` hints at this).
- Behind authentication — segmented to users with grocery order
  history.
- Removed / not yet shipped.

Not implementing until a real sample payload exists. To revive: capture
a populated payload, drop it under
`internal/service/observability/testdata/feed-grocery-deals.json`, and
revisit the design here. #2.1 (`venue_preview_items` + `badges_v2`) and
#2.2 (brand carousels) ship without it and stand on their own.

## Scope and commit slicing

- **#2.1** — `venue_preview_items` + `badges_v2` (shipped, commit
  `4d5cafa`).
- **#2.2** — Brands one-liner (shipped, commit `ce4a041`).
- **#2.3** — Grocery deals — deferred, see section 4 above.

## Tests

- Each enrichment field gets a unit test with a synthetic payload.
- Brand-section test asserts the one-line render.
- Grocery deals get an e2e snapshot of the new mini-table.

## Risks

- **Width.** Adding columns to an already-wide `venues` table starts
  to wrap on narrow terminals. May need to make the **Highlights**
  column opt-in via a `--show-highlights` flag rather than default.
- **Icon rendering.** ASCII glyphs (`◯`, `%`, `⚡`) render
  inconsistently across terminals. If users hit boxes, fall back to
  plain text prefixes (`[Wolt+]`, `[20% off]`, `[Fast]`).

## Output-contract impact

If shipped, the JSON/YAML schemas in `docs/output-contract.md` grow
new optional fields on the venue row:

```
menu_highlights[]: { name, formatted_price }
badges[]: { icon, variant, text }     # was string[] in the legacy shape
```

And a new section type for grocery deals on `wolt feed`:

```
sections[].kind: "venues" | "brands" | "grocery_deals"
sections[].brands[]: { name, slug }              # kind = "brands"
sections[].deals[]: { item_name, old_price, new_price, venue_name, venue_slug }
                                                  # kind = "grocery_deals"
```

These are additive — existing consumers keep working.

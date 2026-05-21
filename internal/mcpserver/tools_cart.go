package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

func registerCartTools(srv *mcp.Server, tc *ToolCtx) {
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true}
	destructiveFalse := false
	mutateAdd := &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructiveFalse, IdempotentHint: false}
	destructiveTrue := true
	mutateRemove := &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructiveTrue, IdempotentHint: true}

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_cart_show",
		Title:       "Show baskets",
		Description: "Return the user's current baskets (one per venue) with item lines, prices, and totals.",
		Annotations: readOnly,
	}, tc.handleCartShow)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_cart_count",
		Title:       "Total items in baskets",
		Description: "Return the total number of items across all the user's baskets. Cheap one-call status check.",
		Annotations: readOnly,
	}, tc.handleCartCount)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_cart_add",
		Title:       "Add an item to a basket",
		Description: "Add an item to a venue's basket. Merges with existing basket items (does not replace them). If price is omitted, looks the item up via the menu and uses its current price.",
		Annotations: mutateAdd,
	}, tc.handleCartAdd)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_cart_remove",
		Title:       "Remove an item from a basket",
		Description: "Remove a single item line from a venue's basket. Count defaults to removing all of that item; pass count to remove a specific quantity.",
		Annotations: mutateRemove,
	}, tc.handleCartRemove)

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "wolt_cart_clear",
		Title:       "Clear all baskets",
		Description: "Delete every basket the user has. Irreversible — confirm with the user before calling.",
		Annotations: mutateRemove,
	}, tc.handleCartClear)
}

// ---------------- wolt_cart_show ----------------

type CartShowInput struct {
	LocationInput
}
type CartShowOutput struct {
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}

func (tc *ToolCtx) handleCartShow(ctx context.Context, _ *mcp.CallToolRequest, in CartShowInput) (*mcp.CallToolResult, CartShowOutput, error) {
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, CartShowOutput{}, toolErr(err)
	}
	loc, _, err := tc.resolveLocation(ctx, in.LocationInput)
	if err != nil {
		return nil, CartShowOutput{}, toolErr(err)
	}
	payload, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.BasketsPage(ctx, loc, a)
	})
	if err != nil {
		return nil, CartShowOutput{}, toolErr(err)
	}
	count := len(asSlice(coalesceAny(payload["baskets"], payload["results"])))
	return nil, CartShowOutput{
		Summary: humanCount(count, "basket", "baskets"),
		Data:    payload,
	}, nil
}

// ---------------- wolt_cart_count ----------------

type CartCountInput struct{}
type CartCountOutput struct {
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data"`
}

func (tc *ToolCtx) handleCartCount(ctx context.Context, _ *mcp.CallToolRequest, _ CartCountInput) (*mcp.CallToolResult, CartCountOutput, error) {
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, CartCountOutput{}, toolErr(err)
	}
	payload, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.BasketCount(ctx, a)
	})
	if err != nil {
		return nil, CartCountOutput{}, toolErr(err)
	}
	total := asInt(coalesceAny(payload["count"], payload["total"]))
	return nil, CartCountOutput{
		Summary: humanCount(total, "item", "items") + " across all baskets",
		Data:    payload,
	}, nil
}

// ---------------- wolt_cart_add ----------------

type CartAddInput struct {
	LocationInput
	Venue              string `json:"venue"               jsonschema:"venue slug, id, or url"`
	ItemID             string `json:"item_id"             jsonschema:"24-char item id (see wolt_venue_menu)"`
	Count              int    `json:"count,omitempty"     jsonschema:"how many to add (default 1)"`
	Price              int    `json:"price,omitempty"     jsonschema:"item price in minor units (auto-detected from menu if omitted)"`
	Currency           string `json:"currency,omitempty"  jsonschema:"3-letter currency (auto-detected if omitted)"`
	Name               string `json:"name,omitempty"      jsonschema:"item display name (auto-detected if omitted)"`
	AllowSubstitutions bool   `json:"allow_substitutions,omitempty" jsonschema:"if true, the venue may substitute equivalent items"`
}
type CartAddOutput struct {
	Summary  string         `json:"summary"`
	VenueID  string         `json:"venue_id"`
	ItemID   string         `json:"item_id"`
	Added    int            `json:"added"`
	Response map[string]any `json:"response,omitempty"`
}

func (tc *ToolCtx) handleCartAdd(ctx context.Context, _ *mcp.CallToolRequest, in CartAddInput) (*mcp.CallToolResult, CartAddOutput, error) {
	if strings.TrimSpace(in.Venue) == "" || strings.TrimSpace(in.ItemID) == "" {
		return nil, CartAddOutput{}, toolErrf("venue and item_id are required")
	}
	count := in.Count
	if count <= 0 {
		count = 1
	}
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, CartAddOutput{}, toolErr(err)
	}
	ref, err := tc.resolveVenueRef(ctx, in.Venue)
	if err != nil {
		return nil, CartAddOutput{}, toolErr(err)
	}
	if ref.ID == "" {
		return nil, CartAddOutput{}, toolErrf("could not resolve venue id for %q", in.Venue)
	}

	// Resolve price/name from the item page if not provided.
	price := in.Price
	currency := strings.TrimSpace(in.Currency)
	name := strings.TrimSpace(in.Name)
	if price <= 0 || currency == "" || name == "" {
		itemPayload, itemErr := tc.wolt.VenueItemPage(ctx, ref.ID, in.ItemID)
		if itemErr != nil {
			return nil, CartAddOutput{}, toolErr(fmt.Errorf("item lookup failed (pass price/currency/name explicitly to skip): %w", itemErr))
		}
		if price <= 0 {
			price = asInt(asMap(itemPayload["price"])["amount"])
		}
		if currency == "" {
			currency = asString(asMap(itemPayload["price"])["currency"])
		}
		if name == "" {
			name = asString(coalesceAny(itemPayload["name"], itemPayload["title"]))
		}
	}
	if price <= 0 {
		return nil, CartAddOutput{}, toolErrf("could not determine item price; pass `price` in minor units")
	}
	if currency == "" {
		currency = "EUR"
	}

	loc, _, err := tc.resolveLocation(ctx, in.LocationInput)
	if err != nil {
		return nil, CartAddOutput{}, toolErr(err)
	}

	newLine := map[string]any{
		"id":      in.ItemID,
		"count":   count,
		"name":    name,
		"price":   price,
		"options": []any{},
		"substitution_settings": map[string]any{
			"is_allowed": in.AllowSubstitutions,
		},
	}

	// Fetch existing basket to preserve other lines (Wolt's AddToBasket replaces
	// the items array wholesale).
	mergedItems := []any{newLine}
	if existingPage, e := tc.wolt.BasketsPage(ctx, loc, auth); e == nil {
		if basket := selectBasketForVenue(existingPage, ref.ID); basket != nil {
			mergedItems = mergeBasketItems(basket, in.ItemID, count, newLine)
		}
	}

	addPayload := map[string]any{
		"items":    mergedItems,
		"venue_id": ref.ID,
		"currency": currency,
	}
	resp, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.AddToBasket(ctx, addPayload, a)
	})
	if err != nil {
		return nil, CartAddOutput{}, toolErr(err)
	}
	return nil, CartAddOutput{
		Summary:  fmt.Sprintf("added %d × %s to %s basket", count, firstNonEmpty(name, in.ItemID), ref.ID),
		VenueID:  ref.ID,
		ItemID:   in.ItemID,
		Added:    count,
		Response: resp,
	}, nil
}

// ---------------- wolt_cart_remove ----------------

type CartRemoveInput struct {
	LocationInput
	Venue  string `json:"venue"   jsonschema:"venue slug, id, or url"`
	ItemID string `json:"item_id" jsonschema:"item id to remove"`
	Count  int    `json:"count,omitempty" jsonschema:"how many to remove (default: all of that item)"`
}
type CartRemoveOutput struct {
	Summary  string         `json:"summary"`
	Removed  int            `json:"removed"`
	Response map[string]any `json:"response,omitempty"`
}

func (tc *ToolCtx) handleCartRemove(ctx context.Context, _ *mcp.CallToolRequest, in CartRemoveInput) (*mcp.CallToolResult, CartRemoveOutput, error) {
	if strings.TrimSpace(in.Venue) == "" || strings.TrimSpace(in.ItemID) == "" {
		return nil, CartRemoveOutput{}, toolErrf("venue and item_id are required")
	}
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, CartRemoveOutput{}, toolErr(err)
	}
	ref, err := tc.resolveVenueRef(ctx, in.Venue)
	if err != nil {
		return nil, CartRemoveOutput{}, toolErr(err)
	}
	loc, _, err := tc.resolveLocation(ctx, in.LocationInput)
	if err != nil {
		return nil, CartRemoveOutput{}, toolErr(err)
	}

	existingPage, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.BasketsPage(ctx, loc, a)
	})
	if err != nil {
		return nil, CartRemoveOutput{}, toolErr(err)
	}
	basket := selectBasketForVenue(existingPage, ref.ID)
	if basket == nil {
		return nil, CartRemoveOutput{}, toolErrf("no basket found for venue %s", ref.ID)
	}
	currency := strings.TrimSpace(asString(coalesceAny(basket["currency"], asMap(basket["total_price"])["currency"])))
	if currency == "" {
		currency = "EUR"
	}

	removed := 0
	remainingItems := make([]any, 0)
	for _, raw := range asSlice(basket["items"]) {
		line := asMap(raw)
		if line == nil {
			continue
		}
		lineID := strings.TrimSpace(asString(line["id"]))
		lineCount := asInt(line["count"])
		if !strings.EqualFold(lineID, in.ItemID) {
			remainingItems = append(remainingItems, buildBasketUpsertItem(line, lineCount))
			continue
		}
		// match
		switch {
		case in.Count <= 0 || in.Count >= lineCount:
			removed += lineCount
			// drop line entirely
		default:
			removed += in.Count
			remainingItems = append(remainingItems, buildBasketUpsertItem(line, lineCount-in.Count))
		}
	}
	if removed == 0 {
		return nil, CartRemoveOutput{}, toolErrf("item %s not in basket for venue %s", in.ItemID, ref.ID)
	}

	if len(remainingItems) == 0 {
		// Empty cart → delete the basket entirely.
		basketID := strings.TrimSpace(asString(coalesceAny(basket["id"], basket["basket_id"])))
		if basketID == "" {
			return nil, CartRemoveOutput{}, toolErrf("internal: could not locate basket id")
		}
		resp, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
			return tc.wolt.DeleteBaskets(ctx, []string{basketID}, a)
		})
		if err != nil {
			return nil, CartRemoveOutput{}, toolErr(err)
		}
		return nil, CartRemoveOutput{
			Summary:  fmt.Sprintf("removed %d × %s (basket now empty, deleted)", removed, in.ItemID),
			Removed:  removed,
			Response: resp,
		}, nil
	}

	payload := map[string]any{
		"items":    remainingItems,
		"venue_id": ref.ID,
		"currency": currency,
	}
	resp, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.AddToBasket(ctx, payload, a)
	})
	if err != nil {
		return nil, CartRemoveOutput{}, toolErr(err)
	}
	return nil, CartRemoveOutput{
		Summary:  fmt.Sprintf("removed %d × %s from basket", removed, in.ItemID),
		Removed:  removed,
		Response: resp,
	}, nil
}

// ---------------- wolt_cart_clear ----------------

type CartClearInput struct{}
type CartClearOutput struct {
	Summary string `json:"summary"`
	Deleted int    `json:"deleted"`
}

func (tc *ToolCtx) handleCartClear(ctx context.Context, _ *mcp.CallToolRequest, _ CartClearInput) (*mcp.CallToolResult, CartClearOutput, error) {
	_, auth, err := tc.requireAuth(ctx)
	if err != nil {
		return nil, CartClearOutput{}, toolErr(err)
	}
	// Use zero-location BasketsPage just to enumerate baskets; the lat/lon is
	// only used by the upstream for delivery-fee calc.
	loc := lookupProfileLocation(ctx, tc)
	page, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.BasketsPage(ctx, loc, a)
	})
	if err != nil {
		return nil, CartClearOutput{}, toolErr(err)
	}
	baskets := asSlice(coalesceAny(page["baskets"], page["results"]))
	if len(baskets) == 0 {
		return nil, CartClearOutput{Summary: "no baskets to clear", Deleted: 0}, nil
	}
	ids := make([]string, 0, len(baskets))
	for _, raw := range baskets {
		basket := asMap(raw)
		id := strings.TrimSpace(asString(coalesceAny(basket["id"], basket["basket_id"])))
		if id != "" {
			ids = append(ids, id)
		}
	}
	if _, err := invokeWithRefresh(ctx, tc, &auth, func(a woltgateway.AuthContext) (map[string]any, error) {
		return tc.wolt.DeleteBaskets(ctx, ids, a)
	}); err != nil {
		return nil, CartClearOutput{}, toolErr(err)
	}
	return nil, CartClearOutput{
		Summary: fmt.Sprintf("deleted %d basket(s)", len(ids)),
		Deleted: len(ids),
	}, nil
}

// ---------------- cart helpers ----------------

func selectBasketForVenue(page map[string]any, venueID string) map[string]any {
	for _, raw := range asSlice(coalesceAny(page["baskets"], page["results"])) {
		basket := asMap(raw)
		if basket == nil {
			continue
		}
		venue := asMap(basket["venue"])
		if strings.EqualFold(strings.TrimSpace(asString(venue["id"])), venueID) {
			return basket
		}
	}
	return nil
}

func mergeBasketItems(basket map[string]any, addedItemID string, addedCount int, newLine map[string]any) []any {
	existing := asSlice(basket["items"])
	out := make([]any, 0, len(existing)+1)
	merged := false
	for _, raw := range existing {
		line := asMap(raw)
		if line == nil {
			continue
		}
		lineID := strings.TrimSpace(asString(line["id"]))
		lineCount := asInt(line["count"])
		if lineCount <= 0 {
			lineCount = 1
		}
		if !merged && strings.EqualFold(lineID, addedItemID) {
			out = append(out, buildBasketUpsertItem(line, lineCount+addedCount))
			merged = true
			continue
		}
		out = append(out, buildBasketUpsertItem(line, lineCount))
	}
	if !merged {
		out = append(out, newLine)
	}
	return out
}

func buildBasketUpsertItem(line map[string]any, count int) map[string]any {
	options := asSlice(line["options"])
	if options == nil {
		options = []any{}
	}
	subs := asMap(line["substitution_settings"])
	if subs == nil {
		subs = map[string]any{"is_allowed": false}
	}
	return map[string]any{
		"id":                    asString(line["id"]),
		"count":                 count,
		"name":                  asString(line["name"]),
		"price":                 asInt(line["price"]),
		"options":               options,
		"substitution_settings": subs,
	}
}

// lookupProfileLocation returns the saved profile location if any, else zero.
// Good enough for BasketsPage, which only uses lat/lon for delivery-fee calc.
func lookupProfileLocation(ctx context.Context, tc *ToolCtx) domain.Location {
	profile, err := tc.loadProfile(ctx)
	if err != nil {
		return domain.Location{}
	}
	return profile.Location
}

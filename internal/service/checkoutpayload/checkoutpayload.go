package checkoutpayload

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

// Build constructs the current Wolt web checkout preview payload.
func Build(
	ctx context.Context,
	wolt woltgateway.API,
	basket map[string]any,
	location domain.Location,
	deliveryMode string,
	tip int,
	promoCode string,
) (map[string]any, []string, error) {
	deliveryMode = strings.ToLower(strings.TrimSpace(deliveryMode))
	if deliveryMode == "" {
		deliveryMode = "standard"
	}
	if deliveryMode != "standard" && deliveryMode != "priority" && deliveryMode != "schedule" {
		return nil, nil, fmt.Errorf("unsupported delivery mode %q", deliveryMode)
	}

	venue := asMap(basket["venue"])
	venueID := strings.TrimSpace(asString(venue["id"]))
	currency := inferCurrency(asString(basket["total"]))
	if currency == "" {
		currency = strings.TrimSpace(asString(coalesceAny(basket["currency"], asMap(basket["total_price"])["currency"])))
	}
	if currency == "" {
		currency = "EUR"
	}
	country := strings.TrimSpace(asString(venue["country"]))
	warnings := []string{}
	itemDetails := map[string]map[string]any{}
	categoryIDsByItemID := map[string]string{}
	assortmentPayload := map[string]any{}

	venueSlug := resolveBasketVenueSlug(venue)
	if venueSlug != "" && wolt != nil {
		if payload, err := wolt.AssortmentByVenueSlug(ctx, venueSlug); err == nil {
			assortmentPayload = payload
			mergeCheckoutCategoryIndexes(categoryIDsByItemID, buildCheckoutCategoryIDIndex(payload))
		} else {
			warnings = append(warnings, fmt.Sprintf("unable to load venue assortment payload for category mapping (slug=%s)", venueSlug))
		}
		if payload, err := wolt.VenuePageStatic(ctx, venueSlug); err == nil {
			mergeCheckoutCategoryIndexes(categoryIDsByItemID, buildCheckoutCategoryIDIndex(payload))
		}
	}

	menuItems := make([]any, 0, len(asSlice(basket["items"])))
	for _, value := range asSlice(basket["items"]) {
		item := asMap(value)
		itemID := strings.TrimSpace(asString(item["id"]))
		count := asInt(item["count"])
		if count <= 0 {
			count = 1
		}
		price := asInt(item["price"])
		if price <= 0 {
			return nil, warnings, fmt.Errorf("unable to resolve base_price for basket item %q", itemID)
		}

		detail := map[string]any{}
		if itemID != "" && wolt != nil {
			if cached, ok := itemDetails[itemID]; ok {
				detail = cached
			} else if payload, err := wolt.VenueItemPage(ctx, venueID, itemID); err == nil {
				detail = payload
				itemDetails[itemID] = payload
				mergeCheckoutCategoryIndexes(categoryIDsByItemID, buildCheckoutCategoryIDIndex(payload))
			} else if len(assortmentPayload) > 0 {
				detail = assortmentPayload
				itemDetails[itemID] = assortmentPayload
			} else {
				warnings = append(warnings, fmt.Sprintf("unable to enrich checkout payload for item %s; using basket defaults", itemID))
			}
		}

		categoryID := resolveCheckoutCategoryID(item, detail, itemID, categoryIDsByItemID)
		if categoryID == "" {
			if domain.LooksLikeObjectID(itemID) {
				categoryID = itemID
				warnings = append(warnings, fmt.Sprintf("unable to resolve category_id for item %s; falling back to item id", itemID))
			} else {
				return nil, warnings, fmt.Errorf("unable to resolve category_id for basket item %q", itemID)
			}
		}
		categoryIDs := resolveCheckoutCategoryIDs(item, categoryID)
		valuePrices := buildOptionValuePriceIndex(detail)
		options := buildCheckoutOptions(item["options"], valuePrices)

		menuItems = append(menuItems, map[string]any{
			"id":                                itemID,
			"count":                             count,
			"options":                           options,
			"base_price":                        price,
			"end_amount":                        count * price,
			"category_id":                       categoryID,
			"category_ids":                      categoryIDs,
			"exclude_from_credits":              asBool(coalesceAny(item["exclude_from_credits"], false)),
			"exclude_from_discounts":            asBool(coalesceAny(item["exclude_from_discounts"], false)),
			"exclude_from_discounts_min_basket": asBool(coalesceAny(item["exclude_from_discounts_min_basket"], false)),
			"alcohol_permille":                  asInt(coalesceAny(item["alcohol_permille"], 0)),
			"restrictions":                      coalesceAny(item["restrictions"], []any{}),
		})
	}

	purchasePlan := map[string]any{
		"courier_tip": tip,
		"delivery": map[string]any{
			"delivery_coordinates": map[string]any{
				"longitude": location.Lon,
				"latitude":  location.Lat,
			},
		},
		"delivery_method": "homedelivery",
		"delivery_config": map[string]any{
			"method":    "homedelivery",
			"schedule":  deliveryMode,
			"time_slot": nil,
		},
		"payment_methods":           []any{},
		"menu_items":                menuItems,
		"selected_offer_ids":        []any{},
		"use_cash":                  false,
		"use_credits_and_tokens":    true,
		"use_loyalty_points_amount": 0,
		"use_promo_surcharge_ids":   []any{},
		"venue": map[string]any{
			"id":       venueID,
			"country":  country,
			"currency": currency,
		},
	}
	if strings.TrimSpace(promoCode) != "" {
		purchasePlan["use_promo_discount_ids"] = []any{strings.TrimSpace(promoCode)}
	}
	if deliveryMode == "priority" {
		purchasePlan["is_priority_delivery"] = true
	}

	return map[string]any{"purchase_plan": purchasePlan}, warnings, nil
}

func resolveCheckoutCategoryID(item map[string]any, detail map[string]any, itemID string, fallback map[string]string) string {
	if id := strings.TrimSpace(asString(item["category_id"])); id != "" {
		return id
	}
	if category := asMap(item["category"]); category != nil {
		if id := strings.TrimSpace(asString(coalesceAny(category["id"], category["_id"]))); id != "" {
			return id
		}
	}
	if categoryIDs := asSlice(item["category_ids"]); len(categoryIDs) > 0 {
		if id := strings.TrimSpace(asString(categoryIDs[0])); id != "" {
			return id
		}
	}
	if detailCategory := resolveCheckoutCategoryIDFromItemLikePayload(detail); detailCategory != "" {
		return detailCategory
	}
	if id := resolveCheckoutCategoryIDFromDetails(detail, itemID); id != "" {
		return id
	}
	if id := strings.TrimSpace(fallback[itemID]); id != "" {
		return id
	}
	return ""
}

func resolveCheckoutCategoryIDFromDetails(detail map[string]any, itemID string) string {
	if strings.TrimSpace(itemID) == "" {
		return ""
	}
	categoryIDsByItemID := buildCheckoutCategoryIDIndex(detail)
	if id := strings.TrimSpace(categoryIDsByItemID[itemID]); id != "" {
		return id
	}
	return ""
}

func resolveCheckoutCategoryIDFromItemLikePayload(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if id := strings.TrimSpace(asString(payload["category_id"])); id != "" {
		return id
	}
	if category := asMap(payload["category"]); category != nil {
		if id := strings.TrimSpace(asString(coalesceAny(category["id"], category["_id"]))); id != "" {
			return id
		}
	}
	if categoryIDs := asSlice(payload["category_ids"]); len(categoryIDs) > 0 {
		if id := strings.TrimSpace(asString(categoryIDs[0])); id != "" {
			return id
		}
	}
	return ""
}

func resolveBasketVenueSlug(venue map[string]any) string {
	if venue == nil {
		return ""
	}
	candidates := []any{
		venue["slug"],
		venue["venue_slug"],
		venue["public_slug"],
		venue["url_slug"],
	}
	for _, candidate := range candidates {
		if slug := strings.TrimSpace(asString(candidate)); slug != "" {
			return slug
		}
	}
	return ""
}

func buildCheckoutCategoryIDIndex(payload map[string]any) map[string]string {
	index := map[string]string{}
	if payload == nil {
		return index
	}
	var walk func(any)
	walk = func(node any) {
		switch value := node.(type) {
		case map[string]any:
			if categories := asSlice(value["categories"]); len(categories) > 0 {
				for _, categoryNode := range categories {
					collectCheckoutCategoryMappings(categoryNode, index)
				}
			}
			if menuItems := asSlice(value["menu_items"]); len(menuItems) > 0 {
				for _, menuItemNode := range menuItems {
					menuItem := asMap(menuItemNode)
					if menuItem == nil {
						continue
					}
					itemID := strings.TrimSpace(asString(coalesceAny(menuItem["item_id"], menuItem["id"])))
					if itemID == "" {
						continue
					}
					if categoryID := resolveCheckoutCategoryIDFromItemLikePayload(menuItem); categoryID != "" {
						index[itemID] = categoryID
					}
				}
			}
			collectCheckoutCategoryMappings(value, index)
			for _, nested := range value {
				walk(nested)
			}
		case []any:
			for _, nested := range value {
				walk(nested)
			}
		}
	}
	walk(payload)
	return index
}

func collectCheckoutCategoryMappings(node any, index map[string]string) {
	category := asMap(node)
	if category == nil {
		return
	}
	categoryID := strings.TrimSpace(asString(coalesceAny(category["category_id"], category["id"], category["_id"])))
	if categoryID == "" {
		return
	}
	for _, itemNode := range asSlice(category["item_ids"]) {
		itemID := strings.TrimSpace(asString(itemNode))
		if itemID == "" {
			continue
		}
		index[itemID] = categoryID
	}
	for _, itemNode := range asSlice(category["items"]) {
		itemID := strings.TrimSpace(asString(itemNode))
		if item := asMap(itemNode); item != nil {
			itemID = strings.TrimSpace(asString(coalesceAny(item["item_id"], item["id"])))
		}
		if itemID == "" {
			continue
		}
		index[itemID] = categoryID
	}
}

func mergeCheckoutCategoryIndexes(target map[string]string, source map[string]string) {
	if target == nil || len(source) == 0 {
		return
	}
	for itemID, categoryID := range source {
		itemID = strings.TrimSpace(itemID)
		categoryID = strings.TrimSpace(categoryID)
		if itemID == "" || categoryID == "" {
			continue
		}
		if _, exists := target[itemID]; exists {
			continue
		}
		target[itemID] = categoryID
	}
}

func resolveCheckoutCategoryIDs(item map[string]any, categoryID string) []any {
	categoryIDs := asSlice(item["category_ids"])
	if len(categoryIDs) > 0 {
		return categoryIDs
	}
	if strings.TrimSpace(categoryID) == "" {
		return []any{}
	}
	return []any{categoryID}
}

func buildOptionValuePriceIndex(detail map[string]any) map[string]int {
	index := map[string]int{}
	for _, spec := range extractOptionSpecs(detail) {
		for valueID, value := range spec.Values {
			valueID = strings.TrimSpace(valueID)
			if valueID == "" {
				continue
			}
			index[valueID] = value.Price
		}
	}
	return index
}

func buildCheckoutOptions(raw any, valuePrices map[string]int) []any {
	options := make([]any, 0, len(asSlice(raw)))
	for _, optionValue := range asSlice(raw) {
		option := asMap(optionValue)
		if option == nil {
			continue
		}

		values := make([]any, 0, len(asSlice(option["values"])))
		for _, selectedValue := range asSlice(option["values"]) {
			value := asMap(selectedValue)
			if value == nil {
				continue
			}
			valueID := strings.TrimSpace(asString(value["id"]))
			if valueID == "" {
				continue
			}
			count := asInt(value["count"])
			if count <= 0 {
				count = 1
			}
			price := asInt(value["price"])
			if inferred, ok := valuePrices[valueID]; ok {
				price = inferred
			}
			values = append(values, map[string]any{
				"id":    valueID,
				"count": count,
				"price": price,
			})
		}

		options = append(options, map[string]any{
			"id":     strings.TrimSpace(asString(option["id"])),
			"values": values,
		})
	}
	return options
}

type optionGroupSpec struct {
	ID        string
	Name      string
	Required  bool
	MinSelect int
	MaxSelect int
	Values    map[string]optionValueSpec
}

type optionValueSpec struct {
	ID    string
	Name  string
	Price int
}

func extractOptionSpecs(payload map[string]any) map[string]optionGroupSpec {
	specs := map[string]optionGroupSpec{}
	visitOptionGroupCandidates(payload, func(group map[string]any) {
		groupID := strings.TrimSpace(asString(coalesceAny(group["id"], group["group_id"])))
		if groupID == "" {
			return
		}

		spec := specs[groupID]
		if spec.ID == "" {
			spec.ID = groupID
			spec.Name = asString(coalesceAny(group["name"], group["title"]))
			spec.Required = asBool(group["required"])
			spec.MinSelect = asInt(coalesceAny(group["min"], group["minimum"], group["min_select"]))
			spec.MaxSelect = asInt(coalesceAny(group["max"], group["maximum"], group["max_select"]))
			spec.Values = map[string]optionValueSpec{}
		}

		for _, value := range asSlice(coalesceAny(group["values"], group["options"], group["items"])) {
			valueMap := asMap(value)
			if valueMap == nil {
				continue
			}
			valueID := strings.TrimSpace(asString(coalesceAny(valueMap["id"], valueMap["value_id"])))
			if valueID == "" {
				continue
			}
			price := asInt(valueMap["price"])
			if price == 0 {
				price = asInt(asMap(valueMap["price"])["amount"])
			}
			spec.Values[valueID] = optionValueSpec{
				ID:    valueID,
				Name:  asString(coalesceAny(valueMap["name"], valueMap["title"])),
				Price: price,
			}
		}
		specs[groupID] = spec
	})
	return specs
}

func visitOptionGroupCandidates(payload map[string]any, visit func(map[string]any)) {
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			if groups := asSlice(coalesceAny(typed["option_groups"], typed["options"])); len(groups) > 0 {
				for _, groupValue := range groups {
					group := asMap(groupValue)
					if group == nil {
						continue
					}
					if strings.TrimSpace(asString(coalesceAny(group["id"], group["group_id"]))) != "" {
						visit(group)
					}
				}
			}
			for _, nested := range typed {
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(payload)
}

func inferCurrency(formatted string) string {
	switch {
	case strings.Contains(formatted, "€"):
		return "EUR"
	case strings.Contains(formatted, "$"):
		return "USD"
	case strings.Contains(formatted, "£"):
		return "GBP"
	default:
		return ""
	}
}

func asMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	switch m := value.(type) {
	case map[string]any:
		return m
	case map[string]string:
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return nil
}

func asSlice(value any) []any {
	if value == nil {
		return nil
	}
	if values, ok := value.([]any); ok {
		return values
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil
	}
	kind := rv.Kind()
	if kind != reflect.Slice && kind != reflect.Array {
		return nil
	}
	if rv.Type().Elem().Kind() == reflect.Uint8 {
		return nil
	}
	values := make([]any, rv.Len())
	for idx := 0; idx < rv.Len(); idx++ {
		values[idx] = rv.Index(idx).Interface()
	}
	return values
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func asBool(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	return false
}

func asInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func coalesceAny(values ...any) any {
	for _, value := range values {
		if value == nil {
			continue
		}
		if s, ok := value.(string); ok && s == "" {
			continue
		}
		return value
	}
	return nil
}

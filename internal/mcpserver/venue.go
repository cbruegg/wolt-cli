package mcpserver

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var objectIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func looksLikeObjectID(value string) bool {
	return objectIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

// venueRef captures the parts of a venue identity that downstream API calls
// might need. After resolution, ID is always populated when possible.
type venueRef struct {
	Input string
	ID    string
	Slug  string
}

// normalizeVenueInput strips a full Wolt URL down to its last path segment, so
// callers can pass either slug, raw ID, or a copy-pasted browser URL.
func normalizeVenueInput(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return value
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		part := strings.TrimSpace(parts[i])
		if part == "" {
			continue
		}
		return part
	}
	return value
}

// resolveVenueRef turns whatever the LLM passed (slug / object ID / URL) into a
// {ID, Slug} pair. For raw IDs it calls RestaurantByID to backfill the slug;
// for slugs it calls VenuePageStatic to backfill the ID.
func (tc *ToolCtx) resolveVenueRef(ctx context.Context, raw string) (venueRef, error) {
	input := normalizeVenueInput(raw)
	if input == "" {
		return venueRef{}, fmt.Errorf("venue identifier is required (slug, id, or wolt.com URL)")
	}
	ref := venueRef{Input: raw}
	if looksLikeObjectID(input) {
		ref.ID = input
		if tc.wolt != nil {
			if restaurant, err := tc.wolt.RestaurantByID(ctx, input); err == nil && restaurant != nil {
				ref.Slug = strings.TrimSpace(restaurant.Slug)
			}
		}
		return ref, nil
	}
	ref.Slug = input
	ref.ID = input
	if tc.wolt == nil {
		return ref, nil
	}
	payload, err := tc.wolt.VenuePageStatic(ctx, input)
	if err == nil {
		if id := venueIDFromPayload(payload); id != "" {
			ref.ID = id
		}
	}
	return ref, nil
}

func venueIDFromPayload(payload map[string]any) string {
	venue := asMap(payload["venue"])
	if venue == nil {
		venue = asMap(payload["venue_raw"])
	}
	return strings.TrimSpace(asString(coalesceAny(
		venue["id"],
		payload["venue_id"],
		payload["id"],
	)))
}

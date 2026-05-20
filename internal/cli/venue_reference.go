package cli

import (
	"context"
	"net/url"
	"strings"
)

type venueReference struct {
	Input     string
	VenueID   string
	VenueSlug string
}

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

func resolveVenueReference(ctx context.Context, deps Dependencies, raw string) (venueReference, error) {
	input := normalizeVenueInput(raw)
	ref := venueReference{Input: raw}
	if input == "" {
		return ref, nil
	}
	if looksLikeObjectID(input) {
		ref.VenueID = input
		if deps.Wolt != nil {
			if restaurant, err := deps.Wolt.RestaurantByID(ctx, input); err == nil && restaurant != nil {
				ref.VenueSlug = strings.TrimSpace(restaurant.Slug)
			}
		}
		return ref, nil
	}
	ref.VenueSlug = input
	ref.VenueID = input
	if deps.Wolt != nil {
		if payload, err := deps.Wolt.VenuePageStatic(ctx, input); err == nil {
			if id := venueIDFromPayload(payload); id != "" {
				ref.VenueID = id
			}
		}
	}
	return ref, nil
}

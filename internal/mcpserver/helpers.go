package mcpserver

import (
	"fmt"
	"strings"

	"github.com/mekedron/wolt-cli/internal/domain"
)

// LocationOut is the canonical lat/lon pair embedded in every location-bound
// tool output, so the agent doesn't have to dig through the raw data field.
type LocationOut struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

func locationOut(loc domain.Location) LocationOut {
	return LocationOut{Lat: loc.Lat, Lon: loc.Lon}
}

func humanCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// filterFeedSectionsByQuery walks BuildDiscoveryFeed output and keeps only
// venues whose name/tagline/top_offer match the (lower-cased) needle.
func filterFeedSectionsByQuery(data map[string]any, needle string) {
	needle = strings.ToLower(strings.TrimSpace(needle))
	if needle == "" {
		return
	}
	sections := asSlice(data["sections"])
	kept := make([]any, 0, len(sections))
	for _, rawSection := range sections {
		section := asMap(rawSection)
		if section == nil {
			continue
		}
		venues := asSlice(section["venues"])
		filtered := make([]any, 0, len(venues))
		for _, rawVenue := range venues {
			venue := asMap(rawVenue)
			if venue == nil {
				continue
			}
			haystack := strings.ToLower(asString(venue["name"])) +
				" " + strings.ToLower(asString(venue["tagline"])) +
				" " + strings.ToLower(asString(venue["top_offer"]))
			if strings.Contains(haystack, needle) {
				filtered = append(filtered, rawVenue)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		section["venues"] = filtered
		kept = append(kept, section)
	}
	data["sections"] = kept
}

func capFeedItemsPerSection(data map[string]any, max int) {
	if max <= 0 {
		return
	}
	for _, rawSection := range asSlice(data["sections"]) {
		section := asMap(rawSection)
		if section == nil {
			continue
		}
		venues := asSlice(section["venues"])
		if len(venues) > max {
			section["venues"] = venues[:max]
		}
	}
}

// applyVenueRowFilters mutates BuildVenueSearchResult output to drop venues
// below the rating floor or above the delivery-fee ceiling.
func applyVenueRowFilters(data map[string]any, minRating float64, maxDeliveryFee int) {
	rows := asSlice(data["items"])
	if len(rows) == 0 {
		return
	}
	filtered := make([]any, 0, len(rows))
	for _, raw := range rows {
		row := asMap(raw)
		if row == nil {
			continue
		}
		if minRating > 0 {
			rating, ok := asFloat(row["rating"])
			if !ok || rating < minRating {
				continue
			}
		}
		if maxDeliveryFee > 0 {
			fee := asInt(asMap(row["delivery_fee"])["amount"])
			if fee > maxDeliveryFee {
				continue
			}
		}
		filtered = append(filtered, raw)
	}
	data["items"] = filtered
}

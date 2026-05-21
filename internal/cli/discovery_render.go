package cli

import (
	"os"
	"strings"
)

// badgesPlainMode is true when the user has asked for bracketed text
// prefixes instead of Unicode glyphs (e.g. terminals that render
// boxes for `⚡` / `◷`).
func badgesPlainMode() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WOLT_BADGES_PLAIN"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// badgeIconGlyph maps an upstream icon name (badges_v2[].icon) to a
// single-rune visual marker. Unknown icons return "·" so the prefix
// stays compact rather than spilling the full icon name into the cell.
func badgeIconGlyph(icon string) string {
	switch strings.ToLower(strings.TrimSpace(icon)) {
	case "":
		return ""
	case "wolt-plus", "wolt_plus", "woltplus":
		return "+"
	case "coupon-fill", "coupon", "discount", "promotion", "promo":
		return "%"
	case "bike", "fast", "fast-delivery", "quick", "lightning":
		return "⚡"
	case "clock", "schedule", "time":
		return "◷"
	case "new", "star", "best":
		return "★"
	default:
		return "·"
	}
}

// formatBadgePrefix renders the badges_v2 array as a short prefix
// suitable to slot in front of the venue name in a table cell. Empty
// slice → empty string. With WOLT_BADGES_PLAIN=1 it falls back to
// bracketed text ("[Wolt+] [20% off]") for terminals that don't
// render the glyphs cleanly.
func formatBadgePrefix(badges []any) string {
	if len(badges) == 0 {
		return ""
	}
	if badgesPlainMode() {
		parts := make([]string, 0, len(badges))
		seen := map[string]struct{}{}
		for _, raw := range badges {
			m := asMap(raw)
			if m == nil {
				continue
			}
			text := strings.TrimSpace(asString(m["text"]))
			if text == "" {
				continue
			}
			if _, exists := seen[text]; exists {
				continue
			}
			seen[text] = struct{}{}
			parts = append(parts, "["+text+"]")
		}
		if len(parts) == 0 {
			return ""
		}
		return strings.Join(parts, " ") + " "
	}

	glyphs := make([]string, 0, len(badges))
	seen := map[string]struct{}{}
	for _, raw := range badges {
		m := asMap(raw)
		if m == nil {
			continue
		}
		icon := strings.TrimSpace(asString(m["icon"]))
		glyph := badgeIconGlyph(icon)
		if glyph == "" {
			text := strings.TrimSpace(asString(m["text"]))
			if text == "" {
				continue
			}
			glyph = badgeIconGlyph(text)
			if glyph == "" {
				glyph = "·"
			}
		}
		if _, exists := seen[glyph]; exists {
			continue
		}
		seen[glyph] = struct{}{}
		glyphs = append(glyphs, glyph)
	}
	if len(glyphs) == 0 {
		return ""
	}
	return strings.Join(glyphs, "") + " "
}

// anyVenueRowHasHighlights reports whether at least one row in the
// given slice carries a non-empty menu_highlights array. Used to
// auto-hide the Highlights column when no row has data to show.
func anyVenueRowHasHighlights(rows []any) bool {
	for _, raw := range rows {
		item := asMap(raw)
		if item == nil {
			continue
		}
		if len(asSlice(item["menu_highlights"])) > 0 {
			return true
		}
	}
	return false
}

// anyFeedSectionHasHighlights walks the feed sections and reports
// whether any venue-section row has highlights data.
func anyFeedSectionHasHighlights(sections []any) bool {
	for _, raw := range sections {
		section := asMap(raw)
		if section == nil {
			continue
		}
		if anyVenueRowHasHighlights(asSlice(section["items"])) {
			return true
		}
	}
	return false
}

// formatHighlightsCell joins menu_highlights[] entries as
// "name (price); name (price)" and truncates the whole string to max
// runes. Truncation happens after join — every entry is intact in JSON
// output; the table cell is the only place truncation kicks in.
func formatHighlightsCell(highlights []any, max int) string {
	if len(highlights) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(highlights))
	for _, raw := range highlights {
		entry := asMap(raw)
		if entry == nil {
			continue
		}
		name := strings.TrimSpace(asString(entry["name"]))
		price := strings.TrimSpace(asString(entry["formatted_price"]))
		switch {
		case name != "" && price != "":
			parts = append(parts, name+" "+price)
		case name != "":
			parts = append(parts, name)
		case price != "":
			parts = append(parts, price)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return truncateForTable(strings.Join(parts, "; "), max)
}

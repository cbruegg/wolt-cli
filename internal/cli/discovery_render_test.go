package cli

import (
	"strings"
	"testing"
)

func TestFormatBadgePrefixGlyphs(t *testing.T) {
	t.Setenv("WOLT_BADGES_PLAIN", "")
	badges := []any{
		map[string]any{"icon": "wolt-plus", "text": "Wolt+"},
		map[string]any{"icon": "coupon-fill", "text": "20% off"},
		map[string]any{"icon": "bike", "text": "Fast"},
	}
	got := formatBadgePrefix(badges)
	for _, want := range []string{"+", "%", "⚡"} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected glyph %q in %q", want, got)
		}
	}
	if !strings.HasSuffix(got, " ") {
		t.Fatalf("expected prefix to end with space, got %q", got)
	}
}

func TestFormatBadgePrefixUnknownIconFallback(t *testing.T) {
	t.Setenv("WOLT_BADGES_PLAIN", "")
	badges := []any{
		map[string]any{"icon": "totally-new-icon-name", "text": "Heads up"},
	}
	got := formatBadgePrefix(badges)
	if !strings.Contains(got, "·") {
		t.Fatalf("expected unknown icon to render as '·', got %q", got)
	}
}

func TestFormatBadgePrefixPlainMode(t *testing.T) {
	t.Setenv("WOLT_BADGES_PLAIN", "1")
	badges := []any{
		map[string]any{"icon": "wolt-plus", "text": "Wolt+"},
		map[string]any{"icon": "coupon-fill", "text": "20% off"},
	}
	got := formatBadgePrefix(badges)
	if !strings.Contains(got, "[Wolt+]") || !strings.Contains(got, "[20% off]") {
		t.Fatalf("expected bracketed text labels, got %q", got)
	}
	for _, glyph := range []string{"⚡", "◷", "★"} {
		if strings.Contains(got, glyph) {
			t.Fatalf("expected plain mode to skip glyph %q, got %q", glyph, got)
		}
	}
}

func TestFormatBadgePrefixEmpty(t *testing.T) {
	if got := formatBadgePrefix(nil); got != "" {
		t.Fatalf("expected empty prefix for nil, got %q", got)
	}
	if got := formatBadgePrefix([]any{}); got != "" {
		t.Fatalf("expected empty prefix for empty slice, got %q", got)
	}
}

func TestFormatHighlightsCell(t *testing.T) {
	highlights := []any{
		map[string]any{"name": "Bacon Burger", "formatted_price": "9.90 €"},
		map[string]any{"name": "Fries", "formatted_price": "3.50 €"},
	}
	got := formatHighlightsCell(highlights, 64)
	if !strings.Contains(got, "Bacon Burger 9.90 €") {
		t.Fatalf("expected joined entry in %q", got)
	}
	if !strings.Contains(got, "; ") {
		t.Fatalf("expected '; ' separator in %q", got)
	}
}

func TestFormatHighlightsCellTruncates(t *testing.T) {
	highlights := []any{
		map[string]any{"name": "An extraordinarily long dish name", "formatted_price": "19.90 €"},
		map[string]any{"name": "Another lengthy entry that should be cut", "formatted_price": "29.90 €"},
	}
	got := formatHighlightsCell(highlights, 24)
	if len([]rune(got)) > 24 {
		t.Fatalf("expected cell <= 24 runes, got %d (%q)", len([]rune(got)), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("expected ellipsis suffix on truncation, got %q", got)
	}
}

func TestFormatHighlightsCellEmpty(t *testing.T) {
	if got := formatHighlightsCell(nil, 32); got != "-" {
		t.Fatalf("expected '-' for nil, got %q", got)
	}
	if got := formatHighlightsCell([]any{map[string]any{}}, 32); got != "-" {
		t.Fatalf("expected '-' when no usable fields, got %q", got)
	}
}

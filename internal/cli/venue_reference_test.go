package cli

import (
	"context"
	"testing"

	"github.com/mekedron/wolt-cli/internal/domain"
)

func TestNormalizeVenueInput(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"whitespace", "   ", ""},
		{"plain slug", "hesburger-helsinki-kamppi", "hesburger-helsinki-kamppi"},
		{"slug with whitespace", "  hesburger-kamppi  ", "hesburger-kamppi"},
		{"object id", "6123456789abcdef01234567", "6123456789abcdef01234567"},
		{"restaurant url", "https://wolt.com/fi/fin/helsinki/restaurant/hesburger-helsinki-kamppi", "hesburger-helsinki-kamppi"},
		{"restaurant url trailing slash", "https://wolt.com/en/fin/helsinki/restaurant/hesburger-helsinki-kamppi/", "hesburger-helsinki-kamppi"},
		{"venue url", "https://wolt.com/en/fin/helsinki/venue/wolt-market-niittari", "wolt-market-niittari"},
		{"discovery url", "https://wolt.com/en/discovery/helsinki/", "helsinki"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeVenueInput(tc.raw)
			if got != tc.want {
				t.Fatalf("normalizeVenueInput(%q) = %q; want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestResolveVenueReferenceSlugQueriesStaticPage(t *testing.T) {
	calls := 0
	wolt := &testWoltAPI{
		venuePageStaticFn: func(_ context.Context, slug string) (map[string]any, error) {
			calls++
			if slug != "hesburger-kamppi" {
				t.Fatalf("expected slug to be passed through, got %q", slug)
			}
			return map[string]any{"venue": map[string]any{"id": "6123456789abcdef01234567"}}, nil
		},
	}
	deps := Dependencies{Wolt: wolt}

	ref, err := resolveVenueReference(context.Background(), deps, "hesburger-kamppi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.VenueSlug != "hesburger-kamppi" {
		t.Fatalf("expected slug to be preserved, got %q", ref.VenueSlug)
	}
	if ref.VenueID != "6123456789abcdef01234567" {
		t.Fatalf("expected resolved venue id, got %q", ref.VenueID)
	}
	if calls != 1 {
		t.Fatalf("expected exactly one static-page call, got %d", calls)
	}
}

func TestResolveVenueReferenceURLExtractsSlugThenResolves(t *testing.T) {
	wolt := &testWoltAPI{
		venuePageStaticFn: func(_ context.Context, slug string) (map[string]any, error) {
			if slug != "wolt-market-niittari" {
				t.Fatalf("expected slug from URL path, got %q", slug)
			}
			return map[string]any{"venue": map[string]any{"id": "6111111111aaaaaaaa222222"}}, nil
		},
	}
	deps := Dependencies{Wolt: wolt}

	ref, err := resolveVenueReference(context.Background(), deps, "https://wolt.com/en/fin/espoo/venue/wolt-market-niittari")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.VenueSlug != "wolt-market-niittari" {
		t.Fatalf("expected slug from URL, got %q", ref.VenueSlug)
	}
	if ref.VenueID != "6111111111aaaaaaaa222222" {
		t.Fatalf("expected resolved id from static payload, got %q", ref.VenueID)
	}
}

func TestResolveVenueReferenceObjectIDReverseLookup(t *testing.T) {
	wolt := &testWoltAPI{
		restaurantByIDFn: func(_ context.Context, id string) (*domain.Restaurant, error) {
			if id != "6123456789abcdef01234567" {
				t.Fatalf("expected object id to be passed through, got %q", id)
			}
			return &domain.Restaurant{ID: id, Slug: "hesburger-kamppi"}, nil
		},
		venuePageStaticFn: func(_ context.Context, slug string) (map[string]any, error) {
			t.Fatalf("static page should not be called when input is an object id, got slug %q", slug)
			return nil, nil
		},
	}
	deps := Dependencies{Wolt: wolt}

	ref, err := resolveVenueReference(context.Background(), deps, "6123456789abcdef01234567")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.VenueID != "6123456789abcdef01234567" {
		t.Fatalf("expected id preserved, got %q", ref.VenueID)
	}
	if ref.VenueSlug != "hesburger-kamppi" {
		t.Fatalf("expected slug from restaurant lookup, got %q", ref.VenueSlug)
	}
}

func TestResolveVenueReferenceEmptyInput(t *testing.T) {
	deps := Dependencies{}

	ref, err := resolveVenueReference(context.Background(), deps, "   ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.VenueID != "" || ref.VenueSlug != "" {
		t.Fatalf("expected empty resolution for blank input, got %+v", ref)
	}
}

func TestResolveVenueReferenceFallsBackToInputWhenStaticFails(t *testing.T) {
	wolt := &testWoltAPI{
		venuePageStaticFn: func(_ context.Context, _ string) (map[string]any, error) {
			return nil, context.DeadlineExceeded
		},
	}
	deps := Dependencies{Wolt: wolt}

	ref, err := resolveVenueReference(context.Background(), deps, "hesburger-kamppi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ref.VenueSlug != "hesburger-kamppi" {
		t.Fatalf("expected slug echoed back when API is down, got %q", ref.VenueSlug)
	}
	if ref.VenueID != "hesburger-kamppi" {
		t.Fatalf("expected venue id to fall back to slug when API is down, got %q", ref.VenueID)
	}
}

package e2e_test

import (
	"strings"
	"testing"

	"github.com/mekedron/wolt-cli/internal/cli"
	"github.com/mekedron/wolt-cli/internal/domain"
)

func TestRemovedCommandsAreRejected(t *testing.T) {
	cases := [][]string{
		{"auth"},
		{"auth", "status"},
		{"profile"},
		{"profile", "show"},
		{"profile", "orders"},
		{"profile", "payments"},
		{"profile", "addresses"},
		{"profile", "favorites"},
		{"configure"},
		{"discover"},
		{"discover", "feed"},
		{"discover", "categories"},
		{"search"},
		{"search", "venues"},
		{"search", "items"},
		{"item"},
		{"item", "show"},
		{"item", "options"},
		{"me"},
	}

	for _, args := range cases {
		args := args
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			deps := cli.Dependencies{
				Wolt:     &mockWolt{},
				Profiles: &mockProfiles{profile: domain.Profile{Name: "default", IsDefault: true}},
				Location: &mockLocation{},
				Config:   &mockConfig{},
				Version:  "test",
			}
			exitCode, out := runCLIWithDeps(t, deps, args...)
			if exitCode == 0 {
				t.Fatalf("expected non-zero exit for removed command %q, got 0 with output:\n%s", args, out)
			}
			lower := strings.ToLower(out)
			if !strings.Contains(lower, "no such command") && !strings.Contains(lower, "unknown command") && !strings.Contains(lower, "unknown subcommand") {
				t.Fatalf("expected unknown-command message for %q, got:\n%s", args, out)
			}
		})
	}
}

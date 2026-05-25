package main

import (
	"os"
	"testing"
	"time"
)

// setArgs swaps os.Args for the duration of a single test and restores it on
// cleanup. resolveLocale reads os.Args directly, so tests need to substitute
// in a controlled slice.
func setArgs(t *testing.T, args []string) {
	t.Helper()
	original := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = original })
}

func TestResolveLocale_DefaultsToEnFI(t *testing.T) {
	setArgs(t, []string{"wolt-mcp"})
	t.Setenv(localeEnv, "")
	if got := resolveLocale(); got != defaultLocale {
		t.Errorf("resolveLocale() = %q, want %q", got, defaultLocale)
	}
}

func TestResolveLocale_EnvFallback(t *testing.T) {
	setArgs(t, []string{"wolt-mcp"})
	t.Setenv(localeEnv, "fi-FI")
	if got := resolveLocale(); got != "fi-FI" {
		t.Errorf("resolveLocale() = %q, want %q", got, "fi-FI")
	}
}

func TestResolveLocale_EnvTrimsWhitespace(t *testing.T) {
	setArgs(t, []string{"wolt-mcp"})
	t.Setenv(localeEnv, "  sv-SE  ")
	if got := resolveLocale(); got != "sv-SE" {
		t.Errorf("resolveLocale() = %q, want %q", got, "sv-SE")
	}
}

func TestResolveLocale_FlagOverridesEnv(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale", "de-DE"})
	t.Setenv(localeEnv, "fi-FI")
	if got := resolveLocale(); got != "de-DE" {
		t.Errorf("resolveLocale() = %q, want %q", got, "de-DE")
	}
}

func TestResolveLocale_FlagAcceptsTrailingPosition(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--something", "value", "--locale", "fr-FR"})
	t.Setenv(localeEnv, "")
	if got := resolveLocale(); got != "fr-FR" {
		t.Errorf("resolveLocale() = %q, want %q", got, "fr-FR")
	}
}

func TestResolveLocale_FlagWithoutValueFallsThrough(t *testing.T) {
	// `--locale` without a following value must not panic and must fall
	// through to the env / default.
	setArgs(t, []string{"wolt-mcp", "--locale"})
	t.Setenv(localeEnv, "et-EE")
	if got := resolveLocale(); got != "et-EE" {
		t.Errorf("resolveLocale() = %q, want %q (env fallback)", got, "et-EE")
	}
}

func TestResolveLocale_FlagTrimsValueWhitespace(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale", "  pl-PL  "})
	t.Setenv(localeEnv, "")
	if got := resolveLocale(); got != "pl-PL" {
		t.Errorf("resolveLocale() = %q, want %q", got, "pl-PL")
	}
}

func TestResolveLocale_EmptyEnvAndEmptyFlagFallBackToDefault(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale", "   "})
	t.Setenv(localeEnv, "")
	// Whitespace-only flag value trims to "" but the function still returns
	// that — the consumer is responsible for treating "" as fallback. This
	// test pins down current behavior so regressions are noticed.
	if got := resolveLocale(); got != "" {
		t.Errorf("resolveLocale() = %q, want empty string for whitespace-only --locale", got)
	}
}

func TestResolveLocale_EqualsFormIsSupported(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale=fi-FI"})
	t.Setenv(localeEnv, "")
	if got := resolveLocale(); got != "fi-FI" {
		t.Errorf("resolveLocale() = %q, want %q", got, "fi-FI")
	}
}

func TestResolveLocale_EqualsFormOverridesEnv(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale=de-DE"})
	t.Setenv(localeEnv, "sv-SE")
	if got := resolveLocale(); got != "de-DE" {
		t.Errorf("resolveLocale() = %q, want %q", got, "de-DE")
	}
}

func TestResolveLocale_EqualsFormTrimsWhitespace(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale=  fr-FR  "})
	t.Setenv(localeEnv, "")
	if got := resolveLocale(); got != "fr-FR" {
		t.Errorf("resolveLocale() = %q, want %q", got, "fr-FR")
	}
}

func TestResolveLocale_EqualsFormEmptyValueIsRespected(t *testing.T) {
	// `--locale=` (empty after =) explicitly opts into the empty string and
	// short-circuits past the env fallback — matches the space-separated
	// `--locale ""` semantics. Lock this in so it doesn't drift.
	setArgs(t, []string{"wolt-mcp", "--locale="})
	t.Setenv(localeEnv, "ignored")
	if got := resolveLocale(); got != "" {
		t.Errorf("resolveLocale() = %q, want empty string for explicit --locale=", got)
	}
}

func TestResolveLocale_IgnoresProgramNameAtArgsZero(t *testing.T) {
	// If the binary is invoked through a path whose basename happens to be
	// `--locale`, the scan must not treat os.Args[0] as a flag.
	setArgs(t, []string{"--locale", "wolt-mcp"})
	t.Setenv(localeEnv, "fi-FI")
	if got := resolveLocale(); got != "fi-FI" {
		t.Errorf("resolveLocale() = %q, want %q (env fallback, os.Args[0] must be ignored)", got, "fi-FI")
	}
}

func TestResolveLocale_PrefersFirstFlagWhenBothFormsPresent(t *testing.T) {
	setArgs(t, []string{"wolt-mcp", "--locale=first", "--locale", "second"})
	t.Setenv(localeEnv, "")
	if got := resolveLocale(); got != "first" {
		t.Errorf("resolveLocale() = %q, want %q (first match wins)", got, "first")
	}
}

func TestResolveWoltRequestMinInterval_Default(t *testing.T) {
	t.Setenv(woltHTTPMinIntervalEnv, "")
	if got := resolveWoltRequestMinInterval(); got != defaultWoltHTTPMinInterval {
		t.Errorf("resolveWoltRequestMinInterval() = %v, want %v", got, defaultWoltHTTPMinInterval)
	}
}

func TestResolveWoltRequestMinInterval_ValidValue(t *testing.T) {
	t.Setenv(woltHTTPMinIntervalEnv, "500")
	if got, want := resolveWoltRequestMinInterval(), 500*time.Millisecond; got != want {
		t.Errorf("resolveWoltRequestMinInterval() = %v, want %v", got, want)
	}
}

func TestResolveWoltRequestMinInterval_Zero(t *testing.T) {
	t.Setenv(woltHTTPMinIntervalEnv, "0")
	if got := resolveWoltRequestMinInterval(); got != 0 {
		t.Errorf("resolveWoltRequestMinInterval() = %v, want 0", got)
	}
}

func TestResolveWoltRequestMinInterval_NegativeFallsBackToDefault(t *testing.T) {
	t.Setenv(woltHTTPMinIntervalEnv, "-10")
	if got := resolveWoltRequestMinInterval(); got != defaultWoltHTTPMinInterval {
		t.Errorf("resolveWoltRequestMinInterval() = %v, want %v (default for negative)", got, defaultWoltHTTPMinInterval)
	}
}

func TestResolveWoltRequestMinInterval_NonNumericFallsBackToDefault(t *testing.T) {
	t.Setenv(woltHTTPMinIntervalEnv, "fast")
	if got := resolveWoltRequestMinInterval(); got != defaultWoltHTTPMinInterval {
		t.Errorf("resolveWoltRequestMinInterval() = %v, want %v (default for non-numeric)", got, defaultWoltHTTPMinInterval)
	}
}

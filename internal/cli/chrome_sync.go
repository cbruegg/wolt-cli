package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

const (
	// chromeProbeTimeout bounds how long we wait to determine whether Chrome
	// is reachable on the debug port. Kept short so opportunistic syncs don't
	// add noticeable latency when Chrome is closed.
	chromeProbeTimeout = 500 * time.Millisecond
	// envDisableChromeSync, when non-empty, suppresses the opportunistic
	// re-sync from a running Chrome. Tests set it to keep results
	// independent of the developer's local browser state; users with
	// hardened setups can set it too.
	envDisableChromeSync = "WOLT_DISABLE_CHROME_SYNC"
)

// pullAuthFromRunningChrome reads Wolt auth cookies from a Chrome that is
// already running with --remote-debugging-port. Unlike loginViaManagedChrome it
// never launches Chrome and never waits for user interaction — if Chrome isn't
// reachable within a short probe window, it returns found=false with no error.
//
// This mirrors what the browser itself does: every wolt.com tab refreshes the
// in-memory access token off the bootstrap __wrtoken cookie. If a user runs
// the CLI while their Chrome is open, the CLI can sip the freshest cookies
// straight from that session — same chain, no divergence.
func pullAuthFromRunningChrome(ctx context.Context, browserURL string) (woltgateway.AuthContext, bool, error) {
	if strings.TrimSpace(os.Getenv(envDisableChromeSync)) != "" {
		return woltgateway.AuthContext{}, false, nil
	}
	browserURL = strings.TrimRight(strings.TrimSpace(browserURL), "/")
	if browserURL == "" {
		browserURL = fmt.Sprintf("http://127.0.0.1:%d", defaultChromeDebugPort)
	}

	probeCtx, cancel := context.WithTimeout(ctx, chromeProbeTimeout)
	defer cancel()
	if !chromeDevToolsReady(probeCtx, browserURL) {
		return woltgateway.AuthContext{}, false, nil
	}

	auth, err := readAuthFromChrome(ctx, browserURL)
	if err != nil {
		// Chrome is up but has no wolt.com session, or CDP refused — treat as
		// "no fresh auth available", not as a hard failure.
		return woltgateway.AuthContext{}, false, nil
	}
	if !auth.HasCredentials() {
		return woltgateway.AuthContext{}, false, nil
	}
	return auth, true, nil
}

// chromeAuthIsFresherThan reports whether Chrome's snapshot has a strictly
// newer __wtoken JWT expiry than the auth we're about to use. This is the
// signal to adopt Chrome's auth before making a request — it means the
// browser has refreshed more recently than we have.
func chromeAuthIsFresherThan(chromeAuth woltgateway.AuthContext, currentToken string) bool {
	chromeExp, chromeOK := tokenExpiry(chromeAuth.WToken)
	if !chromeOK {
		return false
	}
	currentExp, currentOK := tokenExpiry(currentToken)
	if !currentOK {
		// We don't have a usable expiry — Chrome's is at least parseable, so
		// prefer it.
		return true
	}
	return chromeExp.After(currentExp)
}

// adoptChromeAuth replaces the in-memory auth context with Chrome's snapshot
// and persists the cookies+bootstrap refresh token so the next CLI run starts
// off the freshest chain. This is the "re-bootstrap" path — we deliberately
// write the refresh token, unlike a normal mid-process rotation.
func adoptChromeAuth(
	ctx context.Context,
	deps Dependencies,
	auth *woltgateway.AuthContext,
	chromeAuth woltgateway.AuthContext,
) error {
	if auth == nil {
		return fmt.Errorf("auth context is nil")
	}
	auth.WToken = chromeAuth.WToken
	if strings.TrimSpace(chromeAuth.RefreshToken) != "" {
		auth.RefreshToken = chromeAuth.RefreshToken
	}
	auth.Cookies = append([]string(nil), chromeAuth.Cookies...)
	if deps.Config == nil {
		return nil
	}
	cfg, err := deps.Config.Load(ctx)
	if err != nil {
		cfg = domain.Config{Profiles: []domain.Profile{{Name: "default", IsDefault: true}}}
	}
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = []domain.Profile{{Name: "default", IsDefault: true}}
	}
	cfg.Profiles[0].Name = "default"
	cfg.Profiles[0].IsDefault = true
	cfg.Profiles[0].WToken = normalizeWToken(auth.WToken)
	if strings.TrimSpace(auth.RefreshToken) != "" {
		cfg.Profiles[0].WRefreshToken = normalizeRefreshToken(auth.RefreshToken)
	}
	cfg.Profiles[0].Cookies = append([]string(nil), auth.Cookies...)
	return deps.Config.Save(ctx, cfg)
}

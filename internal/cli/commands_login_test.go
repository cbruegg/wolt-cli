package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

// fakeChromeDevTools simulates the subset of the Chrome DevTools Protocol used by
// the browser-login flow: the /json/version probe, the /json/list tab listing, and
// a WebSocket endpoint that answers Network.getAllCookies for one tab.
type fakeChromeDevTools struct {
	server    *httptest.Server
	cookies   []map[string]any
	pageURL   string
	versionOK bool
}

func newFakeChromeDevTools(t *testing.T, cookies []map[string]any, pageURL string) *fakeChromeDevTools {
	t.Helper()
	fake := &fakeChromeDevTools{
		cookies:   cookies,
		pageURL:   pageURL,
		versionOK: true,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, _ *http.Request) {
		if !fake.versionOK {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"Browser":"FakeChrome/0.0"}`))
	})
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		body, _ := json.Marshal([]map[string]any{
			{
				"type":                 "page",
				"url":                  fake.pageURL,
				"webSocketDebuggerUrl": "ws://" + host + "/ws/page-1",
			},
		})
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/json/new", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"page-1"}`))
	})
	mux.HandleFunc("/ws/page-1", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		for {
			var msg map[string]any
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			id, _ := msg["id"].(float64)
			method, _ := msg["method"].(string)
			response := map[string]any{"id": id}
			if method == "Network.getAllCookies" {
				response["result"] = map[string]any{"cookies": fake.cookies}
			} else {
				response["result"] = map[string]any{}
			}
			if err := conn.WriteJSON(response); err != nil {
				return
			}
		}
	})
	fake.server = httptest.NewServer(mux)
	return fake
}

func (f *fakeChromeDevTools) BrowserURL() string { return f.server.URL }

func (f *fakeChromeDevTools) Close() { f.server.Close() }

func TestChromeDevToolsReady(t *testing.T) {
	fake := newFakeChromeDevTools(t, nil, "https://wolt.com/")
	defer fake.Close()

	if !chromeDevToolsReady(context.Background(), fake.BrowserURL()) {
		t.Fatal("expected ready check to succeed against fake DevTools")
	}

	fake.versionOK = false
	if chromeDevToolsReady(context.Background(), fake.BrowserURL()) {
		t.Fatal("expected ready check to fail when /json/version returns 5xx")
	}
}

func TestReadAuthFromChromeExtractsTokensFromWoltCookies(t *testing.T) {
	const wToken = "header.payload.sig"
	const wRefresh = "refresh-value"
	cookies := []map[string]any{
		{"name": "__wtoken", "value": wToken, "domain": "wolt.com"},
		{"name": "__wrtoken", "value": wRefresh, "domain": ".wolt.com"},
		{"name": "session_id", "value": "should-be-included", "domain": "consumer-api.wolt.com"},
		{"name": "tracking", "value": "drop-me", "domain": "analytics.example.com"},
		{"name": "", "value": "no-name", "domain": "wolt.com"},
	}
	fake := newFakeChromeDevTools(t, cookies, "https://wolt.com/account")
	defer fake.Close()

	auth, err := readAuthFromChrome(context.Background(), fake.BrowserURL())
	if err != nil {
		t.Fatalf("readAuthFromChrome returned error: %v", err)
	}
	if auth.WToken != wToken {
		t.Fatalf("expected wtoken %q, got %q", wToken, auth.WToken)
	}
	if auth.RefreshToken != wRefresh {
		t.Fatalf("expected refresh %q, got %q", wRefresh, auth.RefreshToken)
	}
	for _, cookie := range auth.Cookies {
		if strings.HasPrefix(cookie, "tracking=") {
			t.Fatalf("non-wolt cookie leaked into auth context: %q", cookie)
		}
		if strings.HasPrefix(cookie, "=") {
			t.Fatalf("empty-name cookie leaked into auth context: %q", cookie)
		}
	}
}

func TestReadAuthFromChromeFailsWhenNoWoltPages(t *testing.T) {
	fake := newFakeChromeDevTools(t, []map[string]any{}, "https://example.com/")
	defer fake.Close()

	_, err := readAuthFromChrome(context.Background(), fake.BrowserURL())
	if err == nil {
		t.Fatal("expected error when no Wolt pages are open in Chrome")
	}
}

// normalizingConfigStub mirrors the production config.Store's invariant that
// Account is the canonical source after Save: any Profiles[0] changes that
// don't also touch Account get overwritten on the next Load. Mocking Save as a
// pass-through (testConfigManager) hides that — using this stub catches it.
type normalizingConfigStub struct {
	cfg domain.Config
}

func (s *normalizingConfigStub) Path() string { return "/tmp/normalizing-config.json" }

func (s *normalizingConfigStub) Load(context.Context) (domain.Config, error) {
	return s.normalize(s.cfg), nil
}

func (s *normalizingConfigStub) Save(_ context.Context, cfg domain.Config) error {
	s.cfg = s.normalize(cfg)
	return nil
}

func (s *normalizingConfigStub) normalize(cfg domain.Config) domain.Config {
	account := cfg.Account
	if !accountHasDataLocal(account) && len(cfg.Profiles) > 0 {
		account = domain.Account{
			Location:      cfg.Profiles[0].Location,
			WToken:        cfg.Profiles[0].WToken,
			WRefreshToken: cfg.Profiles[0].WRefreshToken,
			Cookies:       cfg.Profiles[0].Cookies,
			WoltAddressID: cfg.Profiles[0].WoltAddressID,
		}
	}
	profile := domain.Profile{
		Name:          "default",
		IsDefault:     true,
		Location:      account.Location,
		WToken:        account.WToken,
		WRefreshToken: account.WRefreshToken,
		Cookies:       account.Cookies,
		WoltAddressID: account.WoltAddressID,
	}
	return domain.Config{Account: account, Profiles: []domain.Profile{profile}}
}

func accountHasDataLocal(account domain.Account) bool {
	return account.WToken != "" ||
		account.WRefreshToken != "" ||
		len(account.Cookies) > 0 ||
		account.WoltAddressID != "" ||
		account.Location.Lat != 0 ||
		account.Location.Lon != 0
}

func TestLogoutClearsSavedAccountState(t *testing.T) {
	cfg := &normalizingConfigStub{
		cfg: domain.Config{
			Account: domain.Account{
				WToken:        "header.payload.sig",
				WRefreshToken: "refresh-value",
				Cookies:       []string{"__wtoken=header.payload.sig", "tracking=drop"},
				WoltAddressID: "addr-1",
				Location:      domain.Location{Lat: 60.1, Lon: 24.9},
			},
		},
	}
	deps := Dependencies{Config: cfg}

	cmd := newLogoutCommand(deps)
	cmd.SetArgs([]string{"--format", "json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logout returned error: %v", err)
	}

	saved, err := cfg.Load(context.Background())
	if err != nil {
		t.Fatalf("load after logout: %v", err)
	}
	if saved.Account.WToken != "" || saved.Account.WRefreshToken != "" || len(saved.Account.Cookies) != 0 || saved.Account.WoltAddressID != "" {
		t.Fatalf("expected account auth fields cleared, got %+v", saved.Account)
	}
	if len(saved.Profiles) != 1 || saved.Profiles[0].WToken != "" || len(saved.Profiles[0].Cookies) != 0 {
		t.Fatalf("expected mirrored profile auth fields cleared, got %+v", saved.Profiles)
	}
	if saved.Account.Location.Lat == 0 && saved.Account.Location.Lon == 0 {
		t.Fatalf("expected logout to preserve location; got %+v", saved.Account.Location)
	}
}

func parseURLForTest(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("invalid test URL %q: %v", raw, err)
	}
	return parsed
}

func TestEnsureManagedChromeAcceptsRunningServer(t *testing.T) {
	fake := newFakeChromeDevTools(t, nil, "https://wolt.com/")
	defer fake.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := ensureManagedChrome(ctx, fake.BrowserURL()); err != nil {
		t.Fatalf("ensureManagedChrome should accept an already-running fake DevTools, got: %v", err)
	}

	parsed := parseURLForTest(t, fake.BrowserURL())
	if parsed.Host == "" {
		t.Fatalf("fake browser URL has no host: %q", fake.BrowserURL())
	}
}

// Regression: Wolt sets telemetry/consent cookies on every page load. An
// earlier version of loginViaManagedChrome used AuthContext.HasCredentials()
// to decide when to stop polling, which returns true on ANY cookie, so the
// command exited the moment the login page rendered — before the user could
// actually sign in. The fix narrowed the polling guard to require wtoken or
// wrtoken specifically.
func TestLoginViaManagedChromeWaitsForRealSession(t *testing.T) {
	telemetryOnly := []map[string]any{
		{"name": "telemetryDeviceId", "value": "abc-123", "domain": "wolt.com"},
		{"name": "telemetrySessionId", "value": "def-456", "domain": "wolt.com"},
		{"name": "cwc-consents", "value": "{}", "domain": "wolt.com"},
	}
	fake := newFakeChromeDevTools(t, telemetryOnly, "https://wolt.com/en/login")
	defer fake.Close()

	start := time.Now()
	_, err := loginViaManagedChrome(context.Background(), fake.BrowserURL(), "https://wolt.com/en/login", 750*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected loginViaManagedChrome to time out on telemetry-only cookies; got nil error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error message, got: %v", err)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("expected polling to wait until timeout (~750ms), got %s — suggests the loop exited early on noise cookies", elapsed)
	}
}

func TestLoginViaManagedChromeReturnsOnRealAuthCookie(t *testing.T) {
	withAuth := []map[string]any{
		{"name": "telemetryDeviceId", "value": "abc-123", "domain": "wolt.com"},
		{"name": "__wtoken", "value": "header.payload.sig", "domain": "wolt.com"},
		{"name": "__wrtoken", "value": "refresh-1", "domain": "wolt.com"},
	}
	fake := newFakeChromeDevTools(t, withAuth, "https://wolt.com/en/login")
	defer fake.Close()

	auth, err := loginViaManagedChrome(context.Background(), fake.BrowserURL(), "https://wolt.com/en/login", 5*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if auth.WToken == "" {
		t.Fatal("expected WToken to be populated from the real auth cookie")
	}
	if auth.RefreshToken == "" {
		t.Fatal("expected RefreshToken to be populated from the refresh cookie")
	}
}

func TestChromeAuthHasRealSession(t *testing.T) {
	cases := []struct {
		name string
		auth woltgateway.AuthContext
		want bool
	}{
		{"empty", woltgateway.AuthContext{}, false},
		{"telemetry cookies only", woltgateway.AuthContext{Cookies: []string{"telemetryDeviceId=abc", "cwc-consents={}"}}, false},
		{"whitespace wtoken", woltgateway.AuthContext{WToken: "   "}, false},
		{"real access token", woltgateway.AuthContext{WToken: "header.payload.sig"}, true},
		{"refresh token only", woltgateway.AuthContext{RefreshToken: "refresh-1"}, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := chromeAuthHasRealSession(tc.auth); got != tc.want {
				t.Fatalf("chromeAuthHasRealSession(%+v) = %v; want %v", tc.auth, got, tc.want)
			}
		})
	}
}

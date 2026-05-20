package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
	"github.com/mekedron/wolt-cli/internal/service/output"
	"github.com/spf13/cobra"
)

const (
	defaultLoginURL        = "https://wolt.com/login"
	defaultChromeDebugPort = 9222
)

func newLoginCommand(deps Dependencies) *cobra.Command {
	var flags globalFlags
	var wtoken string
	var wrtoken string
	var cookies []string
	var timeout time.Duration
	var browserURL string
	var loginURL string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in to Wolt and save this account locally.",
		Long: "Log in to Wolt and save this account locally.\n\n" +
			"Without token flags, this opens a managed Chrome window and extracts Wolt auth cookies through Chrome DevTools. " +
			"Use --wtoken/--wrtoken/--cookie for manual token login.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parseOutputFormat(flags.Format)
			if err != nil {
				return err
			}

			auth := buildAuthContext(globalFlags{WToken: wtoken, WRefreshToken: wrtoken, Cookies: cookies})
			if !auth.HasCredentials() {
				auth, err = loginViaManagedChrome(cmd.Context(), browserURL, loginURL, timeout)
				if err != nil {
					return err
				}
			}
			if err := saveAccountCredentials(cmd.Context(), deps, auth); err != nil {
				return err
			}

			data := map[string]any{
				"logged_in":          auth.HasCredentials(),
				"saved":              true,
				"session_expires_at": emptyToNil(tokenExpiryRFC3339(auth.WToken)),
			}
			warnings := []string{}
			if deps.Wolt != nil && auth.HasCredentials() {
				payload, authWarnings, userErr := invokeWithAuthAutoRefresh(
					cmd.Context(),
					deps,
					flags,
					&auth,
					func(authCtx woltgateway.AuthContext) (map[string]any, error) {
						return deps.Wolt.UserMe(cmd.Context(), authCtx)
					},
				)
				warnings = append(warnings, authWarnings...)
				if userErr != nil {
					warnings = append(warnings, "credentials saved but account validation failed")
				} else {
					user := asMap(payload["user"])
					data["user_id"] = domain.NormalizeID(coalesceAny(user["_id"], user["id"]))
					data["country"] = asString(coalesceAny(user["country"], payload["country"]))
				}
			}

			if format == output.FormatTable {
				return writeTable(cmd, buildLoginTable(data), flags.Output)
			}
			env := output.BuildEnvelope("default", flags.Locale, data, warnings, nil)
			return writeMachinePayload(cmd, env, format, flags.Output)
		},
	}

	cmd.Flags().StringVar(&wtoken, "wtoken", "", "Wolt access token, Bearer value, or copied auth payload.")
	cmd.Flags().StringVar(&wrtoken, "wrtoken", "", "Wolt refresh token or copied refresh payload.")
	cmd.Flags().StringArrayVar(&cookies, "cookie", nil, "Wolt cookie value to save (repeatable).")
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Minute, "How long to wait for browser login.")
	cmd.Flags().StringVar(&browserURL, "browser-url", fmt.Sprintf("http://127.0.0.1:%d", defaultChromeDebugPort), "Chrome DevTools browser URL.")
	cmd.Flags().StringVar(&loginURL, "login-url", defaultLoginURL, "Wolt login URL to open.")
	addGlobalFlags(cmd, &flags)
	return cmd
}

func newLogoutCommand(deps Dependencies) *cobra.Command {
	var flags globalFlags
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Log out locally by removing saved Wolt credentials.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, err := parseOutputFormat(flags.Format)
			if err != nil {
				return err
			}
			if deps.Config != nil {
				cfg, loadErr := deps.Config.Load(cmd.Context())
				if loadErr == nil {
					cfg.Account.WToken = ""
					cfg.Account.WRefreshToken = ""
					cfg.Account.Cookies = nil
					cfg.Account.WoltAddressID = ""
					if len(cfg.Profiles) > 0 {
						cfg.Profiles[0].WToken = ""
						cfg.Profiles[0].WRefreshToken = ""
						cfg.Profiles[0].Cookies = nil
						cfg.Profiles[0].WoltAddressID = ""
					}
					if err := deps.Config.Save(cmd.Context(), cfg); err != nil {
						return err
					}
				}
			}
			data := map[string]any{"logged_out": true}
			if format == output.FormatTable {
				return writeTable(cmd, output.RenderTable("Logout", []string{"Field", "Value"}, [][]string{{"Saved credentials", "removed"}}), flags.Output)
			}
			env := output.BuildEnvelope("default", flags.Locale, data, nil, nil)
			return writeMachinePayload(cmd, env, format, flags.Output)
		},
	}
	addGlobalFlags(cmd, &flags)
	return cmd
}

func saveAccountCredentials(ctx context.Context, deps Dependencies, auth woltgateway.AuthContext) error {
	if deps.Config == nil {
		return nil
	}
	cfg, err := deps.Config.Load(ctx)
	if err != nil || len(cfg.Profiles) == 0 {
		cfg = domain.Config{Profiles: []domain.Profile{{Name: "default", IsDefault: true}}}
	}
	wToken := normalizeWToken(auth.WToken)
	wRefresh := normalizeRefreshToken(auth.RefreshToken)
	cookies := normalizeCookieInputs(auth.Cookies)
	if wToken == "" {
		wToken = extractWTokenFromCookieInputs(cookies)
	}
	if wRefresh == "" {
		wRefresh = extractRefreshTokenFromCookieInputs(cookies)
	}
	cfg.Profiles[0].Name = "default"
	cfg.Profiles[0].IsDefault = true
	cfg.Profiles[0].WToken = wToken
	cfg.Profiles[0].WRefreshToken = wRefresh
	cfg.Profiles[0].Cookies = cookies
	cfg.Account.WToken = wToken
	cfg.Account.WRefreshToken = wRefresh
	cfg.Account.Cookies = cookies
	return deps.Config.Save(ctx, cfg)
}

func buildLoginTable(data map[string]any) string {
	return output.RenderTable("Login", []string{"Field", "Value"}, [][]string{
		{"Logged in", boolToYesNo(asBool(data["logged_in"]))},
		{"Saved", boolToYesNo(asBool(data["saved"]))},
		{"User ID", fallbackString(asString(data["user_id"]), "-")},
		{"Country", fallbackString(asString(data["country"]), "-")},
		{"Session expires", fallbackString(asString(data["session_expires_at"]), "-")},
	})
}

func loginViaManagedChrome(ctx context.Context, browserURL string, loginURL string, timeout time.Duration) (woltgateway.AuthContext, error) {
	browserURL = strings.TrimRight(strings.TrimSpace(browserURL), "/")
	if browserURL == "" {
		browserURL = fmt.Sprintf("http://127.0.0.1:%d", defaultChromeDebugPort)
	}
	if err := ensureManagedChrome(ctx, browserURL); err != nil {
		return woltgateway.AuthContext{}, err
	}
	if err := openChromeTarget(ctx, browserURL, loginURL); err != nil {
		return woltgateway.AuthContext{}, err
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		auth, err := readAuthFromChrome(ctx, browserURL)
		if err == nil && chromeAuthHasRealSession(auth) {
			return auth, nil
		}
		select {
		case <-ctx.Done():
			return woltgateway.AuthContext{}, ctx.Err()
		case <-time.After(1500 * time.Millisecond):
		}
	}
	return woltgateway.AuthContext{}, fmt.Errorf("timed out waiting for Wolt login in Chrome")
}

// chromeAuthHasRealSession reports whether the CDP cookie scrape produced a
// genuine signed-in session, not just the telemetry/consent cookies Wolt sets
// on every page load. The polling loop uses this to wait for the user to
// actually sign in instead of returning immediately on cookie noise.
func chromeAuthHasRealSession(auth woltgateway.AuthContext) bool {
	return strings.TrimSpace(auth.WToken) != "" || strings.TrimSpace(auth.RefreshToken) != ""
}

func ensureManagedChrome(ctx context.Context, browserURL string) error {
	if chromeDevToolsReady(ctx, browserURL) {
		return nil
	}
	profileDir := filepath.Join(defaultConfigDir(), "chrome-profile")
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return err
	}
	chromeBin := os.Getenv("CHROME_BIN")
	if chromeBin == "" {
		chromeBin = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if _, err := os.Stat(chromeBin); err != nil {
		return fmt.Errorf("chrome not found at %s; set CHROME_BIN or start Chrome with remote debugging", chromeBin)
	}
	port := defaultChromeDebugPort
	if parsed, err := url.Parse(browserURL); err == nil && parsed.Port() != "" {
		_, _ = fmt.Sscanf(parsed.Port(), "%d", &port)
	}
	cmd := exec.CommandContext(
		ctx,
		chromeBin,
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		fmt.Sprintf("--remote-debugging-port=%d", port),
		"--remote-debugging-address=127.0.0.1",
		"--no-first-run",
		"--no-default-browser-check",
	)
	if err := cmd.Start(); err != nil {
		return err
	}
	for i := 0; i < 40; i++ {
		if chromeDevToolsReady(ctx, browserURL) {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("chrome started but DevTools did not become available at %s", browserURL)
}

func defaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".wolt")
}

func chromeDevToolsReady(ctx context.Context, browserURL string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, browserURL+"/json/version", nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

func openChromeTarget(ctx context.Context, browserURL string, loginURL string) error {
	if strings.TrimSpace(loginURL) == "" {
		loginURL = defaultLoginURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, browserURL+"/json/new?"+url.QueryEscape(loginURL), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
	}
	return exec.CommandContext(ctx, "open", loginURL).Run()
}

type chromePage struct {
	Type                 string `json:"type"`
	URL                  string `json:"url"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func readAuthFromChrome(ctx context.Context, browserURL string) (woltgateway.AuthContext, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, browserURL+"/json/list", nil)
	if err != nil {
		return woltgateway.AuthContext{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return woltgateway.AuthContext{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	var pages []chromePage
	if err := json.NewDecoder(resp.Body).Decode(&pages); err != nil {
		return woltgateway.AuthContext{}, err
	}
	for _, page := range pages {
		if page.Type != "page" || page.WebSocketDebuggerURL == "" {
			continue
		}
		if !strings.Contains(page.URL, "wolt.") && !strings.Contains(page.URL, "wolt.com") {
			continue
		}
		client, err := newCDPClient(ctx, page.WebSocketDebuggerURL)
		if err != nil {
			continue
		}
		auth, err := client.readWoltAuth(ctx)
		_ = client.close()
		if err == nil && auth.HasCredentials() {
			return auth, nil
		}
	}
	return woltgateway.AuthContext{}, fmt.Errorf("no Wolt auth cookies found in Chrome")
}

type cdpClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
	next int
}

func newCDPClient(ctx context.Context, wsURL string) (*cdpClient, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, err
	}
	return &cdpClient{conn: conn}, nil
}

func (c *cdpClient) close() error {
	return c.conn.Close()
}

func (c *cdpClient) call(method string, params map[string]any) (map[string]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	if params == nil {
		params = map[string]any{}
	}
	if err := c.conn.WriteJSON(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		var msg map[string]any
		if err := c.conn.ReadJSON(&msg); err != nil {
			return nil, err
		}
		if asInt(msg["id"]) != id {
			continue
		}
		if errPayload := asMap(msg["error"]); errPayload != nil {
			return nil, fmt.Errorf("%s", asString(errPayload["message"]))
		}
		return asMap(msg["result"]), nil
	}
}

func (c *cdpClient) readWoltAuth(ctx context.Context) (woltgateway.AuthContext, error) {
	_ = ctx
	result, err := c.call("Network.getAllCookies", nil)
	if err != nil {
		return woltgateway.AuthContext{}, err
	}
	cookies := []string{}
	for _, rawCookie := range asSlice(result["cookies"]) {
		cookie := asMap(rawCookie)
		domainValue := strings.ToLower(asString(cookie["domain"]))
		name := strings.TrimSpace(asString(cookie["name"]))
		value := strings.TrimSpace(asString(cookie["value"]))
		if name == "" || value == "" || !strings.Contains(domainValue, "wolt") {
			continue
		}
		cookies = append(cookies, name+"="+value)
	}
	auth := woltgateway.AuthContext{Cookies: cookies}
	auth.WToken = extractWTokenFromCookieInputs(cookies)
	auth.RefreshToken = extractRefreshTokenFromCookieInputs(cookies)
	return auth, nil
}

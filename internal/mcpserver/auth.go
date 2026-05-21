package mcpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mekedron/wolt-cli/internal/domain"
	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

const tokenRefreshLeeway = 30 * time.Second

// ErrNotLoggedIn is returned from auth-required tools when no credentials are
// available in the persisted profile.
var ErrNotLoggedIn = errors.New("not logged in — run 'wolt login' in a terminal to sign in, then retry")

// loadProfile reads the current default profile from disk. A missing config is
// treated the same as a missing profile — callers see ErrNotLoggedIn.
func (tc *ToolCtx) loadProfile(ctx context.Context) (domain.Profile, error) {
	if tc.profiles == nil {
		return domain.Profile{}, ErrNotLoggedIn
	}
	profile, err := tc.profiles.Find(ctx, "")
	if err != nil {
		return domain.Profile{}, ErrNotLoggedIn
	}
	return profile, nil
}

// buildAuthContext constructs an AuthContext from a profile. Tokens persisted
// to disk by the CLI are already normalized, so no parsing is needed here.
func buildAuthContext(profile domain.Profile) woltgateway.AuthContext {
	return woltgateway.AuthContext{
		WToken:       strings.TrimSpace(profile.WToken),
		RefreshToken: strings.TrimSpace(profile.WRefreshToken),
		Cookies:      append([]string(nil), profile.Cookies...),
	}
}

// optionalAuth returns the persisted auth context if available, or an empty
// one otherwise. Use for tools that benefit from auth (personalized results)
// but work unauthenticated too.
func (tc *ToolCtx) optionalAuth(ctx context.Context) woltgateway.AuthContext {
	profile, err := tc.loadProfile(ctx)
	if err != nil {
		return woltgateway.AuthContext{}
	}
	return buildAuthContext(profile)
}

// requireAuth returns the persisted profile + auth context, or ErrNotLoggedIn
// if the user has not signed in.
func (tc *ToolCtx) requireAuth(ctx context.Context) (domain.Profile, woltgateway.AuthContext, error) {
	profile, err := tc.loadProfile(ctx)
	if err != nil {
		return domain.Profile{}, woltgateway.AuthContext{}, err
	}
	auth := buildAuthContext(profile)
	if !auth.HasCredentials() {
		return domain.Profile{}, woltgateway.AuthContext{}, ErrNotLoggedIn
	}
	return profile, auth, nil
}

// invokeWithRefresh calls fn with the supplied AuthContext. If the access
// token is JWT-expired going in, or fn returns a 401, it rotates tokens via
// the refresh endpoint, persists the new tokens, and retries once.
func invokeWithRefresh[T any](
	ctx context.Context,
	tc *ToolCtx,
	auth *woltgateway.AuthContext,
	fn func(woltgateway.AuthContext) (T, error),
) (T, error) {
	var zero T
	if auth == nil {
		return zero, fmt.Errorf("auth context is nil")
	}
	if tokenExpired(auth.WToken, time.Now().UTC(), tokenRefreshLeeway) {
		// Proactive refresh — ignore failure here; the call below may still
		// succeed if the upstream tolerates the stale token, otherwise we'll
		// retry after a reactive refresh.
		_ = tc.refreshTokens(ctx, auth)
	}

	result, err := fn(*auth)
	if err == nil {
		return result, nil
	}
	if !isUnauthorized(err) {
		return result, err
	}

	if refreshErr := tc.refreshTokens(ctx, auth); refreshErr != nil {
		return zero, fmt.Errorf("%w (token refresh also failed: %v)", err, refreshErr)
	}
	return fn(*auth)
}

func (tc *ToolCtx) refreshTokens(ctx context.Context, auth *woltgateway.AuthContext) error {
	refreshToken := strings.TrimSpace(auth.RefreshToken)
	if refreshToken == "" {
		return fmt.Errorf("no refresh token available")
	}
	if tc.wolt == nil {
		return fmt.Errorf("wolt client unavailable")
	}
	result, err := tc.wolt.RefreshAccessToken(ctx, refreshToken, *auth)
	if err != nil {
		return err
	}
	access := strings.TrimSpace(result.AccessToken)
	if access == "" {
		return fmt.Errorf("refresh response missing access token")
	}
	auth.WToken = access
	if r := strings.TrimSpace(result.RefreshToken); r != "" {
		auth.RefreshToken = r
	}
	if tc.config != nil {
		_ = tc.persistTokens(ctx, auth.WToken, auth.RefreshToken)
	}
	return nil
}

func (tc *ToolCtx) persistTokens(ctx context.Context, accessToken, refreshToken string) error {
	cfg, err := tc.config.Load(ctx)
	if err != nil {
		cfg = domain.Config{Profiles: []domain.Profile{{Name: "default", IsDefault: true}}}
	}
	if len(cfg.Profiles) == 0 {
		cfg.Profiles = []domain.Profile{{Name: "default", IsDefault: true}}
	}
	cfg.Profiles[0].Name = "default"
	cfg.Profiles[0].IsDefault = true
	if accessToken != "" {
		cfg.Profiles[0].WToken = accessToken
	}
	if refreshToken != "" {
		cfg.Profiles[0].WRefreshToken = refreshToken
	}
	return tc.config.Save(ctx, cfg)
}

func isUnauthorized(err error) bool {
	var upstream *woltgateway.UpstreamRequestError
	if !errors.As(err, &upstream) {
		return false
	}
	return upstream.StatusCode == 401
}

// tokenExpired returns true when the JWT's exp claim is in the past (with
// leeway). It quietly returns false on parse errors so the caller falls
// through to the reactive 401 path.
func tokenExpired(token string, now time.Time, leeway time.Duration) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Some tokens use padded encoding.
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return false
		}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return false
	}
	expiry := time.Unix(claims.Exp, 0).UTC()
	return now.After(expiry.Add(-leeway))
}

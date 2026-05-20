package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// slugCacheTTL is how long a slug→venue-id entry stays trusted before we
// re-resolve through the upstream static venue endpoint. Wolt's slug→id
// mapping is effectively permanent, so the TTL is mostly a safety net
// against the rare case where a venue is rebranded.
const slugCacheTTL = 24 * time.Hour

const (
	slugCacheFileName = ".wolt-slug-cache.json"
	envSlugCachePath  = "WOLT_SLUG_CACHE_PATH"
)

type venueSlugCache struct {
	Venues map[string]cachedVenueID `json:"venues"`
}

type cachedVenueID struct {
	ID        string         `json:"id"`
	Payload   map[string]any `json:"payload,omitempty"`
	FetchedAt time.Time      `json:"fetched_at"`
}

// slugCacheMu serializes concurrent read-modify-write cycles within a single
// process. We don't worry about multi-process races: the worst case is one
// stray static-page lookup followed by overwriting a fresh entry with another
// fresh entry, which is harmless.
var slugCacheMu sync.Mutex

// resolvedSlugCachePath returns the path of the on-disk cache. Test code
// overrides it via WOLT_SLUG_CACHE_PATH; otherwise we co-locate next to the
// config file when WOLT_CONFIG_PATH is set, falling back to
// ~/.wolt/.wolt-slug-cache.json.
func resolvedSlugCachePath() string {
	if override := strings.TrimSpace(os.Getenv(envSlugCachePath)); override != "" {
		return override
	}
	if configOverride := strings.TrimSpace(os.Getenv("WOLT_CONFIG_PATH")); configOverride != "" {
		return filepath.Join(filepath.Dir(configOverride), slugCacheFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".wolt", slugCacheFileName)
}

// loadVenueSlugCache reads the cache file. Missing or corrupt files return an
// empty cache — the CLI must never break because of a bad cache file.
func loadVenueSlugCache() venueSlugCache {
	path := resolvedSlugCachePath()
	if path == "" {
		return venueSlugCache{Venues: map[string]cachedVenueID{}}
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return venueSlugCache{Venues: map[string]cachedVenueID{}}
	}
	var cache venueSlugCache
	if err := json.Unmarshal(payload, &cache); err != nil {
		return venueSlugCache{Venues: map[string]cachedVenueID{}}
	}
	if cache.Venues == nil {
		cache.Venues = map[string]cachedVenueID{}
	}
	return cache
}

// saveVenueSlugCache writes the cache file with owner-only permissions. Any
// filesystem error is swallowed so a missing $HOME or read-only FS does not
// fail the CLI command that triggered the write.
func saveVenueSlugCache(cache venueSlugCache) {
	path := resolvedSlugCachePath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	payload, err := json.Marshal(cache)
	if err != nil {
		return
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return
	}
	_ = os.Chmod(path, 0o600)
}

// lookupCachedVenueID returns the cached id for a slug when the entry is
// fresh (within slugCacheTTL). Slugs that are empty, missing, or stale return
// "", false.
func lookupCachedVenueID(slug string) (string, bool) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return "", false
	}
	slugCacheMu.Lock()
	defer slugCacheMu.Unlock()
	cache := loadVenueSlugCache()
	entry, ok := cache.Venues[slug]
	if !ok {
		return "", false
	}
	if entry.ID == "" {
		return "", false
	}
	if time.Since(entry.FetchedAt) > slugCacheTTL {
		return "", false
	}
	return entry.ID, true
}

// rememberVenueID persists a freshly-resolved slug→id mapping. Empty inputs
// are no-ops so callers can safely pass the result of a failed lookup.
// Preserves any existing cached payload so id-only refreshes don't drop the
// full-static-payload cache.
func rememberVenueID(slug, id string) {
	slug = strings.TrimSpace(slug)
	id = strings.TrimSpace(id)
	if slug == "" || id == "" {
		return
	}
	slugCacheMu.Lock()
	defer slugCacheMu.Unlock()
	cache := loadVenueSlugCache()
	if cache.Venues == nil {
		cache.Venues = map[string]cachedVenueID{}
	}
	existing := cache.Venues[slug]
	existing.ID = id
	existing.FetchedAt = time.Now()
	cache.Venues[slug] = existing
	saveVenueSlugCache(cache)
}

// rememberVenueStatic persists the full venue static-page payload along with
// the extracted id so subsequent VenuePageStatic call sites can avoid the
// upstream round-trip. Used by cachedVenuePageStatic.
func rememberVenueStatic(slug, id string, payload map[string]any) {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(payload) == 0 {
		return
	}
	slugCacheMu.Lock()
	defer slugCacheMu.Unlock()
	cache := loadVenueSlugCache()
	if cache.Venues == nil {
		cache.Venues = map[string]cachedVenueID{}
	}
	cache.Venues[slug] = cachedVenueID{
		ID:        strings.TrimSpace(id),
		Payload:   payload,
		FetchedAt: time.Now(),
	}
	saveVenueSlugCache(cache)
}

// lookupCachedVenueStatic returns a previously-cached static page payload for
// a slug, or nil when there's no fresh entry. Stale entries are ignored.
func lookupCachedVenueStatic(slug string) map[string]any {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil
	}
	slugCacheMu.Lock()
	defer slugCacheMu.Unlock()
	cache := loadVenueSlugCache()
	entry, ok := cache.Venues[slug]
	if !ok || len(entry.Payload) == 0 {
		return nil
	}
	if time.Since(entry.FetchedAt) > slugCacheTTL {
		return nil
	}
	return entry.Payload
}

// cachedVenuePageStatic is the cached front-door for `deps.Wolt.VenuePageStatic`.
// Returns the cached payload on a fresh hit; otherwise calls upstream and
// persists the result. Errors from upstream are forwarded verbatim, and the
// cache is only written on success.
func cachedVenuePageStatic(ctx context.Context, deps Dependencies, slug string) (map[string]any, error) {
	if payload := lookupCachedVenueStatic(slug); payload != nil {
		return payload, nil
	}
	if deps.Wolt == nil {
		return nil, nil
	}
	payload, err := deps.Wolt.VenuePageStatic(ctx, slug)
	if err != nil {
		return nil, err
	}
	if len(payload) > 0 {
		rememberVenueStatic(slug, venueIDFromPayload(payload), payload)
	}
	return payload, nil
}

// clearVenueSlugCache deletes the cache file. Called from logout so that
// switching accounts can't reuse stale ids that may have been tied to the
// previous account's locale/coverage. Missing file is fine.
func clearVenueSlugCache() {
	path := resolvedSlugCachePath()
	if path == "" {
		return
	}
	slugCacheMu.Lock()
	defer slugCacheMu.Unlock()
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Swallow other errors — nothing the user can do about them and a
		// stale cache file is harmless.
		return
	}
}

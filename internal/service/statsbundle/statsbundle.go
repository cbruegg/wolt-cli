// Package statsbundle resolves and caches wolt-stats dashboard bundles
// published as GitHub Release assets. It owns the on-disk layout under
// <statsDir> and persists a small state file so re-runs are fast and
// resilient to no-network conditions.
//
// Layout:
//
//	<statsDir>/
//	  state.json
//	  bundles/<version>/    extracted bundle (index.html, _app/, manifest.json, ...)
//	  bundles/<version>/    older versions are kept around for rollback
//
// The bundles/ subdir may contain multiple versions; state.json's
// ActiveVersion field picks the live one. We use a state field instead of
// a "current" symlink so the layout is portable to Windows.
package statsbundle

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultRepo is the GitHub repo wolt-stats releases live in.
	DefaultRepo = "mekedron/wolt-stats"
	// DefaultAPIBase is the GitHub API base URL.
	DefaultAPIBase = "https://api.github.com"
	// DefaultUpdateCheckTTL throttles release lookups. GitHub's anonymous
	// API limit is 60 req/hr per IP, so 1×/hr is conservative and means an
	// offline rerun never blocks startup.
	DefaultUpdateCheckTTL = time.Hour
	// StateFileName is the JSON state file at the root of statsDir.
	StateFileName = "state.json"
	// BundlesDirName holds extracted bundle directories.
	BundlesDirName = "bundles"
	// MaxBundleBytes caps tarball download size to avoid runaway disk usage
	// in the face of a broken release. The current static bundle is ~1.5 MB.
	MaxBundleBytes = 50 << 20 // 50 MiB
)

// Release describes a single wolt-stats GitHub Release with its bundle assets.
type Release struct {
	Version     string    // tag name, e.g. "v0.1.0"
	HTMLURL     string    // human-facing release URL
	PublishedAt time.Time // GitHub-reported publish time
	TarballURL  string    // direct download URL for the .tar.gz bundle asset
	ChecksumURL string    // direct download URL for the .tar.gz.sha256 sibling
}

// State is persisted under <statsDir>/state.json. JSON keys are stable —
// they appear in wolt stats --format json output via BundleInfo.
type State struct {
	ActiveVersion string    `json:"active_version,omitempty"`
	Source        string    `json:"source,omitempty"`
	LastCheckedAt time.Time `json:"last_checked_at,omitempty"`
	ETag          string    `json:"etag,omitempty"`
	ReleaseURL    string    `json:"release_url,omitempty"`
	AssetURL      string    `json:"asset_url,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

// BundleInfo describes the active bundle on disk.
type BundleInfo struct {
	Version    string
	Path       string
	Source     string
	Downloaded bool
}

// EnsureOptions controls EnsureBundle behaviour.
type EnsureOptions struct {
	PinnedVersion   string        // if non-empty, force this version
	SkipUpdateCheck bool          // do not query the GitHub API; use cached state only
	ThrottleAfter   time.Duration // override DefaultUpdateCheckTTL
	Now             time.Time     // injectable clock for tests; defaults to time.Now()
}

// Manager mediates between the on-disk cache and the GitHub releases feed.
type Manager struct {
	StatsDir   string
	Repo       string
	APIBase    string
	HTTPClient *http.Client
}

// New returns a Manager rooted at statsDir. The directory and its bundles/
// subdir are created with mode 0o700 if they do not already exist.
func New(statsDir string) (*Manager, error) {
	if strings.TrimSpace(statsDir) == "" {
		return nil, errors.New("statsbundle: statsDir is required")
	}
	abs, err := filepath.Abs(statsDir)
	if err != nil {
		return nil, fmt.Errorf("statsbundle: resolve statsDir: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, BundlesDirName), 0o700); err != nil {
		return nil, fmt.Errorf("statsbundle: create bundles dir: %w", err)
	}
	return &Manager{
		StatsDir:   abs,
		Repo:       DefaultRepo,
		APIBase:    DefaultAPIBase,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// StatePath returns the absolute path to the state.json file.
func (m *Manager) StatePath() string {
	return filepath.Join(m.StatsDir, StateFileName)
}

// BundlePath returns the absolute directory for the given version.
func (m *Manager) BundlePath(version string) string {
	return filepath.Join(m.StatsDir, BundlesDirName, version)
}

// LoadState reads <statsDir>/state.json. Returns an empty State if the file
// does not exist — that's the cold-start condition, not an error.
func (m *Manager) LoadState() (State, error) {
	raw, err := os.ReadFile(m.StatePath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("statsbundle: read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil {
		return State{}, fmt.Errorf("statsbundle: parse state: %w", err)
	}
	return s, nil
}

// SaveState writes the state file atomically.
func (m *Manager) SaveState(s State) error {
	s.UpdatedAt = time.Now().UTC()
	payload, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("statsbundle: marshal state: %w", err)
	}
	tmp := m.StatePath() + ".tmp"
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return fmt.Errorf("statsbundle: write state tmp: %w", err)
	}
	if err := os.Rename(tmp, m.StatePath()); err != nil {
		return fmt.Errorf("statsbundle: replace state: %w", err)
	}
	return nil
}

// Active returns the current bundle on disk, or ErrNoActiveBundle if none
// has been installed yet.
func (m *Manager) Active() (BundleInfo, error) {
	state, err := m.LoadState()
	if err != nil {
		return BundleInfo{}, err
	}
	if strings.TrimSpace(state.ActiveVersion) == "" {
		return BundleInfo{}, ErrNoActiveBundle
	}
	path := m.BundlePath(state.ActiveVersion)
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		return BundleInfo{}, ErrNoActiveBundle
	}
	return BundleInfo{
		Version: state.ActiveVersion,
		Path:    path,
		Source:  state.Source,
	}, nil
}

// ErrNoActiveBundle indicates that no bundle has been extracted yet.
var ErrNoActiveBundle = errors.New("statsbundle: no active bundle installed")

// ErrNoBundleAvailable indicates that the GitHub Release exists but does
// not carry a usable bundle asset.
var ErrNoBundleAvailable = errors.New("statsbundle: release has no bundle asset")

// ErrChecksumMismatch indicates a downloaded asset's SHA256 did not match
// the published checksum.
var ErrChecksumMismatch = errors.New("statsbundle: bundle checksum mismatch")

// EnsureBundle returns an installed bundle, downloading from GitHub if
// necessary. The decision tree:
//
//  1. If opts.PinnedVersion is set, ensure exactly that version is installed.
//  2. Else, if the active bundle exists and the update check is throttled
//     (last check was within ThrottleAfter), reuse the active bundle.
//  3. Else, query the GitHub API for the latest release. If it matches the
//     active version, refresh LastCheckedAt and return the active bundle.
//     Otherwise download + extract + flip the active pointer.
//
// EnsureBundle never deletes older bundles — the cache is append-only so a
// caller can roll back by editing state.json. The cold-start failure mode
// is reported via the returned error; if a cached active bundle exists and
// the remote lookup fails, EnsureBundle prefers the cache (so air-gapped
// reruns keep working).
func (m *Manager) EnsureBundle(ctx context.Context, opts EnsureOptions) (BundleInfo, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	throttle := opts.ThrottleAfter
	if throttle <= 0 {
		throttle = DefaultUpdateCheckTTL
	}

	state, err := m.LoadState()
	if err != nil {
		return BundleInfo{}, err
	}
	active, activeErr := m.Active()

	if pin := strings.TrimSpace(opts.PinnedVersion); pin != "" {
		if activeErr == nil && active.Version == pin {
			return active, nil
		}
		release, lookupErr := m.lookupRelease(ctx, pin, "")
		if lookupErr != nil {
			return BundleInfo{}, lookupErr
		}
		return m.installRelease(ctx, release, &state)
	}

	if opts.SkipUpdateCheck {
		if activeErr == nil {
			return active, nil
		}
		return BundleInfo{}, fmt.Errorf("statsbundle: --no-check-updates set but no cached bundle exists: %w", activeErr)
	}

	if activeErr == nil && !state.LastCheckedAt.IsZero() && now.Sub(state.LastCheckedAt) < throttle {
		return active, nil
	}

	release, notModified, newETag, lookupErr := m.fetchLatestRelease(ctx, state.ETag)
	if lookupErr != nil {
		if activeErr == nil {
			// Network or API hiccup but we have a cached bundle — log via
			// state metadata, return cache.
			return active, nil
		}
		return BundleInfo{}, fmt.Errorf("statsbundle: latest release lookup failed: %w", lookupErr)
	}
	if notModified {
		state.LastCheckedAt = now
		if newETag != "" {
			state.ETag = newETag
		}
		_ = m.SaveState(state)
		if activeErr == nil {
			return active, nil
		}
		// 304 with no cache should never happen — guard anyway.
		return BundleInfo{}, errors.New("statsbundle: server returned 304 but no cached bundle exists")
	}
	state.ETag = newETag
	state.LastCheckedAt = now
	if activeErr == nil && active.Version == release.Version {
		_ = m.SaveState(state)
		return active, nil
	}
	return m.installRelease(ctx, release, &state)
}

// installRelease downloads, verifies, and extracts the bundle asset, then
// flips ActiveVersion. State is saved before returning.
func (m *Manager) installRelease(ctx context.Context, release Release, state *State) (BundleInfo, error) {
	if strings.TrimSpace(release.TarballURL) == "" {
		return BundleInfo{}, ErrNoBundleAvailable
	}
	target := m.BundlePath(release.Version)
	if err := m.downloadAndExtract(ctx, release, target); err != nil {
		return BundleInfo{}, err
	}
	state.ActiveVersion = release.Version
	state.Source = "github-release"
	state.ReleaseURL = release.HTMLURL
	state.AssetURL = release.TarballURL
	if err := m.SaveState(*state); err != nil {
		return BundleInfo{}, err
	}
	return BundleInfo{
		Version:    release.Version,
		Path:       target,
		Source:     state.Source,
		Downloaded: true,
	}, nil
}

func (m *Manager) downloadAndExtract(ctx context.Context, release Release, destDir string) error {
	tmp, err := os.CreateTemp(m.StatsDir, "bundle-*.tar.gz")
	if err != nil {
		return fmt.Errorf("statsbundle: create tmp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()

	tarballResp, err := m.do(ctx, http.MethodGet, release.TarballURL, nil)
	if err != nil {
		return fmt.Errorf("statsbundle: GET tarball: %w", err)
	}
	defer func() { _ = tarballResp.Body.Close() }()
	if tarballResp.StatusCode != http.StatusOK {
		return fmt.Errorf("statsbundle: tarball download status %d", tarballResp.StatusCode)
	}

	hasher := sha256.New()
	limited := io.LimitReader(tarballResp.Body, MaxBundleBytes+1)
	written, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		return fmt.Errorf("statsbundle: write tarball: %w", err)
	}
	if written > MaxBundleBytes {
		return fmt.Errorf("statsbundle: tarball exceeds %d bytes", MaxBundleBytes)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("statsbundle: sync tarball: %w", err)
	}

	if strings.TrimSpace(release.ChecksumURL) != "" {
		expected, err := m.fetchChecksum(ctx, release.ChecksumURL)
		if err != nil {
			return fmt.Errorf("statsbundle: fetch checksum: %w", err)
		}
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(expected, got) {
			return fmt.Errorf("%w: expected %s, got %s", ErrChecksumMismatch, expected, got)
		}
	}

	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("statsbundle: rewind tarball: %w", err)
	}
	// Extract into a staging dir then atomically rename so a partial
	// extract never leaves a half-written active bundle behind.
	staging := destDir + ".staging"
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("statsbundle: clean staging: %w", err)
	}
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return fmt.Errorf("statsbundle: mkdir staging: %w", err)
	}
	if err := extractTarGz(tmp, staging); err != nil {
		_ = os.RemoveAll(staging)
		return err
	}
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("statsbundle: remove old bundle: %w", err)
	}
	if err := os.Rename(staging, destDir); err != nil {
		return fmt.Errorf("statsbundle: rename staging: %w", err)
	}
	return nil
}

func extractTarGz(r io.Reader, destDir string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("statsbundle: gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	cleanDest := filepath.Clean(destDir)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("statsbundle: tar next: %w", err)
		}
		name := filepath.Clean(header.Name)
		if name == "." {
			continue
		}
		if name == ".." || strings.HasPrefix(name, ".."+string(filepath.Separator)) || filepath.IsAbs(name) {
			return fmt.Errorf("statsbundle: refusing to write outside dest: %s", header.Name)
		}
		target := filepath.Join(cleanDest, name)
		rel, err := filepath.Rel(cleanDest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("statsbundle: refusing to write outside dest: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("statsbundle: mkdir %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("statsbundle: mkdir for %s: %w", target, err)
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return fmt.Errorf("statsbundle: create %s: %w", target, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("statsbundle: write %s: %w", target, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("statsbundle: close %s: %w", target, err)
			}
		case tar.TypeSymlink, tar.TypeLink:
			// Skip — published bundles should not contain links; ignoring
			// keeps the extraction tamper-proof.
			continue
		default:
			continue
		}
	}
}

func (m *Manager) fetchChecksum(ctx context.Context, checksumURL string) (string, error) {
	resp, err := m.do(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("statsbundle: checksum status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if err != nil {
		return "", err
	}
	// Accept either "<hex>" alone or "<hex>  filename" (shasum format).
	first := strings.Fields(strings.TrimSpace(string(body)))
	if len(first) == 0 {
		return "", errors.New("statsbundle: empty checksum file")
	}
	return strings.ToLower(strings.TrimSpace(first[0])), nil
}

// fetchLatestRelease queries the GitHub API for the most recent release,
// honoring ETag for 304 fast-path.
func (m *Manager) fetchLatestRelease(ctx context.Context, etag string) (Release, bool, string, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(m.APIBase, "/"), m.Repo)
	headers := map[string]string{
		"Accept": "application/vnd.github+json",
	}
	if strings.TrimSpace(etag) != "" {
		headers["If-None-Match"] = etag
	}
	resp, err := m.doWithHeaders(ctx, http.MethodGet, endpoint, headers)
	if err != nil {
		return Release{}, false, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		return Release{}, true, resp.Header.Get("ETag"), nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Release{}, false, "", fmt.Errorf("statsbundle: latest release status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	release, err := parseReleaseResponse(resp.Body)
	if err != nil {
		return Release{}, false, "", err
	}
	return release, false, resp.Header.Get("ETag"), nil
}

func (m *Manager) lookupRelease(ctx context.Context, tag string, etag string) (Release, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/releases/tags/%s", strings.TrimRight(m.APIBase, "/"), m.Repo, url.PathEscape(tag))
	headers := map[string]string{
		"Accept": "application/vnd.github+json",
	}
	if strings.TrimSpace(etag) != "" {
		headers["If-None-Match"] = etag
	}
	resp, err := m.doWithHeaders(ctx, http.MethodGet, endpoint, headers)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Release{}, fmt.Errorf("statsbundle: release %s status %d: %s", tag, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return parseReleaseResponse(resp.Body)
}

type apiAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type apiRelease struct {
	TagName     string     `json:"tag_name"`
	HTMLURL     string     `json:"html_url"`
	PublishedAt time.Time  `json:"published_at"`
	Assets      []apiAsset `json:"assets"`
}

func parseReleaseResponse(body io.Reader) (Release, error) {
	var payload apiRelease
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return Release{}, fmt.Errorf("statsbundle: decode release: %w", err)
	}
	release := Release{
		Version:     payload.TagName,
		HTMLURL:     payload.HTMLURL,
		PublishedAt: payload.PublishedAt,
	}
	for _, asset := range payload.Assets {
		name := strings.ToLower(asset.Name)
		switch {
		case strings.HasSuffix(name, ".tar.gz.sha256"):
			release.ChecksumURL = asset.BrowserDownloadURL
		case strings.HasSuffix(name, ".tar.gz"):
			release.TarballURL = asset.BrowserDownloadURL
		}
	}
	if release.TarballURL == "" {
		return release, ErrNoBundleAvailable
	}
	return release, nil
}

func (m *Manager) do(ctx context.Context, method, target string, headers map[string]string) (*http.Response, error) {
	return m.doWithHeaders(ctx, method, target, headers)
}

func (m *Manager) doWithHeaders(ctx context.Context, method, target string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return m.HTTPClient.Do(req)
}

package statsbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadStateMissingFileReturnsZero(t *testing.T) {
	m := newTestManager(t, nil)
	state, err := m.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if state.ActiveVersion != "" {
		t.Fatalf("expected empty active version, got %q", state.ActiveVersion)
	}
}

func TestSaveAndLoadStateRoundTrip(t *testing.T) {
	m := newTestManager(t, nil)
	want := State{ActiveVersion: "v0.1.0", Source: "github-release", ETag: `"abc"`, LastCheckedAt: time.Now().UTC().Truncate(time.Second)}
	if err := m.SaveState(want); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := m.LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if got.ActiveVersion != want.ActiveVersion || got.ETag != want.ETag {
		t.Fatalf("round-trip mismatch: want=%+v got=%+v", want, got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("expected UpdatedAt to be populated on save")
	}
}

func TestActiveWithoutInstallReturnsSentinel(t *testing.T) {
	m := newTestManager(t, nil)
	_, err := m.Active()
	if !errors.Is(err, ErrNoActiveBundle) {
		t.Fatalf("expected ErrNoActiveBundle, got %v", err)
	}
}

func TestActiveWithStaleStateButMissingDirReturnsSentinel(t *testing.T) {
	m := newTestManager(t, nil)
	if err := m.SaveState(State{ActiveVersion: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Active(); !errors.Is(err, ErrNoActiveBundle) {
		t.Fatalf("expected ErrNoActiveBundle for missing dir, got %v", err)
	}
}

func TestEnsureBundleDownloadsAndExtracts(t *testing.T) {
	bundle := makeTarGz(t, map[string]string{
		"index.html":         "<title>dashboard</title>",
		"_app/manifest.json": `{"ok":true}`,
		"manifest.json":      `{"version":"v0.1.0"}`,
	})

	stub := newGitHubStub(t, gitHubStubFixture{
		Tag:        "v0.1.0",
		LatestETag: `"v0.1.0"`,
		BundleBody: bundle,
		BundleSHA:  sha256Hex(bundle),
	})
	defer stub.Server.Close()

	m := stub.NewManager(t)
	info, err := m.EnsureBundle(context.Background(), EnsureOptions{})
	if err != nil {
		t.Fatalf("EnsureBundle: %v", err)
	}
	if info.Version != "v0.1.0" || !info.Downloaded {
		t.Fatalf("unexpected info: %+v", info)
	}
	if !fileExists(filepath.Join(info.Path, "index.html")) {
		t.Fatal("index.html missing after extract")
	}
	if !fileExists(filepath.Join(info.Path, "_app/manifest.json")) {
		t.Fatal("nested asset missing after extract")
	}
	state, _ := m.LoadState()
	if state.ActiveVersion != "v0.1.0" {
		t.Fatalf("state not updated: %+v", state)
	}
	if state.ETag != `"v0.1.0"` {
		t.Fatalf("etag not captured: %+v", state)
	}
}

func TestEnsureBundleHonorsThrottle(t *testing.T) {
	bundle := makeTarGz(t, map[string]string{"index.html": "ok"})
	stub := newGitHubStub(t, gitHubStubFixture{
		Tag:        "v0.1.0",
		LatestETag: `"v0.1.0"`,
		BundleBody: bundle,
		BundleSHA:  sha256Hex(bundle),
	})
	defer stub.Server.Close()

	m := stub.NewManager(t)
	now := time.Now().UTC()
	if _, err := m.EnsureBundle(context.Background(), EnsureOptions{Now: now}); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	stub.resetLatestCalls()

	if _, err := m.EnsureBundle(context.Background(), EnsureOptions{Now: now.Add(30 * time.Minute)}); err != nil {
		t.Fatalf("throttled ensure: %v", err)
	}
	if calls := stub.latestCallCount(); calls != 0 {
		t.Fatalf("expected zero remote calls under throttle, got %d", calls)
	}
}

func TestEnsureBundleSkipUpdateWithoutCacheFails(t *testing.T) {
	m := newTestManager(t, nil)
	_, err := m.EnsureBundle(context.Background(), EnsureOptions{SkipUpdateCheck: true})
	if err == nil {
		t.Fatal("expected error when no cache and skip flag set")
	}
}

func TestEnsureBundlePrefersCacheOnRemoteFailure(t *testing.T) {
	bundle := makeTarGz(t, map[string]string{"index.html": "ok"})
	stub := newGitHubStub(t, gitHubStubFixture{
		Tag:        "v0.1.0",
		LatestETag: `"v0.1.0"`,
		BundleBody: bundle,
		BundleSHA:  sha256Hex(bundle),
	})
	m := stub.NewManager(t)
	if _, err := m.EnsureBundle(context.Background(), EnsureOptions{}); err != nil {
		t.Fatalf("initial ensure: %v", err)
	}
	stub.Server.Close()

	state, _ := m.LoadState()
	state.LastCheckedAt = time.Now().Add(-48 * time.Hour)
	if err := m.SaveState(state); err != nil {
		t.Fatal(err)
	}
	info, err := m.EnsureBundle(context.Background(), EnsureOptions{})
	if err != nil {
		t.Fatalf("expected to fall back to cache, got %v", err)
	}
	if info.Version != "v0.1.0" {
		t.Fatalf("expected cached v0.1.0, got %+v", info)
	}
}

func TestEnsureBundleRejectsChecksumMismatch(t *testing.T) {
	bundle := makeTarGz(t, map[string]string{"index.html": "ok"})
	stub := newGitHubStub(t, gitHubStubFixture{
		Tag:        "v0.1.0",
		LatestETag: `"v0.1.0"`,
		BundleBody: bundle,
		BundleSHA:  strings.Repeat("0", 64),
	})
	defer stub.Server.Close()

	m := stub.NewManager(t)
	_, err := m.EnsureBundle(context.Background(), EnsureOptions{})
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("expected ErrChecksumMismatch, got %v", err)
	}
}

func TestEnsureBundleNotModifiedReturnsCache(t *testing.T) {
	bundle := makeTarGz(t, map[string]string{"index.html": "ok"})
	stub := newGitHubStub(t, gitHubStubFixture{
		Tag:        "v0.1.0",
		LatestETag: `"v0.1.0"`,
		BundleBody: bundle,
		BundleSHA:  sha256Hex(bundle),
	})
	defer stub.Server.Close()

	m := stub.NewManager(t)
	first, err := m.EnsureBundle(context.Background(), EnsureOptions{})
	if err != nil {
		t.Fatalf("cold install: %v", err)
	}
	stub.forceNotModified()

	state, _ := m.LoadState()
	state.LastCheckedAt = time.Time{}
	if err := m.SaveState(state); err != nil {
		t.Fatal(err)
	}
	second, err := m.EnsureBundle(context.Background(), EnsureOptions{Now: time.Now().UTC()})
	if err != nil {
		t.Fatalf("304 ensure: %v", err)
	}
	if second.Version != first.Version {
		t.Fatalf("304 should return same version: %q vs %q", first.Version, second.Version)
	}
	if second.Downloaded {
		t.Fatal("304 path should not re-download")
	}
}

func TestExtractTarGzRejectsPathEscape(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	header := &tar.Header{Name: "../escape.txt", Mode: 0o644, Size: 3, Typeflag: tar.TypeReg}
	_ = tw.WriteHeader(header)
	_, _ = tw.Write([]byte("hi\n"))
	_ = tw.Close()
	_ = gz.Close()
	dest := t.TempDir()
	err := extractTarGz(&buf, dest)
	if err == nil {
		t.Fatal("expected error for path escape")
	}
}

// ---------- test stub ----------

type gitHubStubFixture struct {
	Tag        string
	LatestETag string
	BundleBody []byte
	BundleSHA  string
}

type gitHubStub struct {
	Server          *httptest.Server
	fixture         gitHubStubFixture
	mu              sync.Mutex
	latestCalls     int
	notModifiedNext bool
}

func newGitHubStub(t *testing.T, fx gitHubStubFixture) *gitHubStub {
	t.Helper()
	stub := &gitHubStub{fixture: fx}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/mekedron/wolt-stats/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.latestCalls++
		notMod := stub.notModifiedNext
		stub.notModifiedNext = false
		etag := stub.fixture.LatestETag
		stub.mu.Unlock()
		if notMod || r.Header.Get("If-None-Match") == etag {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stub.releasePayload())
	})
	mux.HandleFunc("/repos/mekedron/wolt-stats/releases/tags/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(stub.releasePayload())
	})
	mux.HandleFunc("/asset.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(stub.fixture.BundleBody)
	})
	mux.HandleFunc("/asset.tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, "%s  asset.tar.gz\n", stub.fixture.BundleSHA)
	})
	stub.Server = httptest.NewServer(mux)
	return stub
}

func (s *gitHubStub) releasePayload() apiRelease {
	base := s.Server.URL
	return apiRelease{
		TagName:     s.fixture.Tag,
		HTMLURL:     "https://example.test/release/" + s.fixture.Tag,
		PublishedAt: time.Now().UTC(),
		Assets: []apiAsset{
			{Name: "wolt-stats-bundle-" + s.fixture.Tag + ".tar.gz", BrowserDownloadURL: base + "/asset.tar.gz"},
			{Name: "wolt-stats-bundle-" + s.fixture.Tag + ".tar.gz.sha256", BrowserDownloadURL: base + "/asset.tar.gz.sha256"},
		},
	}
}

func (s *gitHubStub) resetLatestCalls() {
	s.mu.Lock()
	s.latestCalls = 0
	s.mu.Unlock()
}

func (s *gitHubStub) latestCallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latestCalls
}

func (s *gitHubStub) forceNotModified() {
	s.mu.Lock()
	s.notModifiedNext = true
	s.mu.Unlock()
}

func (s *gitHubStub) NewManager(t *testing.T) *Manager {
	t.Helper()
	m := newTestManager(t, nil)
	m.APIBase = s.Server.URL
	m.HTTPClient = s.Server.Client()
	return m
}

func newTestManager(t *testing.T, _ *gitHubStub) *Manager {
	t.Helper()
	m, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for path, contents := range files {
		header := &tar.Header{Name: path, Mode: 0o644, Size: int64(len(contents)), Typeflag: tar.TypeReg, ModTime: time.Now()}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatalf("write header %s: %v", path, err)
		}
		if _, err := tw.Write([]byte(contents)); err != nil {
			t.Fatalf("write body %s: %v", path, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

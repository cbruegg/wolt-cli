package statsserve

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartServesBundleAndDatabase(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, filepath.Join(bundleDir, "index.html"), []byte("<!doctype html><title>dashboard</title>"))
	writeFile(t, filepath.Join(bundleDir, "_app", "manifest.json"), []byte(`{"ok":true}`))

	dbPath := filepath.Join(t.TempDir(), "wolt-history.sqlite")
	writeFile(t, dbPath, []byte("SQLITE_BYTES_PLACEHOLDER"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	srv, err := Start(Options{
		BundleDir: bundleDir,
		DBPath:    dbPath,
		Listener:  ln,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	if !strings.HasPrefix(srv.URL(), "http://127.0.0.1:") {
		t.Fatalf("expected localhost URL, got %q", srv.URL())
	}
	if srv.Port() == 0 {
		t.Fatal("expected resolved port")
	}

	indexBody := httpGet(t, srv.URL()+"/")
	if !strings.Contains(indexBody, "dashboard") {
		t.Fatalf("index body missing 'dashboard': %q", indexBody)
	}

	nestedBody := httpGet(t, srv.URL()+"/_app/manifest.json")
	if !strings.Contains(nestedBody, `"ok":true`) {
		t.Fatalf("nested asset body unexpected: %q", nestedBody)
	}

	dbBody := httpGet(t, srv.URL()+DatabaseRoute)
	if dbBody != "SQLITE_BYTES_PLACEHOLDER" {
		t.Fatalf("db body mismatch: %q", dbBody)
	}
}

func TestStartServesMissingDatabaseAs404(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, filepath.Join(bundleDir, "index.html"), []byte("<!doctype html><title>empty</title>"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv, err := Start(Options{BundleDir: bundleDir, Listener: ln})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	resp, err := http.Get(srv.URL() + DatabaseRoute)
	if err != nil {
		t.Fatalf("GET db: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when DB absent, got %d", resp.StatusCode)
	}
}

func TestStartRejectsMissingBundleDir(t *testing.T) {
	_, err := Start(Options{BundleDir: filepath.Join(t.TempDir(), "does-not-exist")})
	if err == nil {
		t.Fatal("expected error for missing bundle dir")
	}
}

func TestStartRejectsBundleDirThatIsAFile(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "not-a-dir")
	writeFile(t, tmpFile, []byte("oops"))
	_, err := Start(Options{BundleDir: tmpFile})
	if err == nil {
		t.Fatal("expected error when bundle dir is a regular file")
	}
}

func TestStartRejectsMutatingDatabase(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, filepath.Join(bundleDir, "index.html"), []byte("ok"))
	dbPath := filepath.Join(t.TempDir(), "db.sqlite")
	writeFile(t, dbPath, []byte("x"))

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv, err := Start(Options{BundleDir: bundleDir, DBPath: dbPath, Listener: ln})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	req, _ := http.NewRequest(http.MethodPost, srv.URL()+DatabaseRoute, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST db: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Allow"); !strings.Contains(got, "GET") {
		t.Fatalf("expected Allow header to mention GET, got %q", got)
	}
}

func TestListenWithRetrySkipsBusyPort(t *testing.T) {
	// Occupy the first port so the helper has to roll forward.
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen first: %v", err)
	}
	defer func() { _ = first.Close() }()
	busy := first.Addr().(*net.TCPAddr).Port

	ln, port, err := listenWithRetry("127.0.0.1", busy, 5)
	if err != nil {
		t.Fatalf("listenWithRetry: %v", err)
	}
	defer func() { _ = ln.Close() }()
	if port == busy {
		t.Fatalf("expected port roll-forward, got %d", port)
	}
}

func TestShutdownIsIdempotent(t *testing.T) {
	bundleDir := t.TempDir()
	writeFile(t, filepath.Join(bundleDir, "index.html"), []byte("ok"))
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv, err := Start(Options{BundleDir: bundleDir, Listener: ln})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("second shutdown should be a no-op: %v", err)
	}
	if err := srv.Wait(); err != nil {
		t.Fatalf("Wait after shutdown: %v", err)
	}
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, contents, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body %s: %v", url, err)
	}
	return string(body)
}

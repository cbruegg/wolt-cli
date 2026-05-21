// Package statsserve runs a localhost HTTP server that exposes a wolt-stats
// dashboard bundle and the user's SQLite order history. The server is bound
// to 127.0.0.1 only — order history is sensitive personal data and must not
// be reachable from the LAN.
package statsserve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultPort matches the Vite dev server default so users get the URL they
// already have muscle memory for.
const DefaultPort = 5173

// PortAutoPickLimit caps how many sequential ports we try when the requested
// one is busy. 5 is enough to cover the common case (one or two other Vite
// dev servers already running) without surprising the user with a far-off port.
const PortAutoPickLimit = 5

// DatabaseRoute is the URL path the wolt-stats bundle reads its SQLite file
// from by default. Hard-coded because the bundle's compile-time
// PUBLIC_WOLT_STATS_DATABASE_PATH default ("data/wolt-history.sqlite") is
// what we serve here — keeping these in sync avoids a per-user rebuild.
const DatabaseRoute = "/data/wolt-history.sqlite"

// Options configures a Server.
type Options struct {
	// BundleDir is the on-disk directory containing the extracted wolt-stats
	// static bundle (index.html, _app/, etc.).
	BundleDir string
	// DBPath is the absolute path to the SQLite file served at DatabaseRoute.
	// It may not exist yet (the dashboard surfaces an empty-state).
	DBPath string
	// Port is the preferred port. If 0, DefaultPort is used. If the port is
	// busy, the server tries Port+1, Port+2, ... up to PortAutoPickLimit.
	Port int
	// Host overrides the bind address. Defaults to 127.0.0.1.
	// Tests may pass "127.0.0.1:0" via Listener instead.
	Host string
	// Listener, if non-nil, is used directly and Port/Host are ignored.
	// Intended for tests that want an ephemeral port via net.Listen.
	Listener net.Listener
}

// Server is a running stats dashboard HTTP server.
type Server struct {
	srv     *http.Server
	ln      net.Listener
	url     string
	port    int
	once    sync.Once
	done    chan struct{}
	exitErr error
}

// Start opens a listener and begins serving. It returns immediately; use
// Wait to block on the server lifetime and Shutdown to stop it.
func Start(opts Options) (*Server, error) {
	if strings.TrimSpace(opts.BundleDir) == "" {
		return nil, errors.New("statsserve: BundleDir is required")
	}
	if info, err := os.Stat(opts.BundleDir); err != nil {
		return nil, fmt.Errorf("statsserve: bundle dir %q: %w", opts.BundleDir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("statsserve: bundle dir %q is not a directory", opts.BundleDir)
	}

	host := strings.TrimSpace(opts.Host)
	if host == "" {
		host = "127.0.0.1"
	}

	ln := opts.Listener
	var port int
	if ln == nil {
		requested := opts.Port
		if requested == 0 {
			requested = DefaultPort
		}
		var err error
		ln, port, err = listenWithRetry(host, requested, PortAutoPickLimit)
		if err != nil {
			return nil, err
		}
	} else {
		if addr, ok := ln.Addr().(*net.TCPAddr); ok {
			port = addr.Port
		}
	}

	mux := buildMux(opts.BundleDir, opts.DBPath)
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	s := &Server{
		srv:  srv,
		ln:   ln,
		url:  fmt.Sprintf("http://%s", ln.Addr().String()),
		port: port,
		done: make(chan struct{}),
	}

	go func() {
		err := srv.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.exitErr = err
		}
		close(s.done)
	}()

	return s, nil
}

// URL returns the http://host:port the server is bound to.
func (s *Server) URL() string { return s.url }

// Port returns the resolved TCP port.
func (s *Server) Port() int { return s.port }

// Wait blocks until the server has exited. It is safe to call multiple times.
func (s *Server) Wait() error {
	<-s.done
	return s.exitErr
}

// Shutdown gracefully stops the server, respecting ctx's deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	var err error
	s.once.Do(func() {
		err = s.srv.Shutdown(ctx)
	})
	return err
}

func buildMux(bundleDir, dbPath string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(DatabaseRoute, func(w http.ResponseWriter, r *http.Request) {
		// Reject anything but GET/HEAD — the dashboard only reads.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if strings.TrimSpace(dbPath) == "" {
			http.NotFound(w, r)
			return
		}
		if _, err := os.Stat(dbPath); err != nil {
			http.NotFound(w, r)
			return
		}
		// Tell the browser this asset is volatile (re-synced on every wolt
		// stats run). The dashboard fetches once on load, so this just
		// matters on hard reload.
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, dbPath)
	})

	fileServer := http.FileServer(http.Dir(filepath.Clean(bundleDir)))
	mux.Handle("/", fileServer)
	return mux
}

func listenWithRetry(host string, startPort, attempts int) (net.Listener, int, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		port := startPort + i
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("statsserve: no free port in [%d,%d]: %w", startPort, startPort+attempts-1, lastErr)
}

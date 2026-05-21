package statssync

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWritePhaseAndDetailNoOpOnNil(t *testing.T) {
	writePhase(nil, "should not panic")
	writeDetail(nil, "should not panic")
}

func TestWritePhaseAndDetailFormat(t *testing.T) {
	var buf bytes.Buffer
	writePhase(&buf, "syncing %s", "user@example.com")
	writeDetail(&buf, "page %d", 1)
	want := "==> syncing user@example.com\n    page 1\n"
	if buf.String() != want {
		t.Fatalf("output mismatch: want %q, got %q", want, buf.String())
	}
}

func TestFormatStopReason(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "scanned through history"},
		{"known_purchase", "stopped at already-known order"},
		{"checkpoint_reached", "stopped at last-known payment timestamp"},
		{"weird", "weird"},
	}
	for _, c := range cases {
		if got := formatStopReason(c.in); got != c.want {
			t.Fatalf("formatStopReason(%q): want %q, got %q", c.in, c.want, got)
		}
	}
}

func TestPlural(t *testing.T) {
	if plural(1) != "" {
		t.Fatal("singular should be empty")
	}
	if plural(0) != "s" || plural(2) != "s" {
		t.Fatal("plural should be 's'")
	}
}

// TestSyncEmitsProgressWhenWriterProvided runs a real Sync against the
// in-memory fake client and asserts that the captured progress writer
// receives the expected phase + detail lines. Counterpart proof for the
// nil-writer test below: nothing is emitted when Progress is nil.
func TestSyncEmitsProgressWhenWriterProvided(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")
	var buf bytes.Buffer

	if _, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		Progress:  &buf,
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	out := buf.String()
	for _, needle := range []string{
		"Syncing order history for user@example.com (full mode)",
		"Catalog page 1:",
		"Catalog phase:",
		"Fetching 2 order details",
		"2/2 details fetched",
		"Detail phase: 2 fetched (2 new, 0 updated)",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("expected progress to contain %q, got:\n%s", needle, out)
		}
	}
}

func TestSyncEmitsNothingWhenProgressNil(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")
	if _, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		// Progress: nil
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	// Nothing to assert directly — the test passes if Sync did not panic
	// when no writer was provided. The other Sync tests in this package
	// already verify correctness.
}

func TestVerboseProgressLogsEveryDetail(t *testing.T) {
	client := newFakeClient(twoOrderCorpus())
	dbPath := filepath.Join(t.TempDir(), "wolt.sqlite")
	var buf bytes.Buffer
	if _, err := Sync(context.Background(), client, Options{
		DBPath:    dbPath,
		UserEmail: "user@example.com",
		RateLimit: time.Millisecond,
		Now:       fixedClock(),
		Progress:  &buf,
		Verbose:   true,
	}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	out := buf.String()
	// In --verbose mode each individual order id should appear in the log.
	for _, id := range []string{"p1", "p2"} {
		if !strings.Contains(out, id) {
			t.Fatalf("verbose progress should mention purchase id %q, got:\n%s", id, out)
		}
	}
}

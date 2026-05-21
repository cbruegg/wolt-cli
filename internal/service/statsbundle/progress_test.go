package statsbundle

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

func TestRenderBarDeterminate(t *testing.T) {
	cases := []struct {
		current, total int64
		want           string
	}{
		{0, 100, "[........................]   0%"},
		{50, 100, "[############............]  50%"},
		{100, 100, "[########################] 100%"},
		// Over-shoots clamp to 100.
		{150, 100, "[########################] 100%"},
	}
	for _, c := range cases {
		got := renderBar(c.current, c.total, 24)
		if got != c.want {
			t.Fatalf("renderBar(%d,%d): want %q, got %q", c.current, c.total, c.want, got)
		}
	}
}

func TestRenderBarIndeterminate(t *testing.T) {
	got := renderBar(123, -1, 8)
	if got != "[........] --" {
		t.Fatalf("indeterminate bar wrong: %q", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{-1, "unknown size"},
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{int64(2.5 * 1024 * 1024), "2.5 MB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Fatalf("humanBytes(%d): want %q, got %q", c.in, c.want, got)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1 * time.Second, "1s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "1m30s"},
		{125*time.Second + 500*time.Millisecond, "2m05s"},
	}
	for _, c := range cases {
		if got := humanDuration(c.in); got != c.want {
			t.Fatalf("humanDuration(%v): want %q, got %q", c.in, c.want, got)
		}
	}
}

func TestAssetNameFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://example.test/releases/download/v0.2.1/wolt-stats-bundle-v0.2.1.tar.gz", "wolt-stats-bundle-v0.2.1.tar.gz"},
		{"", "bundle"},
		{"not a url", "not a url"}, // url.Parse accepts this; Base returns the input
		{"https://example.test/", "bundle"},
	}
	for _, c := range cases {
		if got := assetNameFromURL(c.in); got != c.want {
			t.Fatalf("assetNameFromURL(%q): want %q, got %q", c.in, c.want, got)
		}
	}
}

func TestWritePhaseAndDetailNoOpOnNil(t *testing.T) {
	// Should not panic with nil writer.
	writePhase(nil, "should not panic")
	writeDetail(nil, "should not panic")
}

func TestWritePhaseAndDetailFormat(t *testing.T) {
	var buf bytes.Buffer
	writePhase(&buf, "step %s", "one")
	writeDetail(&buf, "detail %d", 7)
	want := "==> step one\n    detail 7\n"
	if buf.String() != want {
		t.Fatalf("output mismatch: want %q, got %q", want, buf.String())
	}
}

func TestProgressReaderEmitsBarAndFinish(t *testing.T) {
	body := strings.NewReader(strings.Repeat("x", 1024))
	var buf bytes.Buffer
	pr := &progressReader{
		Reader:   body,
		total:    int64(body.Len()),
		out:      &buf,
		throttle: 0, // emit on every read
	}
	if _, err := io.Copy(io.Discard, pr); err != nil {
		t.Fatalf("copy: %v", err)
	}
	pr.finish()
	out := buf.String()
	if !strings.Contains(out, "\r") {
		t.Fatal("expected at least one carriage return in progress output")
	}
	if !strings.Contains(out, "100%") {
		t.Fatalf("expected 100%% in final output: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatal("finish() should leave the cursor on a fresh line")
	}
}

func TestProgressReaderFinishWithoutReadIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	pr := &progressReader{Reader: strings.NewReader(""), total: 0, out: &buf}
	pr.finish()
	if buf.Len() != 0 {
		t.Fatalf("expected empty output when nothing was read, got %q", buf.String())
	}
}

func TestProgressReaderNilSinkIsNoOp(t *testing.T) {
	pr := &progressReader{Reader: strings.NewReader("hi"), total: 2, out: nil}
	if _, err := io.Copy(io.Discard, pr); err != nil {
		t.Fatalf("copy with nil sink: %v", err)
	}
	pr.finish() // should not panic
}

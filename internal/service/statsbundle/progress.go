package statsbundle

import (
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

// writePhase emits a "==> ..." line. It's a no-op when w is nil.
// Callers use it for top-level status transitions inside a phase.
func writePhase(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "==> "+format+"\n", args...)
}

// writeDetail emits a "    ..." indented continuation line under the
// current phase. No-op when w is nil.
func writeDetail(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "    "+format+"\n", args...)
}

// progressReader wraps an io.Reader and emits a redrawable progress bar
// to a sink Writer as bytes flow through. The bar uses carriage returns
// so it overwrites itself in place on a TTY; on a non-TTY pipe the lines
// stack up but stay legible. Set throttle to limit redraw frequency.
type progressReader struct {
	io.Reader
	total    int64
	read     int64
	out      io.Writer
	throttle time.Duration

	mu       sync.Mutex
	lastDraw time.Time
	drawn    bool
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.Reader.Read(b)
	if n > 0 {
		p.mu.Lock()
		p.read += int64(n)
		now := time.Now()
		if p.out != nil && (p.lastDraw.IsZero() || now.Sub(p.lastDraw) >= p.throttle) {
			p.drawLocked(now)
		}
		p.mu.Unlock()
	}
	return n, err
}

// finish forces a final 100% draw + newline so subsequent log lines start
// on a clean line. Safe to call multiple times.
func (p *progressReader) finish() {
	if p == nil || p.out == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.drawn {
		return
	}
	p.drawLocked(time.Now())
	_, _ = fmt.Fprintln(p.out)
}

func (p *progressReader) drawLocked(now time.Time) {
	p.lastDraw = now
	p.drawn = true
	bar := renderBar(p.read, p.total, 24)
	_, _ = fmt.Fprintf(p.out, "\r    %s %s / %s", bar, humanBytes(p.read), humanBytes(p.total))
}

// renderBar produces a fixed-width ascii bar like "[████░░░░] 40%".
// total <= 0 renders an indeterminate "[ working … ]" with no percentage.
func renderBar(current, total int64, width int) string {
	if width < 4 {
		width = 4
	}
	if total <= 0 {
		// Indeterminate: print a static placeholder so the user still sees
		// activity through the byte-count next to it.
		return "[" + strings.Repeat(".", width) + "] --"
	}
	if current > total {
		current = total
	}
	filled := int(float64(current) * float64(width) / float64(total))
	if filled > width {
		filled = width
	}
	pct := int(float64(current) * 100 / float64(total))
	return "[" + strings.Repeat("#", filled) + strings.Repeat(".", width-filled) + "] " + fmt.Sprintf("%3d%%", pct)
}

// humanBytes formats n into a short human-readable string. n < 0 is
// rendered as "unknown" so callers don't have to special-case missing
// Content-Length.
func humanBytes(n int64) string {
	if n < 0 {
		return "unknown size"
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	suffix := []string{"KB", "MB", "GB", "TB"}[exp]
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), suffix)
}

// humanDuration formats d as "1m23s" / "45s" / "120ms" — short enough to
// fit on a status line without taking attention from the message.
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	mins := int(d.Minutes())
	secs := int(d.Seconds()) - mins*60
	return fmt.Sprintf("%dm%02ds", mins, secs)
}

// assetNameFromURL returns the last path segment of a release asset URL,
// for use in user-facing log lines. Falls back to "bundle" on parse failure.
func assetNameFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Path == "" {
		return "bundle"
	}
	name := path.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		return "bundle"
	}
	return name
}

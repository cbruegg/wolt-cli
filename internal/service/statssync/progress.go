package statssync

import (
	"fmt"
	"io"
)

// writePhase emits a "==> ..." line. No-op when w is nil.
func writePhase(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "==> "+format+"\n", args...)
}

// writeDetail emits a "    ..." indented line. No-op when w is nil.
func writeDetail(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	_, _ = fmt.Fprintf(w, "    "+format+"\n", args...)
}

// formatStopReason humanises the catalog phase exit reason for the log line.
func formatStopReason(reason string) string {
	switch reason {
	case "":
		return "scanned through history"
	case "known_purchase":
		return "stopped at already-known order"
	case "checkpoint_reached":
		return "stopped at last-known payment timestamp"
	default:
		return reason
	}
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

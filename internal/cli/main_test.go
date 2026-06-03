package cli

import (
	"os"
	"testing"
)

// TestMain isolates the cli package's unit tests from the developer's local
// Chrome. The opportunistic re-sync in invokeWithAuthAutoRefresh would
// otherwise pull live wolt.com cookies into in-process auth contexts, which
// makes assertions about token-rotation paths non-deterministic.
func TestMain(m *testing.M) {
	if err := os.Setenv(envDisableChromeSync, "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

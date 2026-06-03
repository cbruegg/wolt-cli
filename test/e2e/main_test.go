package e2e_test

import (
	"os"
	"testing"
)

// TestMain isolates the e2e tests from the developer's local Chrome. The
// CLI's opportunistic re-sync in invokeWithAuthAutoRefresh would otherwise
// inject live wolt.com cookies into the mocked auth flows here.
func TestMain(m *testing.M) {
	if err := os.Setenv("WOLT_DISABLE_CHROME_SYNC", "1"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

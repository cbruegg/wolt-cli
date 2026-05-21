package mcpserver

import (
	"errors"
	"fmt"

	woltgateway "github.com/mekedron/wolt-cli/internal/gateway/wolt"
)

// toolErr returns an error suitable for handing back from a tool handler.
// The SDK wraps it into a CallToolResult with IsError=true and the message in
// a TextContent block — and crucially, skips output-schema validation on
// errors, which means zero-valued Out structs are safe to return.
//
// For known upstream errors (401/403/404/429) the message is rewritten to a
// short, LLM-friendly directive.
func toolErr(err error) error {
	if err == nil {
		return nil
	}
	var upstream *woltgateway.UpstreamRequestError
	if errors.As(err, &upstream) && upstream.StatusCode > 0 {
		switch upstream.StatusCode {
		case 401, 403:
			return errors.New("wolt session expired or missing — run 'wolt login' in a terminal to refresh, then retry")
		case 404:
			return fmt.Errorf("wolt API returned 404 (not found): %s", upstream.Error())
		case 429:
			return errors.New("wolt is rate-limiting requests; try again in a few seconds")
		default:
			return fmt.Errorf("wolt API returned status %d: %s", upstream.StatusCode, upstream.Error())
		}
	}
	return err
}

func toolErrf(format string, args ...any) error {
	return toolErr(fmt.Errorf(format, args...))
}

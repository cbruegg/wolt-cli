package cli

import (
	"regexp"
	"strings"
)

var objectIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func looksLikeObjectID(value string) bool {
	return objectIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

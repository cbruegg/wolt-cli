package domain

import (
	"regexp"
	"strings"
)

var objectIDPattern = regexp.MustCompile(`^[a-f0-9]{24}$`)

func LooksLikeObjectID(value string) bool {
	return objectIDPattern.MatchString(strings.ToLower(strings.TrimSpace(value)))
}

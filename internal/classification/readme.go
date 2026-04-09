package classification

import (
	"crypto/sha256"
	"fmt"
)

// HashReadme returns the hex-encoded SHA256 hash of the README content.
// Returns an empty string for empty content.
func HashReadme(content string) string {
	if content == "" {
		return ""
	}
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h)
}

// TruncateReadme truncates content to at most maxChars characters (runes).
// If the content is shorter than maxChars, it is returned unchanged.
func TruncateReadme(content string, maxChars int) string {
	if maxChars <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) <= maxChars {
		return content
	}
	return string(runes[:maxChars])
}

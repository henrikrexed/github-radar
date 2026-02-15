package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// envVarPattern matches ${VAR} and ${VAR:-default} patterns.
// Group 1: variable name
// Group 2: optional default value (after :-)
var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

// escapePattern matches $${...} escape sequences.
var escapePattern = regexp.MustCompile(`\$\$\{`)

// ExpandEnvVars expands ${VAR} and ${VAR:-default} patterns in content.
// Returns error if any required variables (without defaults) are missing.
func ExpandEnvVars(content []byte) ([]byte, error) {
	str := string(content)

	// First, temporarily replace escape sequences
	const escapePlaceholder = "\x00ESCAPED_DOLLAR\x00"
	str = escapePattern.ReplaceAllString(str, escapePlaceholder)

	// Track missing variables
	var missing []string

	// Expand all env var references
	result := envVarPattern.ReplaceAllStringFunc(str, func(match string) string {
		groups := envVarPattern.FindStringSubmatch(match)
		if len(groups) < 2 {
			return match
		}

		varName := groups[1]
		// Check if the match contains :- to determine if there's a default
		// (even if the default value is empty)
		hasDefault := strings.Contains(match, ":-")
		defaultValue := ""
		if hasDefault && len(groups) > 2 {
			defaultValue = groups[2]
		}

		value := os.Getenv(varName)
		if value == "" {
			if hasDefault {
				return defaultValue
			}
			// Check if the variable is set but empty vs not set at all
			if _, exists := os.LookupEnv(varName); !exists {
				missing = append(missing, varName)
			}
			return ""
		}
		return value
	})

	// Restore escape sequences as literal ${
	result = strings.ReplaceAll(result, escapePlaceholder, "${")

	// Return error if any required variables are missing
	if len(missing) > 0 {
		return nil, &ExpansionError{
			MissingVars: missing,
		}
	}

	return []byte(result), nil
}

// ExpansionError indicates missing environment variables.
type ExpansionError struct {
	MissingVars []string
}

// Error implements the error interface.
func (e *ExpansionError) Error() string {
	return fmt.Sprintf("config expansion failed: missing environment variables: %s",
		strings.Join(e.MissingVars, ", "))
}

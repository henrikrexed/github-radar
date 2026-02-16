package main

import (
	"testing"
)

func TestVersion(t *testing.T) {
	// Verify Version variable is set
	if Version == "" {
		t.Error("Version should not be empty")
	}
	// Default version is "dev"
	if Version != "dev" {
		t.Logf("Version is set to: %s", Version)
	}
}

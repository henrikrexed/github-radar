package classification

import (
	"strings"
	"testing"
)

func TestHashReadme(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantLen int // SHA256 hex is 64 chars
	}{
		{"empty", "", 0},
		{"non-empty", "hello world", 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashReadme(tt.content)
			if len(got) != tt.wantLen {
				t.Errorf("HashReadme(%q) length = %d, want %d", tt.content, len(got), tt.wantLen)
			}
		})
	}
}

func TestHashReadme_Deterministic(t *testing.T) {
	content := "# Test README\n\nSome content here."
	h1 := HashReadme(content)
	h2 := HashReadme(content)
	if h1 != h2 {
		t.Errorf("HashReadme not deterministic: %s != %s", h1, h2)
	}
}

func TestHashReadme_DifferentContent(t *testing.T) {
	h1 := HashReadme("content A")
	h2 := HashReadme("content B")
	if h1 == h2 {
		t.Error("different content produced same hash")
	}
}

func TestTruncateReadme(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxChars int
		want     string
	}{
		{"shorter than max", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncated", "hello world", 5, "hello"},
		{"zero max", "hello", 0, "hello"},
		{"negative max", "hello", -1, "hello"},
		{"empty content", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateReadme(tt.content, tt.maxChars)
			if got != tt.want {
				t.Errorf("TruncateReadme(%q, %d) = %q, want %q", tt.content, tt.maxChars, got, tt.want)
			}
		})
	}
}

func TestTruncateReadme_MultiByte(t *testing.T) {
	content := "héllo 世界"
	got := TruncateReadme(content, 7)
	want := "héllo 世"
	if got != want {
		t.Errorf("TruncateReadme(%q, 7) = %q, want %q", content, got, want)
	}
	// Ensure result is valid UTF-8
	for i, r := range got {
		if r == '\uFFFD' {
			t.Errorf("invalid UTF-8 at byte offset %d", i)
		}
	}
}

func TestTruncateReadme_LargeContent(t *testing.T) {
	content := strings.Repeat("a", 5000)
	got := TruncateReadme(content, 2000)
	if len(got) != 2000 {
		t.Errorf("truncated length = %d, want 2000", len(got))
	}
}

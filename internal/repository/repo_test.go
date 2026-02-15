package repository

import (
	"strings"
	"testing"
)

func TestParse_OwnerRepo(t *testing.T) {
	repo, err := Parse("kubernetes/kubernetes")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "kubernetes" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "kubernetes")
	}
	if repo.Name != "kubernetes" {
		t.Errorf("Name = %q, want %q", repo.Name, "kubernetes")
	}
}

func TestParse_HTTPSUrl(t *testing.T) {
	repo, err := Parse("https://github.com/prometheus/prometheus")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "prometheus" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "prometheus")
	}
	if repo.Name != "prometheus" {
		t.Errorf("Name = %q, want %q", repo.Name, "prometheus")
	}
}

func TestParse_HTTPUrl(t *testing.T) {
	repo, err := Parse("http://github.com/grafana/grafana")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "grafana" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "grafana")
	}
	if repo.Name != "grafana" {
		t.Errorf("Name = %q, want %q", repo.Name, "grafana")
	}
}

func TestParse_NoScheme(t *testing.T) {
	repo, err := Parse("github.com/opentelemetry/opentelemetry-go")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "opentelemetry" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "opentelemetry")
	}
	if repo.Name != "opentelemetry-go" {
		t.Errorf("Name = %q, want %q", repo.Name, "opentelemetry-go")
	}
}

func TestParse_WithGitSuffix(t *testing.T) {
	repo, err := Parse("https://github.com/helm/helm.git")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "helm" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "helm")
	}
	if repo.Name != "helm" {
		t.Errorf("Name = %q, want %q", repo.Name, "helm")
	}
}

func TestParse_WithTrailingSlash(t *testing.T) {
	repo, err := Parse("https://github.com/istio/istio/")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "istio" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "istio")
	}
	if repo.Name != "istio" {
		t.Errorf("Name = %q, want %q", repo.Name, "istio")
	}
}

func TestParse_WithWhitespace(t *testing.T) {
	repo, err := Parse("  kubernetes/kubernetes  ")
	if err != nil {
		t.Fatalf("Parse() error: %v", err)
	}

	if repo.Owner != "kubernetes" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "kubernetes")
	}
}

func TestParse_Invalid_Empty(t *testing.T) {
	_, err := Parse("")
	if err == nil {
		t.Fatal("Parse() should return error for empty input")
	}

	parseErr, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error should be *ParseError, got %T", err)
	}

	if !strings.Contains(parseErr.Error(), "invalid repository identifier") {
		t.Errorf("error message should mention 'invalid repository identifier': %v", err)
	}
}

func TestParse_Invalid_OnlyOwner(t *testing.T) {
	_, err := Parse("kubernetes")
	if err == nil {
		t.Fatal("Parse() should return error for owner-only input")
	}
}

func TestParse_Invalid_TooManyParts(t *testing.T) {
	_, err := Parse("owner/repo/extra")
	if err == nil {
		t.Fatal("Parse() should return error for too many parts")
	}
}

func TestParse_Invalid_EmptyOwner(t *testing.T) {
	_, err := Parse("/repo")
	if err == nil {
		t.Fatal("Parse() should return error for empty owner")
	}
}

func TestParse_Invalid_EmptyName(t *testing.T) {
	_, err := Parse("owner/")
	if err == nil {
		t.Fatal("Parse() should return error for empty name")
	}
}

func TestParse_Invalid_NotGitHub(t *testing.T) {
	_, err := Parse("https://gitlab.com/owner/repo")
	if err == nil {
		t.Fatal("Parse() should return error for non-GitHub URL")
	}
}

func TestParse_Invalid_Whitespace(t *testing.T) {
	_, err := Parse("owner name/repo")
	if err == nil {
		t.Fatal("Parse() should return error for whitespace in owner")
	}
}

func TestRepo_FullName(t *testing.T) {
	repo := Repo{Owner: "kubernetes", Name: "kubernetes"}
	if repo.FullName() != "kubernetes/kubernetes" {
		t.Errorf("FullName() = %q, want %q", repo.FullName(), "kubernetes/kubernetes")
	}
}

func TestRepo_String(t *testing.T) {
	repo := Repo{Owner: "prometheus", Name: "prometheus"}
	if repo.String() != "prometheus/prometheus" {
		t.Errorf("String() = %q, want %q", repo.String(), "prometheus/prometheus")
	}
}

func TestParseError_ContainsFormats(t *testing.T) {
	err := &ParseError{Input: "invalid"}
	errStr := err.Error()

	expectedFormats := []string{
		"owner/repo",
		"https://github.com/owner/repo",
		"github.com/owner/repo",
	}

	for _, format := range expectedFormats {
		if !strings.Contains(errStr, format) {
			t.Errorf("error should contain format %q: %v", format, errStr)
		}
	}
}

func TestMustParse_Success(t *testing.T) {
	repo := MustParse("kubernetes/kubernetes")
	if repo.Owner != "kubernetes" {
		t.Errorf("Owner = %q, want %q", repo.Owner, "kubernetes")
	}
}

func TestMustParse_Panic(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("MustParse() should panic on invalid input")
		}
	}()

	MustParse("invalid")
}

func TestParse_AllValidFormats(t *testing.T) {
	testCases := []struct {
		input string
		owner string
		name  string
	}{
		{"owner/repo", "owner", "repo"},
		{"https://github.com/owner/repo", "owner", "repo"},
		{"http://github.com/owner/repo", "owner", "repo"},
		{"github.com/owner/repo", "owner", "repo"},
		{"https://github.com/owner/repo.git", "owner", "repo"},
		{"https://github.com/owner/repo/", "owner", "repo"},
		{"github.com/owner/repo/", "owner", "repo"},
		{"github.com/owner/repo.git", "owner", "repo"},
		{"  owner/repo  ", "owner", "repo"},
		{"org-name/repo-name", "org-name", "repo-name"},
		{"my_org/my_repo", "my_org", "my_repo"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			repo, err := Parse(tc.input)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.input, err)
			}
			if repo.Owner != tc.owner {
				t.Errorf("Owner = %q, want %q", repo.Owner, tc.owner)
			}
			if repo.Name != tc.name {
				t.Errorf("Name = %q, want %q", repo.Name, tc.name)
			}
		})
	}
}

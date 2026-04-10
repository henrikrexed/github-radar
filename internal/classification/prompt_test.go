package classification

import (
	"strings"
	"testing"
)

func TestBuildSystemPrompt(t *testing.T) {
	categories := []string{"kubernetes", "observability", "ai-agents", "other"}
	tmpl := `You are a classifier. Categories: {{.Categories}}`

	result, err := BuildSystemPrompt(tmpl, categories)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "You are a classifier. Categories: kubernetes, observability, ai-agents, other"
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestBuildSystemPrompt_DefaultTemplate(t *testing.T) {
	categories := []string{"kubernetes", "observability", "other"}
	tmpl := `You are a GitHub repository classifier for CNCF and cloud-native projects.
Classify into exactly ONE category from: {{.Categories}}
If unclear, use "other".
Respond ONLY with JSON: {"category": "<name>", "confidence": <0.0-1.0>, "reasoning": "<one sentence>"}`

	result, err := BuildSystemPrompt(tmpl, categories)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "kubernetes, observability, other") {
		t.Errorf("categories not rendered: %s", result)
	}
}

func TestBuildSystemPrompt_InvalidTemplate(t *testing.T) {
	_, err := BuildSystemPrompt(`{{.Invalid`, nil)
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestBuildUserPrompt(t *testing.T) {
	tmpl := `Repository: {{.RepoName}}
Description: {{.Description}}
Language: {{.Language}}
Topics: {{.Topics}}
Stars: {{.Stars}} (trend: {{.StarTrend}})
README excerpt:
{{.Readme}}`

	data := PromptData{
		RepoName:    "kubernetes/kubernetes",
		Description: "Production-Grade Container Scheduling and Management",
		Language:    "Go",
		Topics:      "kubernetes, containers, orchestration",
		Stars:       110000,
		StarTrend:   "+250/week",
		Readme:      "# Kubernetes\nContainer orchestration platform.",
	}

	result, err := BuildUserPrompt(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "kubernetes/kubernetes") {
		t.Error("RepoName not rendered")
	}
	if !strings.Contains(result, "110000") {
		t.Error("Stars not rendered")
	}
	if !strings.Contains(result, "+250/week") {
		t.Error("StarTrend not rendered")
	}
	if !strings.Contains(result, "Container orchestration platform.") {
		t.Error("Readme not rendered")
	}
}

func TestBuildUserPrompt_EmptyFields(t *testing.T) {
	tmpl := `Repo: {{.RepoName}} | Desc: {{.Description}} | Lang: {{.Language}}`
	data := PromptData{RepoName: "foo/bar"}

	result, err := BuildUserPrompt(tmpl, data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "foo/bar") {
		t.Error("RepoName not rendered")
	}
	// Empty fields should render as empty strings.
	if !strings.Contains(result, "Desc:  |") {
		t.Errorf("empty Description not rendered as empty: %s", result)
	}
}

func TestBuildUserPrompt_InvalidTemplate(t *testing.T) {
	_, err := BuildUserPrompt(`{{.Bad`, PromptData{})
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

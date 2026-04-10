package classification

import (
	"bytes"
	"strings"
	"text/template"
)

// SystemPromptData holds the data available to the system prompt template.
type SystemPromptData struct {
	Categories string // Comma-separated list of allowed categories.
}

// PromptData holds the data available to the user prompt template.
type PromptData struct {
	RepoName    string
	Description string
	Language    string
	Topics      string
	Stars       int
	StarTrend   string
	Readme      string
}

// BuildSystemPrompt renders the system prompt Go template with the given categories.
// The template receives a SystemPromptData with Categories as a comma-separated string.
func BuildSystemPrompt(systemTemplate string, categories []string) (string, error) {
	tmpl, err := template.New("system").Parse(systemTemplate)
	if err != nil {
		return "", err
	}

	data := SystemPromptData{
		Categories: strings.Join(categories, ", "),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// BuildUserPrompt renders the user prompt Go template with the given PromptData.
func BuildUserPrompt(userTemplate string, data PromptData) (string, error) {
	tmpl, err := template.New("user").Parse(userTemplate)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

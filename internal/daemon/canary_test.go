package daemon

import (
	"testing"

	"github.com/hrexed/github-radar/internal/config"
	"github.com/hrexed/github-radar/internal/github"
)

func TestPartitionByCanary(t *testing.T) {
	repos := []github.Repo{
		{Owner: "kubernetes", Name: "kubernetes"},
		{Owner: "hashicorp", Name: "vault"},
		{Owner: "open-telemetry", Name: "opentelemetry-go"},
		{Owner: "prometheus", Name: "prometheus"},
		{Owner: "grafana", Name: "grafana"},
	}

	tests := []struct {
		name            string
		canary          []string
		wantCanaryNames []string
		wantLegacyNames []string
	}{
		{
			name:            "empty list yields all legacy",
			canary:          nil,
			wantCanaryNames: nil,
			wantLegacyNames: []string{
				"kubernetes/kubernetes", "hashicorp/vault", "open-telemetry/opentelemetry-go",
				"prometheus/prometheus", "grafana/grafana",
			},
		},
		{
			name: "exact match canary subset",
			canary: []string{
				"kubernetes/kubernetes",
				"hashicorp/vault",
			},
			wantCanaryNames: []string{"kubernetes/kubernetes", "hashicorp/vault"},
			wantLegacyNames: []string{
				"open-telemetry/opentelemetry-go", "prometheus/prometheus", "grafana/grafana",
			},
		},
		{
			name:            "case-insensitive match",
			canary:          []string{"Kubernetes/Kubernetes", "GRAFANA/grafana"},
			wantCanaryNames: []string{"kubernetes/kubernetes", "grafana/grafana"},
			wantLegacyNames: []string{
				"hashicorp/vault", "open-telemetry/opentelemetry-go", "prometheus/prometheus",
			},
		},
		{
			name:            "whitespace-tolerant",
			canary:          []string{"  prometheus/prometheus  ", "\thashicorp/vault\n"},
			wantCanaryNames: []string{"hashicorp/vault", "prometheus/prometheus"},
			wantLegacyNames: []string{
				"kubernetes/kubernetes", "open-telemetry/opentelemetry-go", "grafana/grafana",
			},
		},
		{
			name:            "empty entries are ignored",
			canary:          []string{"", "  ", "kubernetes/kubernetes"},
			wantCanaryNames: []string{"kubernetes/kubernetes"},
			wantLegacyNames: []string{
				"hashicorp/vault", "open-telemetry/opentelemetry-go", "prometheus/prometheus", "grafana/grafana",
			},
		},
		{
			name:            "unknown canary entries are dropped (no panic)",
			canary:          []string{"made-up/repo", "kubernetes/kubernetes"},
			wantCanaryNames: []string{"kubernetes/kubernetes"},
			wantLegacyNames: []string{
				"hashicorp/vault", "open-telemetry/opentelemetry-go", "prometheus/prometheus", "grafana/grafana",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCanary, gotLegacy := partitionByCanary(repos, tc.canary)
			assertNamesEqual(t, "canary", gotCanary, tc.wantCanaryNames)
			assertNamesEqual(t, "legacy", gotLegacy, tc.wantLegacyNames)
		})
	}
}

func TestPartitionByCanary_PreservesOrder(t *testing.T) {
	repos := []github.Repo{
		{Owner: "a", Name: "1"},
		{Owner: "b", Name: "2"},
		{Owner: "c", Name: "3"},
		{Owner: "d", Name: "4"},
	}
	gotCanary, gotLegacy := partitionByCanary(repos, []string{"a/1", "c/3"})
	assertNamesEqual(t, "canary", gotCanary, []string{"a/1", "c/3"})
	assertNamesEqual(t, "legacy", gotLegacy, []string{"b/2", "d/4"})
}

func TestScanPathLabel(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.GithubConfig
		want string
	}{
		{
			name: "legacy when bulk fetch off",
			cfg:  config.GithubConfig{BulkFetchEnabled: false},
			want: "legacy",
		},
		{
			name: "legacy when bulk fetch off ignores canary list",
			cfg: config.GithubConfig{
				BulkFetchEnabled:         false,
				BulkFetchCanaryFullNames: []string{"a/b"},
			},
			want: "legacy",
		},
		{
			name: "bulk when bulk fetch on and canary empty",
			cfg:  config.GithubConfig{BulkFetchEnabled: true},
			want: "bulk",
		},
		{
			name: "canary when bulk fetch on and canary non-empty",
			cfg: config.GithubConfig{
				BulkFetchEnabled:         true,
				BulkFetchCanaryFullNames: []string{"a/b"},
			},
			want: "canary",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scanPathLabel(tc.cfg); got != tc.want {
				t.Errorf("scanPathLabel: got %q, want %q", got, tc.want)
			}
		})
	}
}

func assertNamesEqual(t *testing.T, label string, got []github.Repo, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: got %d repos, want %d (got=%v want=%v)", label, len(got), len(want), repoNames(got), want)
		return
	}
	gotNames := repoNames(got)
	wantSet := make(map[string]struct{}, len(want))
	for _, n := range want {
		wantSet[n] = struct{}{}
	}
	for _, n := range gotNames {
		if _, ok := wantSet[n]; !ok {
			t.Errorf("%s: unexpected repo %q (got=%v want=%v)", label, n, gotNames, want)
		}
	}
}

func repoNames(rs []github.Repo) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Owner+"/"+r.Name)
	}
	return out
}

package ghstub

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
)

// graphqlEnvelope is the wire shape the github client sends.
type graphqlEnvelope struct {
	Query string `json:"query"`
}

// AliasRef is one alias->{owner,name} mapping parsed from a GraphQL
// query body so the stub can mint a deterministic response per alias.
type AliasRef struct {
	Alias string
	Owner string
	Name  string
}

// aliasLine matches `r0: repository(owner: "foo", name: "bar") { ...RepoFields }`.
var aliasLine = regexp.MustCompile(`(\w+):\s*repository\(owner:\s*"([^"]+)",\s*name:\s*"([^"]+)"\)`)

// parseAliases decodes the request body and extracts the alias map.
// It is tolerant: if the body cannot be parsed it returns an empty
// slice so the handler can still write a valid (empty) response.
func parseAliases(body []byte) []AliasRef {
	var env graphqlEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	matches := aliasLine.FindAllStringSubmatch(env.Query, -1)
	out := make([]AliasRef, 0, len(matches))
	for _, m := range matches {
		out = append(out, AliasRef{Alias: m[1], Owner: m[2], Name: m[3]})
	}
	return out
}

// readBody reads the entire request body, returning an error if the
// reader fails. Bounded by the http.Server max body size.
func readBody(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

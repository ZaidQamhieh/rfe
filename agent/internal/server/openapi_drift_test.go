package server

import (
	"bufio"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

var paramName = regexp.MustCompile(`\{[^}]*\}`)

// shape drops path-parameter names: the spec shares one /share/{token} template
// between the public download and the revoke-by-hash call, so names may differ
// while the route shape is the same.
func shape(op string) string { return paramName.ReplaceAllString(op, "{}") }

// specOperations reads the "METHOD /v1/path" operations out of
// protocol/openapi.yaml. The spec's server URL ends in /v1, so every path is
// prefixed with it to match what chi reports.
func specOperations(t *testing.T) map[string]bool {
	t.Helper()
	f, err := os.Open("../../../protocol/openapi.yaml")
	if err != nil {
		t.Fatalf("open spec: %v", err)
	}
	defer f.Close()
	pathLine := regexp.MustCompile(`^  (/[^\s:]*):\s*$`)
	methodLine := regexp.MustCompile(`^    (get|put|post|patch|delete):\s*$`)
	ops := map[string]bool{}
	inPaths := false
	current := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !inPaths {
			inPaths = line == "paths:"
			continue
		}
		if line != "" && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "#") {
			break // next top-level key (components:)
		}
		if m := pathLine.FindStringSubmatch(line); m != nil {
			current = m[1]
			continue
		}
		if m := methodLine.FindStringSubmatch(line); m != nil && current != "" {
			ops[shape(strings.ToUpper(m[1])+" /v1"+current)] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read spec: %v", err)
	}
	if len(ops) == 0 {
		t.Fatal("found no operations in the spec; the parser needs updating")
	}
	return ops
}

// Routes the router serves on purpose without a spec entry. Keep it empty if
// you can; every line is a hole in the contract.
var routesNotInSpec = map[string]string{}

// Spec operations the router intentionally does not serve (none today).
var specNotInRouter = map[string]string{}

// TestOpenAPIMatchesRegisteredRoutes fails when a route is added to or removed
// from the router without the same change in protocol/openapi.yaml. The spec is
// the contract the app builds against, so the two must not drift.
func TestOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	h, _ := newTierRouter(t)
	routes := map[string]bool{}
	err := chi.Walk(h.(chi.Routes), func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if pattern == "/*" {
			return nil // web companion catch-all, every method
		}
		routes[shape(method+" "+pattern)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	spec := specOperations(t)

	var missingFromSpec, missingFromRouter []string
	for r := range routes {
		if !spec[r] {
			if _, ok := routesNotInSpec[r]; !ok {
				missingFromSpec = append(missingFromSpec, r)
			}
		}
	}
	for op := range spec {
		if !routes[op] {
			if _, ok := specNotInRouter[op]; !ok {
				missingFromRouter = append(missingFromRouter, op)
			}
		}
	}
	sort.Strings(missingFromSpec)
	sort.Strings(missingFromRouter)
	if len(missingFromSpec) > 0 {
		t.Errorf("routes registered but missing from protocol/openapi.yaml (add them in the same commit): %v", missingFromSpec)
	}
	if len(missingFromRouter) > 0 {
		t.Errorf("operations in protocol/openapi.yaml with no registered route: %v", missingFromRouter)
	}
}

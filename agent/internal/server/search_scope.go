package server

import (
	"path/filepath"
	"strings"
)

type rootScope struct {
	root             string
	descendantPrefix string
}

func prepareRootScopes(roots []string) []rootScope {
	scopes := make([]rootScope, 0, len(roots))
	separator := string(filepath.Separator)
	for _, root := range roots {
		scopes = append(scopes, rootScope{
			root:             root,
			descendantPrefix: strings.TrimSuffix(root, separator) + separator,
		})
	}
	return scopes
}

// underAnyRootScopes reports whether path is a root itself or inside one of
// the already-prepared roots. It performs no per-root string construction.
func underAnyRootScopes(path string, scopes []rootScope) bool {
	for _, scope := range scopes {
		if path == scope.root || strings.HasPrefix(path, scope.descendantPrefix) {
			return true
		}
	}
	return false
}

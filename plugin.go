// Package plugindemo is an example of Traefik middleware plugin run by Yaegi v2.
// It removes the request headers whose name starts with one of the configured prefixes.
package plugindemo

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"strings"
)

// Config is the configuration of the plugin.
type Config struct {
	// Prefixes of the request headers to remove, for example "X-Internal-".
	Prefixes []string `json:"prefixes,omitempty"`
}

// CreateConfig creates the default configuration of the plugin.
func CreateConfig() *Config {
	return &Config{}
}

// HeaderFilter is the middleware.
type HeaderFilter struct {
	next     http.Handler
	name     string
	prefixes []string
}

// New creates a new instance of the middleware.
func New(_ context.Context, next http.Handler, config *Config, name string) (http.Handler, error) {
	if len(config.Prefixes) == 0 {
		return nil, errors.New("prefixes cannot be empty")
	}

	prefixes := make([]string, 0, len(config.Prefixes))
	for _, p := range config.Prefixes {
		prefixes = append(prefixes, http.CanonicalHeaderKey(p))
	}

	return &HeaderFilter{
		next:     next,
		name:     name,
		prefixes: prefixes,
	}, nil
}

func (h *HeaderFilter) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// maps.Keys returns an iterator (Go 1.23), and slices.ContainsFunc is a generic function.
	// Yaegi v1 cannot run this code, Yaegi v2 runs it as compiled Go does.
	for name := range maps.Keys(req.Header) {
		if slices.ContainsFunc(h.prefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			req.Header.Del(name)
		}
	}

	h.next.ServeHTTP(rw, req)
}

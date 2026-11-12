package plugindemo_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	plugindemo "github.com/juliens/plugin-yaegi-v2-demo"
)

func TestHeaderFilter(t *testing.T) {
	cfg := plugindemo.CreateConfig()
	cfg.Prefixes = []string{"x-internal-", "X-Debug"}

	var got http.Header
	next := http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		got = req.Header
	})

	handler, err := plugindemo.New(context.Background(), next, cfg, "demo")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Internal-User", "admin")
	req.Header.Set("X-Debug", "1")
	req.Header.Set("X-Request-Id", "42")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	for _, name := range []string{"X-Internal-User", "X-Debug"} {
		if v := got.Get(name); v != "" {
			t.Errorf("header %s: got %q, want it removed", name, v)
		}
	}

	if v := got.Get("X-Request-Id"); v != "42" {
		t.Errorf("header X-Request-Id: got %q, want %q", v, "42")
	}
}

func TestNew_noPrefixes(t *testing.T) {
	_, err := plugindemo.New(context.Background(), http.NotFoundHandler(), plugindemo.CreateConfig(), "demo")
	if err == nil {
		t.Fatal("expected an error")
	}
}

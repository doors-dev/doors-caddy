package upstream

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/common"
)

func TestGetUpstreams_WithUpstreams(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	expected := []*reverseproxy.Upstream{
		{Dial: "host1:8080"},
		{Dial: "host2:8080"},
	}
	common.SetUpstreams(req, expected)

	m := Module{}
	got, err := m.GetUpstreams(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("expected %d upstreams, got %d", len(expected), len(got))
	}
	if got[0].Dial != "host1:8080" {
		t.Errorf("expected host1:8080, got %s", got[0].Dial)
	}
}

func TestGetUpstreams_NoUpstreams(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	m := Module{}
	_, err := m.GetUpstreams(req)
	if err == nil {
		t.Fatal("expected error when no upstreams in context")
	}
	if err != errorNoUpstreams {
		t.Errorf("expected errorNoUpstreams, got: %v", err)
	}
}

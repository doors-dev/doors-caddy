package common

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

func TestContextRoundTrip(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	upstreams := []*reverseproxy.Upstream{
		{Dial: "host1:8080"},
		{Dial: "host2:8080"},
	}
	SetUpstreams(req, upstreams)

	got := GetUpstreams(req)
	if len(got) != len(upstreams) {
		t.Fatalf("expected %d upstreams, got %d", len(upstreams), len(got))
	}
	if got[0].Dial != "host1:8080" {
		t.Errorf("expected host1:8080, got %s", got[0].Dial)
	}
	if got[1].Dial != "host2:8080" {
		t.Errorf("expected host2:8080, got %s", got[1].Dial)
	}
}

func TestGetUpstreams_NoUpstreams(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	got := GetUpstreams(req)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestGetUpstreams_WrongType(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	caddyhttp.SetVar(req.Context(), upstreamKey, "not upstreams")
	got := GetUpstreams(req)
	if got != nil {
		t.Errorf("expected nil for wrong type, got %v", got)
	}
}

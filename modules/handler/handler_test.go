package handler

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/common"
)

type nextHandler func(http.ResponseWriter, *http.Request) error

func (f nextHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	return f(w, r)
}

var testSecretHex = base64.StdEncoding.EncodeToString([]byte("16-byte-key-here"))

func mustParseAddr(s string) netip.Addr {
	a, err := netip.ParseAddr(s)
	if err != nil {
		panic(err)
	}
	return a
}

func provisionedModule(t *testing.T, opts ...func(*Module)) *Module {
	t.Helper()
	m := &Module{
		Secret: testSecretHex,
		Upstreams: []Upstream{
			{CIDR: "10.0.0.0/24", Host: "app.example.com", Port: 8080},
		},
	}
	for _, opt := range opts {
		opt(m)
	}
	if err := m.Provision(caddy.Context{}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	return m
}

func withUpstream(cidr, host string, port int) func(*Module) {
	return func(m *Module) {
		m.Upstreams = append(m.Upstreams, Upstream{CIDR: cidr, Host: host, Port: port})
	}
}

func withCookieName(name string) func(*Module) {
	return func(m *Module) {
		m.CookieName = name
	}
}

func testRequest(method, path, host string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if host != "" {
		r.Host = host
	}
	ctx := context.WithValue(r.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	r = r.WithContext(ctx)
	return r
}

func getUpstreamsVar(r *http.Request) []*reverseproxy.Upstream {
	return common.GetUpstreams(r)
}

func TestSystemRequest_ValidToken_MatchingCIDR(t *testing.T) {
	m := provisionedModule(t)
	ip := "10.0.0.5"
	token := m.cipher.Encode(mustParseAddr(ip))
	req := testRequest("GET", "/~/"+token+"/api/data", "example.com")

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler was not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(upstreams))
	}
	expectedDial := ip + ":8080"
	if upstreams[0].Dial != expectedDial {
		t.Errorf("expected direct-to-pod Dial %q, got %q", expectedDial, upstreams[0].Dial)
	}
}

func TestSystemRequest_InvalidToken(t *testing.T) {
	m := provisionedModule(t)
	req := testRequest("GET", "/~/bad-token/api", "example.com")

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("next handler should NOT be called for 410 Gone")
	}
	if rr.Code != http.StatusGone {
		t.Errorf("expected 410 Gone, got %d", rr.Code)
	}
}

func TestSystemRequest_TokenNoCIDRMatch(t *testing.T) {
	m := provisionedModule(t)
	ip := "192.168.1.5"
	token := m.cipher.Encode(mustParseAddr(ip))
	req := testRequest("GET", "/~/"+token+"/api", "example.com")

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("next handler should NOT be called for CIDR mismatch")
	}
	if rr.Code != http.StatusGone {
		t.Errorf("expected 410 Gone, got %d", rr.Code)
	}
}

func TestSystemRequest_PathWithRest(t *testing.T) {
	m := provisionedModule(t)
	ip := "10.0.0.99"
	token := m.cipher.Encode(mustParseAddr(ip))
	req := testRequest("GET", "/~/"+token+"/api/v2/resource?q=1", "example.com")

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler should be called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(upstreams))
	}
	expectedDial := ip + ":8080"
	if upstreams[0].Dial != expectedDial {
		t.Errorf("expected Dial %q, got %q", expectedDial, upstreams[0].Dial)
	}
}

func TestNormalRequest_SingleUpstream(t *testing.T) {
	m := provisionedModule(t)
	req := testRequest("GET", "/normal/path", "example.com")

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(upstreams))
	}
	if upstreams[0].Dial != "app.example.com:8080" {
		t.Errorf("expected host-based upstream, got %q", upstreams[0].Dial)
	}
}

func TestNormalRequest_SingleUpstream_IgnoresCookie(t *testing.T) {
	m := provisionedModule(t)
	ip := "10.0.0.5"
	token := m.cipher.Encode(mustParseAddr(ip))
	req := testRequest("GET", "/normal/path", "example.com")
	req.AddCookie(&http.Cookie{Name: "doors_upstream", Value: token})

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(upstreams))
	}
	if upstreams[0].Dial != "app.example.com:8080" {
		t.Errorf("single upstream should ignore cookie; expected app.example.com:8080, got %q", upstreams[0].Dial)
	}
}

func TestNormalRequest_MultiUpstream_ValidCookie(t *testing.T) {
	m := provisionedModule(t,
		withUpstream("10.0.1.0/24", "app2.example.com", 8081),
		withCookieName("doors_upstream"),
	)
	ip := "10.0.1.5"
	token := m.cipher.Encode(mustParseAddr(ip))
	req := testRequest("GET", "/normal/path", "example.com")
	req.AddCookie(&http.Cookie{Name: "doors_upstream", Value: token})

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 1 {
		t.Fatalf("expected 1 pinned upstream, got %d", len(upstreams))
	}
	if upstreams[0].Dial != "app2.example.com:8081" {
		t.Errorf("expected app2.example.com:8081, got %q", upstreams[0].Dial)
	}
}

func TestNormalRequest_MultiUpstream_NoCookie(t *testing.T) {
	m := provisionedModule(t,
		withUpstream("10.0.1.0/24", "app2.example.com", 8081),
		withCookieName("doors_upstream"),
	)
	req := testRequest("GET", "/normal/path", "example.com")

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 2 {
		t.Fatalf("expected all 2 upstreams, got %d", len(upstreams))
	}
}

func TestNormalRequest_MultiUpstream_InvalidCookie(t *testing.T) {
	m := provisionedModule(t,
		withUpstream("10.0.1.0/24", "app2.example.com", 8081),
		withCookieName("doors_upstream"),
	)
	req := testRequest("GET", "/normal/path", "example.com")
	req.AddCookie(&http.Cookie{Name: "doors_upstream", Value: "not-a-valid-token"})

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 2 {
		t.Fatalf("expected fallback to all 2 upstreams, got %d", len(upstreams))
	}
}

func TestNormalRequest_MultiUpstream_CIDRMismatch(t *testing.T) {
	m := provisionedModule(t,
		withUpstream("10.0.1.0/24", "app2.example.com", 8081),
		withCookieName("doors_upstream"),
	)
	ip := "192.168.1.5"
	token := m.cipher.Encode(mustParseAddr(ip))
	req := testRequest("GET", "/normal/path", "example.com")
	req.AddCookie(&http.Cookie{Name: "doors_upstream", Value: token})

	rr := httptest.NewRecorder()
	called := false
	err := m.ServeHTTP(rr, req, nextHandler(func(w http.ResponseWriter, r *http.Request) error {
		called = true
		return nil
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("next handler not called")
	}

	upstreams := getUpstreamsVar(req)
	if len(upstreams) != 2 {
		t.Fatalf("expected fallback to all 2 upstreams, got %d", len(upstreams))
	}
}

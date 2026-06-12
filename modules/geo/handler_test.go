package geo

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/gaissmai/bart"
	"go.uber.org/zap"
)

func fakeGeoServiceInstance(v4Entries map[string]string, v6Entries map[string]string) *geoService {
	s := &geoService{Logger: zap.NewNop()}
	if v4Entries != nil {
		t := new(bart.Table[string])
		for cidr, country := range v4Entries {
			prefix := netip.MustParsePrefix(cidr)
			t.Insert(prefix.Masked(), country)
		}
		s.v4.Store(t)
	}
	if v6Entries != nil {
		t := new(bart.Table[string])
		for cidr, country := range v6Entries {
			prefix := netip.MustParsePrefix(cidr)
			t.Insert(prefix.Masked(), country)
		}
		s.v6.Store(t)
	}
	return s
}

type geoHandlerNext struct {
	called bool
}

func (h *geoHandlerNext) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	h.called = true
	return nil
}

func TestGeoHandler_Redirect(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/some/path?q=1", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.TLS = &tls.ConnectionState{}
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if next.called {
		t.Error("next handler should NOT be called on redirect")
	}
	if rr.Code != http.StatusTemporaryRedirect {
		t.Errorf("expected 307, got %d", rr.Code)
	}
	location := rr.Header().Get("Location")
	expected := "https://us.example.com/some/path?q=1"
	if location != expected {
		t.Errorf("expected Location %q, got %q", expected, location)
	}
}

func TestGeoHandler_AlreadyOnDomain(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.Host = "us.example.com"
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.called {
		t.Error("next handler should be called when already on domain")
	}
	if rr.Code != 0 && rr.Code != 200 {
		t.Errorf("unexpected status code: %d", rr.Code)
	}
}

func TestGeoHandler_CountryNotConfigured(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "JP"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.called {
		t.Error("next handler should be called when no redirect configured")
	}
}

func TestGeoHandler_IPNotFound(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "5.6.7.8:12345"
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.called {
		t.Error("next handler should be called when IP not in DB")
	}
}

func TestGeoHandler_HTTPScheme(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	location := rr.Header().Get("Location")
	if location[:5] != "http:" {
		t.Errorf("expected http:// scheme, got %q", location)
	}
}

func TestGeoHandler_HTTPSScheme(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.TLS = &tls.ConnectionState{}
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	location := rr.Header().Get("Location")
	if location[:6] != "https:" {
		t.Errorf("expected https:// scheme, got %q", location)
	}
}

func TestGeoHandler_TrustedProxyIP(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"5.6.7.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.TLS = &tls.ConnectionState{}
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)
	caddyhttp.SetVar(req.Context(), caddyhttp.ClientIPVarKey, "5.6.7.8")

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "us.example.com") {
		t.Errorf("expected redirect to us.example.com, got %q", location)
	}
}

func TestGeoHandler_FallbackRemoteAddr(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.TLS = &tls.ConnectionState{}
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	location := rr.Header().Get("Location")
	if !strings.Contains(location, "us.example.com") {
		t.Errorf("expected redirect to us.example.com via RemoteAddr, got %q", location)
	}
}

func TestGeoHandler_InvalidRemoteAddr(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/path", nil)
	req.RemoteAddr = "invalid:addr"
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !next.called {
		t.Error("next handler should be called on invalid RemoteAddr")
	}
}

func TestGeoHandler_PreservesPath(t *testing.T) {
	m := Module{
		logger:  zap.NewNop(),
		service: fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil),
		lookup:  map[string]string{"US": "us.example.com"},
	}
	req := httptest.NewRequest("GET", "/deep/nested/path?param=value&other=123", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	req.TLS = &tls.ConnectionState{}
	ctx := context.WithValue(req.Context(), caddyhttp.VarsCtxKey, make(map[string]any))
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	next := &geoHandlerNext{}
	err := m.ServeHTTP(rr, req, next)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	location := rr.Header().Get("Location")
	expected := "https://us.example.com/deep/nested/path?param=value&other=123"
	if location != expected {
		t.Errorf("expected %q, got %q", expected, location)
	}
}

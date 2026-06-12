package geo

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestGeoUnmarshalCaddyfile_Full(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_geo {
	ipv4_url https://example.com/v4.tar.gz
	ipv6_url https://example.com/v6.tar.gz
	update_interval 12h
	download_timeout 60s
	example.com {
		US
		ca
	}
	other.example.com {
		gB
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.IPv4URL != "https://example.com/v4.tar.gz" {
		t.Errorf("expected custom IPv4URL, got %q", m.IPv4URL)
	}
	if m.IPv6URL != "https://example.com/v6.tar.gz" {
		t.Errorf("expected custom IPv6URL, got %q", m.IPv6URL)
	}
	if time.Duration(m.UpdateInterval) != 12*time.Hour {
		t.Errorf("expected 12h interval, got %v", time.Duration(m.UpdateInterval))
	}
	if time.Duration(m.DownloadTimeout) != 60*time.Second {
		t.Errorf("expected 60s timeout, got %v", time.Duration(m.DownloadTimeout))
	}
	if len(m.Redirects) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(m.Redirects))
	}
	if codes := m.Redirects["example.com"]; len(codes) != 2 || codes[0] != "US" || codes[1] != "CA" {
		t.Errorf("expected [US CA], got %v", codes)
	}
	if codes := m.Redirects["other.example.com"]; len(codes) != 1 || codes[0] != "GB" {
		t.Errorf("expected [GB], got %v", codes)
	}
}

func TestGeoUnmarshalCaddyfile_DomainBlock(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_geo {
	example.com {
		us gb de
		fr
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	codes := m.Redirects["example.com"]
	if len(codes) != 4 {
		t.Fatalf("expected 4 codes, got %d: %v", len(codes), codes)
	}
	expected := []string{"US", "GB", "DE", "FR"}
	for i, code := range codes {
		if code != expected[i] {
			t.Errorf("code[%d]: expected %q, got %q", i, expected[i], code)
		}
	}
}

func TestGeoUnmarshalCaddyfile_DuplicateDomain(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_geo {
	example.com {
		US
	}
}
doors_geo {
	example.com {
		GB
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "duplicate domain block") {
		t.Errorf("expected 'duplicate domain block', got: %v", err)
	}
}

func TestGeoUnmarshalCaddyfile_InvalidCountryCode(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_geo {
	example.com { usa }
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "invalid country code") {
		t.Errorf("expected 'invalid country code', got: %v", err)
	}
}

func TestGeoUnmarshalCaddyfile_EmptyDomainBlock(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_geo {
	example.com { }
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "must contain at least one country code") {
		t.Errorf("expected 'must contain at least one country code', got: %v", err)
	}
}

func TestGeoUnmarshalCaddyfile_InvalidDuration(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_geo {
	update_interval notaduration
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "invalid update_interval") {
		t.Errorf("expected 'invalid update_interval', got: %v", err)
	}
}

func TestGeoProvision_Defaults(t *testing.T) {
	srv := archiveServer(t)
	defer srv.Close()

	m := &Module{
		IPv4URL: srv.URL,
		IPv6URL: srv.URL,
		Redirects: map[string][]string{
			"us.example.com": {"US"},
		},
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	m.Cleanup()

	if m.lookup["US"] != "us.example.com" {
		t.Errorf("expected US -> us.example.com, got %q", m.lookup["US"])
	}
	if m.IPv4URL != srv.URL {
		t.Errorf("expected kept custom URL, got %q", m.IPv4URL)
	}
	updateInterval := time.Duration(m.UpdateInterval)
	defaultInterval := 24 * time.Hour
	if updateInterval != defaultInterval {
		t.Errorf("expected 24h interval, got %v", updateInterval)
	}
	if time.Duration(m.DownloadTimeout) != 30*time.Second {
		t.Errorf("expected 30s timeout, got %v", time.Duration(m.DownloadTimeout))
	}
}

func TestGeoProvision_ReverseLookup(t *testing.T) {
	srv := archiveServer(t)
	defer srv.Close()

	m := &Module{
		IPv4URL: srv.URL,
		IPv6URL: srv.URL,
		Redirects: map[string][]string{
			"us.example.com": {"US", "CA"},
			"eu.example.com": {"DE", "FR"},
		},
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	m.Cleanup()

	if m.lookup["US"] != "us.example.com" {
		t.Errorf("expected US -> us.example.com, got %q", m.lookup["US"])
	}
	if m.lookup["CA"] != "us.example.com" {
		t.Errorf("expected CA -> us.example.com, got %q", m.lookup["CA"])
	}
	if m.lookup["DE"] != "eu.example.com" {
		t.Errorf("expected DE -> eu.example.com, got %q", m.lookup["DE"])
	}
	if m.lookup["FR"] != "eu.example.com" {
		t.Errorf("expected FR -> eu.example.com, got %q", m.lookup["FR"])
	}
}

func TestGeoProvision_CustomValues(t *testing.T) {
	srv := archiveServer(t)
	defer srv.Close()

	m := &Module{
		IPv4URL:         srv.URL,
		IPv6URL:         srv.URL,
		UpdateInterval:  caddy.Duration(6 * time.Hour),
		DownloadTimeout: caddy.Duration(45 * time.Second),
		Redirects: map[string][]string{
			"example.com": {"US"},
		},
	}
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	m.Cleanup()

	if m.IPv4URL != srv.URL {
		t.Errorf("expected custom IPv4URL, got %q", m.IPv4URL)
	}
	if time.Duration(m.UpdateInterval) != 6*time.Hour {
		t.Errorf("expected 6h interval, got %v", time.Duration(m.UpdateInterval))
	}
	if time.Duration(m.DownloadTimeout) != 45*time.Second {
		t.Errorf("expected 45s timeout, got %v", time.Duration(m.DownloadTimeout))
	}
}

func archiveServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
}

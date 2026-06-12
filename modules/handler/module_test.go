package handler

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

var testSecret = base64.StdEncoding.EncodeToString([]byte("16-byte-key-here"))

func validModule() *Module {
	return &Module{
		Secret: testSecret,
		Upstreams: []Upstream{
			{CIDR: "10.0.0.0/24", Host: "app.example.com", Port: 8080},
		},
	}
}

func TestProvision_Valid(t *testing.T) {
	m := validModule()
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.cipher == nil {
		t.Error("cipher not set")
	}
	if len(m.upstreams) != 1 {
		t.Errorf("expected 1 compiled upstream, got %d", len(m.upstreams))
	}
	if m.upstreams[0].Dial != "app.example.com:8080" {
		t.Errorf("expected app.example.com:8080, got %s", m.upstreams[0].Dial)
	}
}

func TestProvision_MissingSecret(t *testing.T) {
	m := validModule()
	m.Secret = ""
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "secret is required") {
		t.Errorf("expected 'secret is required', got: %v", err)
	}
}

func TestProvision_NoUpstreams(t *testing.T) {
	m := &Module{Secret: testSecret}
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "at least one upstream is required") {
		t.Errorf("expected 'at least one upstream is required', got: %v", err)
	}
}

func TestProvision_MultiUpstream_NoCookie(t *testing.T) {
	m := &Module{
		Secret: testSecret,
		Upstreams: []Upstream{
			{CIDR: "10.0.0.0/24", Host: "app1.example.com", Port: 8080},
			{CIDR: "10.0.1.0/24", Host: "app2.example.com", Port: 8080},
		},
	}
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "cookie_name is required") {
		t.Errorf("expected 'cookie_name is required', got: %v", err)
	}
}

func TestProvision_SingleUpstream_NoCookie(t *testing.T) {
	m := validModule()
	m.CookieName = ""
	err := m.Provision(caddy.Context{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestProvision_MissingCIDR(t *testing.T) {
	m := validModule()
	m.Upstreams[0].CIDR = ""
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "cidr is required") {
		t.Errorf("expected 'cidr is required', got: %v", err)
	}
}

func TestProvision_MissingHost(t *testing.T) {
	m := validModule()
	m.Upstreams[0].Host = ""
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "host is required") {
		t.Errorf("expected 'host is required', got: %v", err)
	}
}

func TestProvision_MissingPort(t *testing.T) {
	m := validModule()
	m.Upstreams[0].Port = 0
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "port is required") {
		t.Errorf("expected 'port is required', got: %v", err)
	}
}

func TestProvision_InvalidPort(t *testing.T) {
	tests := []struct {
		port int
	}{
		{-1},    // negative
		{65536}, // above max
	}
	for _, tt := range tests {
		m := validModule()
		m.Upstreams[0].Port = tt.port
		err := m.Provision(caddy.Context{})
		if err == nil || !strings.Contains(err.Error(), "port must be between 1 and 65535") {
			t.Errorf("port %d: expected port range error, got: %v", tt.port, err)
		}
	}
}

func TestProvision_InvalidCIDR(t *testing.T) {
	m := validModule()
	m.Upstreams[0].CIDR = "not-a-cidr"
	err := m.Provision(caddy.Context{})
	if err == nil || !strings.Contains(err.Error(), "invalid upstreams[0].cidr") {
		t.Errorf("expected CIDR error, got: %v", err)
	}
}

func TestProvision_InvalidSecret(t *testing.T) {
	m := validModule()
	m.Secret = "not-base64!!!"
	err := m.Provision(caddy.Context{})
	if err == nil {
		t.Fatal("expected error for invalid secret")
	}
}

func TestUnmarshalCaddyfile_Full(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret c2VjcmV0
	upstream {
		pod_cidr 10.0.0.0/24
		host app.example.com
		upstream_port 8080
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Secret != "c2VjcmV0" {
		t.Errorf("expected secret c2VjcmV0, got %q", m.Secret)
	}
	if len(m.Upstreams) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(m.Upstreams))
	}
	if m.Upstreams[0].CIDR != "10.0.0.0/24" {
		t.Errorf("expected CIDR 10.0.0.0/24, got %q", m.Upstreams[0].CIDR)
	}
	if m.Upstreams[0].Host != "app.example.com" {
		t.Errorf("expected host app.example.com, got %q", m.Upstreams[0].Host)
	}
	if m.Upstreams[0].Port != 8080 {
		t.Errorf("expected port 8080, got %d", m.Upstreams[0].Port)
	}
}

func TestUnmarshalCaddyfile_MultipleUpstreams(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret c2VjcmV0
	upstream {
		pod_cidr 10.0.0.0/24
		host app1.example.com
		upstream_port 8080
	}
	upstream {
		pod_cidr 10.0.1.0/24
		host app2.example.com
		upstream_port 8081
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Upstreams) != 2 {
		t.Fatalf("expected 2 upstreams, got %d", len(m.Upstreams))
	}
	if m.Upstreams[1].Port != 8081 {
		t.Errorf("expected port 8081, got %d", m.Upstreams[1].Port)
	}
}

func TestUnmarshalCaddyfile_UnknownSubdirective(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret c2VjcmV0
	unknown_directive value
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "unknown subdirective") {
		t.Errorf("expected 'unknown subdirective', got: %v", err)
	}
}

func TestUnmarshalCaddyfile_UnknownUpstreamSub(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret c2VjcmV0
	upstream {
		pod_cidr 10.0.0.0/24
		host app.example.com
		upstream_port 8080
		unknown_field value
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "unknown upstream subdirective") {
		t.Errorf("expected 'unknown upstream subdirective', got: %v", err)
	}
}

func TestUnmarshalCaddyfile_MissingSecretArg(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("expected error for missing secret argument")
	}
}

func TestUnmarshalCaddyfile_ExtraArgs(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret c2VjcmV0 extra
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil {
		t.Fatal("expected error for extra arguments")
	}
}

func TestUnmarshalCaddyfile_InvalidPort(t *testing.T) {
	d := caddyfile.NewTestDispenser(`doors_handler {
	secret c2VjcmV0
	upstream {
		pod_cidr 10.0.0.0/24
		host app.example.com
		upstream_port abc
	}
}`)
	var m Module
	err := m.UnmarshalCaddyfile(d)
	if err == nil || !strings.Contains(err.Error(), "invalid upstream_port") {
		t.Errorf("expected 'invalid upstream_port', got: %v", err)
	}
}

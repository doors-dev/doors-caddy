package geo

import (
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/doors-dev/doors-caddy/common"
	"go.uber.org/zap"
)

const (
	defaultIPv4URL = "https://www.ipdeny.com/ipblocks/data/countries/all-zones.tar.gz"
	defaultIPv6URL = "https://www.ipdeny.com/ipv6/ipaddresses/blocks/ipv6-all-zones.tar.gz"
)

type Module struct {
	IPv4URL         string              `json:"ipv4_url,omitempty"`
	IPv6URL         string              `json:"ipv6_url,omitempty"`
	UpdateInterval  caddy.Duration      `json:"update_interval,omitempty"`
	DownloadTimeout caddy.Duration      `json:"download_timeout,omitempty"`
	Redirects       map[string][]string `json:"redirects,omitempty"`
	lookup          map[string]string
	service         *geoService
	logger          *zap.Logger
}

func (Module) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers." + common.DirectiveGeo,
		New: func() caddy.Module { return new(Module) },
	}
}

func (m *Module) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	m.Redirects = make(map[string][]string)
	for d.Next() {
		for d.NextBlock(0) {
			key := d.Val()
			switch key {
			case "ipv4_url":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.IPv4URL = d.Val()
				if d.NextArg() {
					return d.ArgErr()
				}
			case "ipv6_url":
				if !d.NextArg() {
					return d.ArgErr()
				}
				m.IPv6URL = d.Val()
				if d.NextArg() {
					return d.ArgErr()
				}
			case "update_interval":
				if !d.NextArg() {
					return d.ArgErr()
				}
				duration, err := caddy.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("invalid update_interval %q: %v", d.Val(), err)
				}
				m.UpdateInterval = caddy.Duration(duration)
				if d.NextArg() {
					return d.ArgErr()
				}
			case "download_timeout":
				if !d.NextArg() {
					return d.ArgErr()
				}
				duration, err := caddy.ParseDuration(d.Val())
				if err != nil {
					return d.Errf("invalid download_timeout %q: %v", d.Val(), err)
				}
				m.DownloadTimeout = caddy.Duration(duration)
				if d.NextArg() {
					return d.ArgErr()
				}
			default:
				domain := key
				if _, exists := m.Redirects[domain]; exists {
					return d.Errf("duplicate domain block: %s", domain)
				}
				for nesting := d.Nesting(); d.NextBlock(nesting); {
					codes := append([]string{d.Val()}, d.RemainingArgs()...)
					for _, code := range codes {
						code = strings.ToUpper(strings.TrimSpace(code))
						if len(code) != 2 {
							return d.Errf("invalid country code %q", code)
						}
						m.Redirects[domain] = append(m.Redirects[domain], code)
					}
				}
				if len(m.Redirects[domain]) == 0 {
					return d.Errf("domain %q must contain at least one country code", domain)
				}
			}
		}
	}

	return nil
}

func (m *Module) Provision(ctx caddy.Context) (err error) {
	m.lookup = make(map[string]string)
	for domain, countries := range m.Redirects {
		for _, country := range countries {
			m.lookup[country] = domain
		}
	}
	if m.IPv4URL == "" {
		m.IPv4URL = defaultIPv4URL
	}
	if m.IPv6URL == "" {
		m.IPv6URL = defaultIPv6URL
	}
	if m.UpdateInterval == 0 {
		m.UpdateInterval = caddy.Duration(24 * time.Hour)
	}
	if m.DownloadTimeout == 0 {
		m.DownloadTimeout = caddy.Duration(30 * time.Second)
	}
	m.logger = ctx.Logger(m)
	m.service = &geoService{
		IPv4URL:  m.IPv4URL,
		IPv6URL:  m.IPv6URL,
		Logger:   ctx.Logger(m),
		Interval: time.Duration(m.UpdateInterval),
		Timeout:  time.Duration(m.DownloadTimeout),
	}
	m.service.Launch()
	return nil
}

func (m *Module) Cleanup() error {
	if m.service == nil {
		return nil
	}
	m.service.Cancel()
	return nil
}

var (
	_ caddy.Provisioner           = (*Module)(nil)
	_ caddyhttp.MiddlewareHandler = (*Module)(nil)
	_ caddyfile.Unmarshaler       = (*Module)(nil)
)

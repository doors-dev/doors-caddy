package handler

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/common"
	"github.com/doors-dev/doors-caddy/lib"
	"go.uber.org/zap"
)

type Module struct {
	Secret     string     `json:"secret,omitempty"`
	CookieName string     `json:"cookie_name,omitempty"`
	Upstreams  []Upstream `json:"upstreams,omitempty"`
	cipher     lib.TokenCipher
	upstreams  []*reverseproxy.Upstream
	logger     *zap.Logger
}

type Upstream struct {
	CIDR      string `json:"cidr,omitempty"`
	Port      int    `json:"port,omitempty"`
	Host      string `json:"host,omitempty"`
	prefix    netip.Prefix
	upstreams []*reverseproxy.Upstream
}

func (Module) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers." + common.DirectiveHandler,
		New: func() caddy.Module { return new(Module) },
	}
}

func (m *Module) Provision(ctx caddy.Context) (err error) {
	m.logger = ctx.Logger(m)
	if m.Secret == "" {
		return fmt.Errorf("secret is required")
	}
	if len(m.Upstreams) == 0 {
		return fmt.Errorf("at least one upstream is required")
	}
	if len(m.Upstreams) > 1 {
		if m.CookieName == "" {
			return fmt.Errorf("cookie_name is required when using more than 1 upstream")
		}
	}
	m.upstreams = make([]*reverseproxy.Upstream, 0, len(m.Upstreams))
	for i := range m.Upstreams {
		upstream := &m.Upstreams[i]
		if upstream.CIDR == "" {
			return fmt.Errorf("upstreams[%d].cidr is required", i)
		}
		if upstream.Host == "" {
			return fmt.Errorf("upstreams[%d].host is required", i)
		}
		if upstream.Port == 0 {
			return fmt.Errorf("upstreams[%d].port is required", i)
		}
		if upstream.Port < 1 || upstream.Port > 65535 {
			return fmt.Errorf("upstreams[%d].port must be between 1 and 65535", i)
		}
		upstream.prefix, err = netip.ParsePrefix(upstream.CIDR)
		if err != nil {
			return fmt.Errorf("invalid upstreams[%d].cidr %q: %w", i, upstream.CIDR, err)
		}
		reverseUpstream := &reverseproxy.Upstream{
			Dial: net.JoinHostPort(
				upstream.Host,
				strconv.Itoa(upstream.Port),
			),
		}
		upstream.upstreams = []*reverseproxy.Upstream{reverseUpstream}
		m.upstreams = append(m.upstreams, reverseUpstream)
	}
	m.cipher, err = lib.NewTokenCipher(m.Secret)
	return err
}

var (
	_ caddy.Provisioner           = (*Module)(nil)
	_ caddyhttp.MiddlewareHandler = (*Module)(nil)
	_ caddyfile.Unmarshaler       = (*Module)(nil)
)

func (m *Module) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	d.Next()
	for d.NextBlock(0) {
		switch d.Val() {
		case "secret":
			if !d.NextArg() {
				return d.ArgErr()
			}
			m.Secret = d.Val()
			if d.NextArg() {
				return d.ArgErr()
			}
		case "upstream":
			if d.NextArg() {
				return d.ArgErr()
			}
			var upstream Upstream
			nesting := d.Nesting()
			for d.NextBlock(nesting) {
				switch d.Val() {
				case "pod_cidr":
					if !d.NextArg() {
						return d.ArgErr()
					}
					upstream.CIDR = d.Val()
					if d.NextArg() {
						return d.ArgErr()
					}
				case "host":
					if !d.NextArg() {
						return d.ArgErr()
					}
					upstream.Host = d.Val()
					if d.NextArg() {
						return d.ArgErr()
					}
				case "upstream_port":
					if !d.NextArg() {
						return d.ArgErr()
					}
					port, err := strconv.Atoi(d.Val())
					if err != nil {
						return d.Errf("invalid upstream_port %q", d.Val())
					}
					upstream.Port = port
					if d.NextArg() {
						return d.ArgErr()
					}
				default:
					return d.Errf("unknown upstream subdirective %q", d.Val())
				}
			}
			m.Upstreams = append(m.Upstreams, upstream)
		default:
			return d.Errf("unknown subdirective %q", d.Val())
		}
	}

	return nil
}

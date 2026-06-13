package upstream

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/common"
)

type Module struct{}

func (Module) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.upstreams." + common.DirectiveUpstreams,
		New: func() caddy.Module { return new(Module) },
	}
}

func (m *Module) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	if !d.Next() {
		return d.ArgErr()
	}
	if d.NextArg() {
		return d.ArgErr()
	}
	return nil
}

func (m *Module) Provision(ctx caddy.Context) (err error) {
	return nil
}

var _ reverseproxy.UpstreamSource = (*Module)(nil)
var _ caddyfile.Unmarshaler = (*Module)(nil)

package upstream

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

type Module struct{}

func (Module) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.reverse_proxy.upstreams" + Directive,
		New: func() caddy.Module { return new(Module) },
	}
}

func (m *Module) Provision(ctx caddy.Context) (err error) {
	return nil
}

var _ reverseproxy.UpstreamSource = (*Module)(nil)

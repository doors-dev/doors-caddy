package geo

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/doors-dev/doors-caddy/common"
)

func Register() {
	caddy.RegisterModule(Module{})
	httpcaddyfile.RegisterHandlerDirective(common.DirectiveGeo, func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		var m Module
		err := m.UnmarshalCaddyfile(h.Dispenser)
		return &m, err
	})
	httpcaddyfile.RegisterDirectiveOrder(common.DirectiveGeo, httpcaddyfile.Before, common.DirectiveHandler)
}

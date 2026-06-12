package handler

import (
	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

const Directive = "doors_handler"

func Register() {
	caddy.RegisterModule(Module{})
	httpcaddyfile.RegisterHandlerDirective(Directive, func(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
		var m Module
		err := m.UnmarshalCaddyfile(h.Dispenser)
		return &m, err
	})
	httpcaddyfile.RegisterDirectiveOrder(Directive, httpcaddyfile.Before, "reverse_proxy")
}

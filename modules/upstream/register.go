package upstream

import (
	"github.com/caddyserver/caddy/v2"
)

const Directive = "doors_upstreams"

func Register() {
	caddy.RegisterModule(Module{})
}

package upstream

import (
	"github.com/caddyserver/caddy/v2"
)

const Directive = "doors_upstream"

func Register() {
	caddy.RegisterModule(Module{})
}

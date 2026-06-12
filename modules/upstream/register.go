package upstream

import (
	"github.com/caddyserver/caddy/v2"
)

func Register() {
	caddy.RegisterModule(Module{})
}

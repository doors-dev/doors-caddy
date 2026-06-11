package upstream

import (
	"github.com/caddyserver/caddy/v2"
)

func init() {
	caddy.RegisterModule(Module{})
}

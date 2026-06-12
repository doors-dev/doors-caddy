package caddy

import (
	"github.com/doors-dev/doors-caddy/modules/handler"
	"github.com/doors-dev/doors-caddy/modules/upstream"
)

func init() {
	handler.Register()
	upstream.Register()
}

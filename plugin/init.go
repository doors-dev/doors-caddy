package plugin

import (
	"github.com/doors-dev/doors-caddy/modules/geo"
	"github.com/doors-dev/doors-caddy/modules/handler"
	"github.com/doors-dev/doors-caddy/modules/upstream"
)

func init() {
	geo.Register()
	handler.Register()
	upstream.Register()
}

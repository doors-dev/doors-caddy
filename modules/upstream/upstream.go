package upstream

import (
	"errors"
	"net/http"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/common"
)

var errorNoUpstreams = errors.New("upstreams are not provider by doors_handler; ensure you are using doors_handler directive before reverse proxy")

func (m Module) GetUpstreams(r *http.Request) ([]*reverseproxy.Upstream, error) {
	upstreams := common.GetUpstreams(r)
	if len(upstreams) == 0 {
		return nil, errorNoUpstreams
	}
	return upstreams, nil
}

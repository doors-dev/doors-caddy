package common

import (
	"net/http"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
)

const upstreamKey = "doors.upstreams"

func SetUpstreams(r *http.Request, u []*reverseproxy.Upstream) {
	caddyhttp.SetVar(r.Context(), upstreamKey, u)
}

func GetUpstreams(r *http.Request) []*reverseproxy.Upstream {
	upstreams, _ := caddyhttp.GetVar(r.Context(), upstreamKey).([]*reverseproxy.Upstream)
	return upstreams
}

package upstream

import (
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/lib"
	"go.uber.org/zap"
)

var errorDecode = errors.New("failed to decode dynamic upstream: stale client, wrong configuration or attack")
var errorCIDR = errors.New("upstream failed CIDR match: stale client, wrong configureation or attack")

func (m *Module) GetUpstreams(r *http.Request) ([]*reverseproxy.Upstream, error) {
	token, ok := tokenFromPath(r.URL.Path)
	if !ok {
		return m.defaultUpstream(r)
	}
	ip, err := m.cipher.Decode(token)
	if err != nil {
		return nil, lib.ErrorsJoin(errorDecode, err)
	}
	var reverseUpstream *reverseproxy.Upstream
	for _, upstream := range m.Upstreams {
		if !upstream.prefix.Contains(ip) {
			continue
		}
		reverseUpstream = &reverseproxy.Upstream{
			Dial: net.JoinHostPort(
				ip.String(),
				strconv.Itoa(upstream.Port),
			),
		}
	}
	if reverseUpstream == nil {
		return nil, errorCIDR
	}
	return []*reverseproxy.Upstream{reverseUpstream}, nil

}

func (m *Module) defaultUpstream(r *http.Request) ([]*reverseproxy.Upstream, error) {
	if len(m.Upstreams) == 1 {
		return m.upstreams, nil
	}
	token, ok := tokenFromCookie(m.CookieName, r)
	if !ok {
		return m.upstreams, nil
	}
	ip, err := m.cipher.Decode(token)
	if err != nil {
		m.logger.Warn(errorCIDR.Error(),
			zap.Error(err),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("host", r.Host),
			zap.String("remote_addr", r.RemoteAddr),
		)
		return m.upstreams, nil
	}
	for _, upstream := range m.Upstreams {
		if upstream.prefix.Contains(ip) {
			return upstream.upstreams, nil
		}
	}
	m.logger.Warn(errorCIDR.Error(),
		zap.Error(err),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("host", r.Host),
		zap.String("remote_addr", r.RemoteAddr),
	)
	return m.upstreams, nil
}

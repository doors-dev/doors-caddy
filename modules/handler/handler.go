package handler

import (
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	"github.com/doors-dev/doors-caddy/common"
	"go.uber.org/zap"
)

var errorDecode = errors.New("failed to decode dynamic upstream: stale client, wrong configuration or attack")
var errorCIDR = errors.New("upstream failed CIDR match: stale client, wrong configureation or attack")

func (m Module) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	token, ok := tokenFromPath(r.URL.Path)
	if !ok {
		m.setCommonUpstreams(r)
		return next.ServeHTTP(w, r)
	}
	if !m.setSystemUpstreams(token, r) {
		w.WriteHeader(http.StatusGone)
		return nil
	}
	return next.ServeHTTP(w, r)
}

func (m *Module) setSystemUpstreams(token string, r *http.Request) bool {
	ip, err := m.cipher.Decode(token)
	if err != nil {
		m.logger.Warn(errorDecode.Error(),
			zap.Error(err),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("host", r.Host),
			zap.String("remote_addr", r.RemoteAddr),
		)
		return false
	}
	for _, upstream := range m.Upstreams {
		if !upstream.prefix.Contains(ip) {
			continue
		}
		common.SetUpstreams(r, []*reverseproxy.Upstream{{
			Dial: net.JoinHostPort(
				ip.String(),
				strconv.Itoa(upstream.Port),
			),
		}})
		return true
	}
	return false
}

func (m *Module) setCommonUpstreams(r *http.Request) {
	if len(m.Upstreams) == 1 {
		common.SetUpstreams(r, m.upstreams)
		return
	}
	token, ok := tokenFromCookie(m.CookieName, r)
	if !ok {
		common.SetUpstreams(r, m.upstreams)
		return
	}
	ip, err := m.cipher.Decode(token)
	if err != nil {
		m.logger.Warn(errorDecode.Error(),
			zap.Error(err),
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("host", r.Host),
			zap.String("remote_addr", r.RemoteAddr),
		)
		common.SetUpstreams(r, m.upstreams)
		return
	}
	for _, upstream := range m.Upstreams {
		if !upstream.prefix.Contains(ip) {
			continue
		}
		common.SetUpstreams(r, upstream.upstreams)
		return
	}
	m.logger.Warn(errorCIDR.Error(),
		zap.String("method", r.Method),
		zap.String("path", r.URL.Path),
		zap.String("host", r.Host),
		zap.String("remote_addr", r.RemoteAddr),
	)
	common.SetUpstreams(r, m.upstreams)
}

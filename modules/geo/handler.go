package geo

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func (m Module) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	if strings.HasPrefix(r.URL.Path, "/.well-known/acme-challenge/") {
		return next.ServeHTTP(w, r)
	}
	ip, err := clientIP(r)
	if err != nil {
		m.logger.Error("failed to get client IP",
			zap.Error(err),
			zap.String("remote_addr", r.RemoteAddr),
		)
		return next.ServeHTTP(w, r)
	}
	country, found := m.service.Lookup(ip)
	if !found {
		m.logger.Warn("country not found or database not ready",
			zap.String("ip", ip.String()),
		)
		return next.ServeHTTP(w, r)
	}
	domain, exists := m.lookup[country]
	if !exists {
		m.logger.Warn("no redirect configured for country",
			zap.String("country", country),
			zap.String("ip", ip.String()),
		)
		return next.ServeHTTP(w, r)
	}
	if domain == r.Host {
		return next.ServeHTTP(w, r)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURL := scheme + "://" + domain + r.URL.RequestURI()
	http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
	return nil
}

func clientIP(r *http.Request) (netip.Addr, error) {
	if s, _ := caddyhttp.GetVar(r.Context(), caddyhttp.ClientIPVarKey).(string); s != "" {
		addr, err := netip.ParseAddr(s)
		if err == nil {
			return addr, nil
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return netip.Addr{}, err
	}
	return netip.ParseAddr(host)
}

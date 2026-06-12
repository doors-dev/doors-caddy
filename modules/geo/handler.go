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
	m.logger.Debug("geo handler: request received",
		zap.String("host", r.Host),
		zap.String("url", r.URL.String()),
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("method", r.Method),
	)
	if strings.HasPrefix(r.URL.Path, "/~/") {
		m.logger.Debug("geo handler: skipping system path",
			zap.String("path", r.URL.Path),
		)
		return next.ServeHTTP(w, r)
	}
	if r.Method != http.MethodGet {
		m.logger.Debug("geo handler: skipping non-GET request",
			zap.String("method", r.Method),
		)
		return next.ServeHTTP(w, r)
	}
	ip, err := clientIP(r)
	if err != nil {
		m.logger.Warn("geo handler: failed to get client IP, passing through",
			zap.Error(err),
			zap.String("remote_addr", r.RemoteAddr),
		)
		return next.ServeHTTP(w, r)
	}
	m.logger.Debug("geo handler: client IP resolved",
		zap.String("ip", ip.String()),
		zap.Bool("is_v4", ip.Is4()),
		zap.Bool("is_v6", ip.Is6()),
	)
	country, found := m.service.Lookup(ip)
	if !found {
		m.logger.Warn("geo handler: country not found for IP, database may not be ready, passing through",
			zap.String("ip", ip.String()),
		)
		return next.ServeHTTP(w, r)
	}
	m.logger.Debug("geo handler: country resolved from IP",
		zap.String("ip", ip.String()),
		zap.String("country", country),
	)
	domain, exists := m.lookup[country]
	if !exists {
		m.logger.Warn("geo handler: no redirect configured for country, passing through",
			zap.String("country", country),
			zap.String("ip", ip.String()),
		)
		return next.ServeHTTP(w, r)
	}
	m.logger.Debug("geo handler: redirect domain found for country",
		zap.String("country", country),
		zap.String("target_domain", domain),
		zap.String("request_host", r.Host),
	)
	if domain == r.Host {
		m.logger.Debug("geo handler: already on target domain, passing through",
			zap.String("host", r.Host),
			zap.String("country", country),
		)
		return next.ServeHTTP(w, r)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	redirectURL := scheme + "://" + domain + r.URL.RequestURI()
	m.logger.Info("geo handler: redirecting",
		zap.String("ip", ip.String()),
		zap.String("country", country),
		zap.String("target", redirectURL),
		zap.Int("status", http.StatusTemporaryRedirect),
	)
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

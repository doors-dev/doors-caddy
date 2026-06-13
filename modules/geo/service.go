package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/netip"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/doors-dev/doors-caddy/lib"
	"github.com/gaissmai/bart"
	"go.uber.org/zap"
)

const maxBodySize = 32 * 1024 * 1024

type geoService struct {
	TarballURL string
	Interval   time.Duration
	Timeout    time.Duration
	Logger     *zap.Logger
	cancel     context.CancelFunc
	v4         atomic.Pointer[bart.Table[string]]
	v6         atomic.Pointer[bart.Table[string]]
}

func (m *geoService) Launch() {
	m.Logger.Info("geo service: launching background updater",
		zap.String("tarball_url", m.TarballURL),
		zap.Duration("interval", m.Interval),
		zap.Duration("timeout", m.Timeout),
	)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go updater{
		Ctx:        ctx,
		TarballURL: m.TarballURL,
		V4:         &m.v4,
		V6:         &m.v6,
		Logger:     m.Logger,
		Interval:   m.Interval,
		Timeout:    m.Timeout,
	}.Run()
}

func (g *geoService) Lookup(ip netip.Addr) (string, bool) {
	ip = ip.Unmap()
	var table *bart.Table[string]
	if ip.Is4() {
		table = g.v4.Load()
		g.Logger.Debug("geo service: Lookup using IPv4 table",
			zap.String("ip", ip.String()),
			zap.Bool("table_loaded", table != nil),
		)
	}
	if ip.Is6() {
		table = g.v6.Load()
		g.Logger.Debug("geo service: Lookup using IPv6 table",
			zap.String("ip", ip.String()),
			zap.Bool("table_loaded", table != nil),
		)
	}
	if table == nil {
		g.Logger.Warn("geo service: Lookup table is nil, database not yet loaded",
			zap.String("ip", ip.String()),
			zap.Bool("is_v4", ip.Is4()),
			zap.Bool("is_v6", ip.Is6()),
		)
		return "", false
	}
	country, found := table.Lookup(ip)
	g.Logger.Debug("geo service: Lookup result",
		zap.String("ip", ip.String()),
		zap.String("country", country),
		zap.Bool("found", found),
	)
	return country, found
}

func (m *geoService) Cancel() {
	m.Logger.Info("geo service: cancelling background updater")
	m.cancel()
}

type HTTPMeta struct {
	ETag         string
	LastModified string
}

type updater struct {
	Ctx        context.Context
	TarballURL string
	V4         *atomic.Pointer[bart.Table[string]]
	V6         *atomic.Pointer[bart.Table[string]]
	Logger     *zap.Logger
	Interval   time.Duration
	Timeout    time.Duration
	failures   int
	meta       HTTPMeta
}

func (u updater) Run() {
	u.Logger.Info("geo updater: starting",
		zap.String("tarball_url", u.TarballURL),
		zap.Duration("interval", u.Interval),
		zap.Duration("timeout", u.Timeout),
	)
	for {
		err := u.update()
		var delay time.Duration
		if err != nil {
			u.Logger.Error("geo updater: IP database update error",
				zap.String("tarball_url", u.TarballURL),
				zap.Error(err),
			)
			delay = u.retry()
			u.Logger.Warn("geo updater: retrying after delay",
				zap.String("tarball_url", u.TarballURL),
				zap.Duration("delay", delay),
				zap.Int("consecutive_failures", u.failures),
			)
		} else {
			delay = u.wait()
			u.Logger.Info("geo updater: update succeeded, waiting for next cycle",
				zap.String("tarball_url", u.TarballURL),
				zap.Duration("delay", delay),
			)
		}
		select {
		case <-time.After(delay):
		case <-u.Ctx.Done():
			u.Logger.Info("geo updater: context cancelled, stopping",
				zap.String("tarball_url", u.TarballURL),
			)
			return
		}
	}
}

var errorRequest = errors.New("request error")
var errorParse = errors.New("parse error")

func (u *updater) wait() time.Duration {
	u.failures = 0
	return jitter(u.Interval)
}

const (
	retryMin = 30 * time.Second
	retryMax = 1 * time.Hour
)

func (u *updater) retry() time.Duration {
	delay := retryMin << uint(u.failures)
	if delay < retryMax {
		u.failures += 1
	}
	return jitter(min(delay, retryMax))
}

func (u *updater) update() error {
	u.Logger.Debug("geo updater: downloading database",
		zap.String("tarball_url", u.TarballURL),
		zap.String("etag", u.meta.ETag),
		zap.String("last_modified", u.meta.LastModified),
	)
	req, err := http.NewRequestWithContext(u.Ctx, http.MethodGet, u.TarballURL, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("User-Agent", "doors-caddy/1.0")
	if u.meta.ETag != "" {
		req.Header.Set("If-None-Match", u.meta.ETag)
	}
	if u.meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", u.meta.LastModified)
	}
	client := &http.Client{Timeout: u.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		u.Logger.Error("geo updater: HTTP request failed",
			zap.String("tarball_url", u.TarballURL),
			zap.Error(err),
		)
		return err
	}
	defer resp.Body.Close()

	u.Logger.Debug("geo updater: HTTP response received",
		zap.String("tarball_url", u.TarballURL),
		zap.Int("status", resp.StatusCode),
		zap.String("status_text", resp.Status),
		zap.Int64("content_length", resp.ContentLength),
		zap.String("etag", resp.Header.Get("ETag")),
		zap.String("last_modified", resp.Header.Get("Last-Modified")),
	)

	if resp.StatusCode == http.StatusNotModified {
		u.Logger.Debug("geo updater: database not modified (304)",
			zap.String("tarball_url", u.TarballURL),
		)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		u.Logger.Error("geo updater: unexpected HTTP status",
			zap.String("tarball_url", u.TarballURL),
			zap.Int("status", resp.StatusCode),
			zap.String("status_text", resp.Status),
		)
		return lib.ErrorsJoin(errorRequest, fmt.Errorf("HTTP %s", resp.Status))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		u.Logger.Error("geo updater: failed to read response body",
			zap.String("tarball_url", u.TarballURL),
			zap.Error(err),
		)
		return lib.ErrorsJoin(errorRequest, err)
	}

	u.Logger.Debug("geo updater: database downloaded",
		zap.String("tarball_url", u.TarballURL),
		zap.Int("size_bytes", len(data)),
	)

	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	if err := parseTarball(data, v4, v6); err != nil {
		u.Logger.Error("geo updater: failed to parse tarball",
			zap.String("tarball_url", u.TarballURL),
			zap.Error(err),
		)
		return lib.ErrorsJoin(errorParse, err)
	}
	u.V4.Store(v4)
	u.V6.Store(v6)
	u.meta.ETag = resp.Header.Get("ETag")
	u.meta.LastModified = resp.Header.Get("Last-Modified")

	u.Logger.Info("geo updater: database loaded successfully",
		zap.String("tarball_url", u.TarballURL),
		zap.Int("v4_entries", v4.Size()),
		zap.Int("v6_entries", v6.Size()),
	)

	return nil
}

type ipverseJSON struct {
	CountryCode string `json:"countryCode"`
	Prefixes    struct {
		IPv4 []string `json:"ipv4"`
		IPv6 []string `json:"ipv6"`
	} `json:"prefixes"`
}

func parseTarball(data []byte, v4, v6 *bart.Table[string]) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		_, ok := extractCountry(hdr.Name)
		if !ok {
			continue
		}
		var entry ipverseJSON
		if err := json.NewDecoder(tr).Decode(&entry); err != nil {
			return fmt.Errorf("%s: %w", hdr.Name, err)
		}
		if entry.CountryCode == "" {
			continue
		}
		for _, cidr := range entry.Prefixes.IPv4 {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return fmt.Errorf("%s: bad ipv4 prefix %q: %w", hdr.Name, cidr, err)
			}
			v4.Insert(prefix.Masked(), entry.CountryCode)
		}
		for _, cidr := range entry.Prefixes.IPv6 {
			prefix, err := netip.ParsePrefix(cidr)
			if err != nil {
				return fmt.Errorf("%s: bad ipv6 prefix %q: %w", hdr.Name, cidr, err)
			}
			v6.Insert(prefix.Masked(), entry.CountryCode)
		}
	}
	return nil
}

func extractCountry(name string) (string, bool) {
	parts := strings.Split(path.Clean(name), "/")
	if len(parts) < 4 {
		return "", false
	}
	if parts[len(parts)-3] != "country" {
		return "", false
	}
	cc := parts[len(parts)-2]
	if len(cc) != 2 {
		return "", false
	}
	if parts[len(parts)-1] != cc+".json" {
		return "", false
	}
	return cc, true
}

func jitter(dur time.Duration) time.Duration {
	return dur + time.Duration(rand.Int63n(int64(dur/5))) - dur/10
}

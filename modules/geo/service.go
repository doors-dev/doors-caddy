package geo

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/netip"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/doors-dev/doors-caddy/common"
	"github.com/gaissmai/bart"
	"go.uber.org/zap"
)

const maxBodySize = 8 * 1024 * 1024

type geoService struct {
	IPv4URL  string
	IPv6URL  string
	Interval time.Duration
	Timeout  time.Duration
	Logger   *zap.Logger
	cancel   context.CancelFunc
	v4       atomic.Pointer[bart.Table[string]]
	v6       atomic.Pointer[bart.Table[string]]
}

func (m *geoService) Launch() {
	m.Logger.Info("geo service: launching background updaters",
		zap.String("ipv4_url", m.IPv4URL),
		zap.String("ipv6_url", m.IPv6URL),
		zap.Duration("interval", m.Interval),
		zap.Duration("timeout", m.Timeout),
	)
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	go updater{
		Ctx:      ctx,
		Url:      m.IPv4URL,
		Cell:     &m.v4,
		Logger:   m.Logger,
		Interval: m.Interval,
		Timeout:  m.Timeout,
	}.Run()
	go updater{
		Ctx:      ctx,
		Url:      m.IPv6URL,
		Cell:     &m.v6,
		Logger:   m.Logger,
		Interval: m.Interval,
		Timeout:  m.Timeout,
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
	m.Logger.Info("geo service: cancelling background updaters")
	m.cancel()
}

type HTTPMeta struct {
	ETag         string
	LastModified string
}

type updater struct {
	Ctx      context.Context
	Url      string
	Cell     *atomic.Pointer[bart.Table[string]]
	Logger   *zap.Logger
	Interval time.Duration
	Timeout  time.Duration
	failures int
	meta     HTTPMeta
}

func (u updater) Run() {
	u.Logger.Info("geo updater: starting",
		zap.String("url", u.Url),
		zap.Duration("interval", u.Interval),
		zap.Duration("timeout", u.Timeout),
	)
	for {
		err := u.update()
		var delay time.Duration
		if err != nil {
			u.Logger.Error("geo updater: IP database update error",
				zap.String("url", u.Url),
				zap.Error(err),
			)
			delay = u.retry()
			u.Logger.Warn("geo updater: retrying after delay",
				zap.String("url", u.Url),
				zap.Duration("delay", delay),
				zap.Int("consecutive_failures", u.failures),
			)
		} else {
			delay = u.wait()
			u.Logger.Info("geo updater: update succeeded, waiting for next cycle",
				zap.String("url", u.Url),
				zap.Duration("delay", delay),
			)
		}
		select {
		case <-time.After(delay):
		case <-u.Ctx.Done():
			u.Logger.Info("geo updater: context cancelled, stopping",
				zap.String("url", u.Url),
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
		zap.String("url", u.Url),
		zap.String("etag", u.meta.ETag),
		zap.String("last_modified", u.meta.LastModified),
	)
	req, err := http.NewRequestWithContext(u.Ctx, http.MethodGet, u.Url, nil)
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
			zap.String("url", u.Url),
			zap.Error(err),
		)
		return err
	}
	defer resp.Body.Close()

	u.Logger.Debug("geo updater: HTTP response received",
		zap.String("url", u.Url),
		zap.Int("status", resp.StatusCode),
		zap.String("status_text", resp.Status),
		zap.Int64("content_length", resp.ContentLength),
		zap.String("etag", resp.Header.Get("ETag")),
		zap.String("last_modified", resp.Header.Get("Last-Modified")),
	)

	if resp.StatusCode == http.StatusNotModified {
		u.Logger.Debug("geo updater: database not modified (304)",
			zap.String("url", u.Url),
		)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		u.Logger.Error("geo updater: unexpected HTTP status",
			zap.String("url", u.Url),
			zap.Int("status", resp.StatusCode),
			zap.String("status_text", resp.Status),
		)
		return common.ErrorsJoin(errorRequest, fmt.Errorf("HTTP %s", resp.Status))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		u.Logger.Error("geo updater: failed to read response body",
			zap.String("url", u.Url),
			zap.Error(err),
		)
		return common.ErrorsJoin(errorRequest, err)
	}
	u.Logger.Debug("geo updater: database downloaded",
		zap.String("url", u.Url),
		zap.Int("size_bytes", len(data)),
	)

	table := new(bart.Table[string])
	if err := parseArchive(data, table); err != nil {
		u.Logger.Error("geo updater: failed to parse archive",
			zap.String("url", u.Url),
			zap.Error(err),
		)
		return common.ErrorsJoin(errorParse, err)
	}
	u.Cell.Store(table)
	u.meta.ETag = resp.Header.Get("ETag")
	u.meta.LastModified = resp.Header.Get("Last-Modified")

	u.Logger.Info("geo updater: database loaded successfully",
		zap.String("url", u.Url),
		zap.Int("entries", table.Size()),
	)

	return nil
}

func parseArchive(data []byte, table *bart.Table[string]) error {
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
		name := filepath.Base(hdr.Name)
		if !strings.HasSuffix(name, ".zone") {
			continue
		}
		country := strings.ToUpper(strings.TrimSuffix(name, ".zone"))
		if len(country) != 2 {
			continue
		}
		scanner := bufio.NewScanner(tr)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			prefix, err := netip.ParsePrefix(line)
			if err != nil {
				return fmt.Errorf("%s: bad prefix %q: %w", name, line, err)
			}
			table.Insert(prefix.Masked(), country)
		}
		if err := scanner.Err(); err != nil {
			return err
		}
	}
	return nil
}

func jitter(dur time.Duration) time.Duration {
	return dur + time.Duration(rand.Int63n(int64(dur/5))) - dur/10
}

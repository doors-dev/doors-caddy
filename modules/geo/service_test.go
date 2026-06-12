package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gaissmai/bart"
	"go.uber.org/zap"
)

func TestGeoService_Lookup_IPv4(t *testing.T) {
	s := fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil)
	country, found := s.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found {
		t.Fatal("expected found=true")
	}
	if country != "US" {
		t.Errorf("expected US, got %q", country)
	}
}

func TestGeoService_Lookup_IPv6(t *testing.T) {
	s := fakeGeoServiceInstance(nil, map[string]string{"2001:db8::/48": "GB"})
	country, found := s.Lookup(netip.MustParseAddr("2001:db8::1"))
	if !found {
		t.Fatal("expected found=true")
	}
	if country != "GB" {
		t.Errorf("expected GB, got %q", country)
	}
}

func TestGeoService_Lookup_NotFound(t *testing.T) {
	s := fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil)
	country, found := s.Lookup(netip.MustParseAddr("5.6.7.8"))
	if found {
		t.Error("expected found=false")
	}
	if country != "" {
		t.Errorf("expected empty string, got %q", country)
	}
}

func TestGeoService_Lookup_NilTable(t *testing.T) {
	s := &geoService{Logger: zap.NewNop()}
	country, found := s.Lookup(netip.MustParseAddr("1.2.3.4"))
	if found {
		t.Error("expected found=false for nil table")
	}
	if country != "" {
		t.Errorf("expected empty string, got %q", country)
	}
}

func TestGeoService_Lookup_IPv4MappedIPv6(t *testing.T) {
	s := fakeGeoServiceInstance(map[string]string{"1.2.3.0/24": "US"}, nil)
	ip := netip.MustParseAddr("::ffff:1.2.3.4")
	country, found := s.Lookup(ip)
	if !found {
		t.Fatal("expected found=true for IPv4-mapped IPv6")
	}
	if country != "US" {
		t.Errorf("expected US, got %q", country)
	}
}

func TestParseArchive_Valid(t *testing.T) {
	data := makeTarGz(t, map[string][]string{
		"us.zone": {"1.2.3.0/24", "5.6.7.0/24"},
		"gb.zone": {"10.0.0.0/8"},
	})
	table := new(bart.Table[string])
	err := parseArchive(data, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	country, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found || country != "US" {
		t.Errorf("expected US, got %q (found=%v)", country, found)
	}

	country, found = table.Lookup(netip.MustParseAddr("10.0.0.1"))
	if !found || country != "GB" {
		t.Errorf("expected GB, got %q (found=%v)", country, found)
	}
}

func TestParseArchive_SkipsNonZone(t *testing.T) {
	data := makeTarGz(t, map[string][]string{
		"us.zone":  {"1.2.3.0/24"},
		"readme.txt": {"some text"},
	})
	table := new(bart.Table[string])
	err := parseArchive(data, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found {
		t.Error("expected US to be loaded")
	}
}

func TestParseArchive_SkipsDirs(t *testing.T) {
	data := makeTarGzWithDir(t, "somedir", map[string][]string{
		"us.zone": {"1.2.3.0/24"},
	})
	table := new(bart.Table[string])
	err := parseArchive(data, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found {
		t.Error("expected US to be loaded despite directory entries")
	}
}

func TestParseArchive_SkipsComments(t *testing.T) {
	data := makeTarGz(t, map[string][]string{
		"us.zone": {"# this is a comment", "", "   ", "1.2.3.0/24", "# another comment"},
	})
	table := new(bart.Table[string])
	err := parseArchive(data, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found {
		t.Error("expected US after skipping comments and blanks")
	}
}

func TestParseArchive_InvalidCountryLen(t *testing.T) {
	data := makeTarGz(t, map[string][]string{
		"usa.zone": {"1.2.3.0/24"},
		"x.zone":   {"5.6.7.0/24"},
		"us.zone":  {"10.0.0.0/8"},
	})
	table := new(bart.Table[string])
	err := parseArchive(data, table)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// usa.zone (len 3) and x.zone (len 1) should be skipped
	_, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if found {
		t.Error("usa.zone should be skipped (country code len != 2)")
	}
	_, found = table.Lookup(netip.MustParseAddr("5.6.7.8"))
	if found {
		t.Error("x.zone should be skipped (country code len != 2)")
	}
	// us.zone should be loaded
	_, found = table.Lookup(netip.MustParseAddr("10.0.0.1"))
	if !found {
		t.Error("us.zone should be loaded")
	}
}

func TestParseArchive_BadPrefix(t *testing.T) {
	data := makeTarGz(t, map[string][]string{
		"us.zone": {"not-a-cidr"},
	})
	table := new(bart.Table[string])
	err := parseArchive(data, table)
	if err == nil {
		t.Fatal("expected error for bad prefix")
	}
	if !strings.Contains(err.Error(), "bad prefix") {
		t.Errorf("expected 'bad prefix' in error, got: %v", err)
	}
}

func TestParseArchive_NotGzip(t *testing.T) {
	table := new(bart.Table[string])
	err := parseArchive([]byte("not gzip data"), table)
	if err == nil {
		t.Fatal("expected error for non-gzip data")
	}
}

func TestRetry_ExponentialBackoff(t *testing.T) {
	u := updater{failures: 0}
	d1 := u.retry()
	if d1 < retryMin-retryMin/10 || d1 > retryMin+retryMin/10 {
		t.Errorf("retry 1 should be ~30s (+/-10%%), got %v", d1)
	}
	if u.failures != 1 {
		t.Errorf("expected failures=1 after first retry, got %d", u.failures)
	}

	d2 := u.retry()
	base2 := retryMin << 1 // 60s
	if d2 < base2-base2/10 || d2 > base2+base2/10 {
		t.Errorf("retry 2 should be ~60s (+/-10%%), got %v", d2)
	}
	if u.failures != 2 {
		t.Errorf("expected failures=2 after second retry, got %d", u.failures)
	}

	d3 := u.retry()
	base3 := retryMin << 2 // 120s
	if d3 < base3-base3/10 || d3 > base3+base3/10 {
		t.Errorf("retry 3 should be ~120s (+/-10%%), got %v", d3)
	}
}

func TestRetry_CapsAt1Hour(t *testing.T) {
	u := updater{failures: 20}
	delay := u.retry()
	// base delay capped at 1h, jitter can add up to 10%, so max is ~1h6m
	maxWithJitter := retryMax + retryMax/10
	if delay > maxWithJitter {
		t.Errorf("retry should be capped at ~1h (+10%% jitter), got %v", delay)
	}
}

func TestRetry_JitterBounds(t *testing.T) {
	u := updater{failures: 0}
	baseDelay := retryMin // 30s after 0 failures
	delay := u.retry()

	minExpected := baseDelay - baseDelay/10
	maxExpected := baseDelay + baseDelay/5

	if delay < minExpected {
		t.Errorf("jitter too low: %v < %v", delay, minExpected)
	}
	if delay > maxExpected {
		t.Errorf("jitter too high: %v > %v", delay, maxExpected)
	}
}

func TestWait_ResetsFailures(t *testing.T) {
	u := updater{failures: 5, Interval: time.Hour}
	_ = u.wait()
	if u.failures != 0 {
		t.Errorf("wait() should reset failures to 0, got %d", u.failures)
	}
}

func TestUpdater_304NotModified(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer ts.Close()

	u := updater{
		Ctx:      context.Background(),
		Url:      ts.URL,
		Cell:     &atomic.Pointer[bart.Table[string]]{},
		Logger:   zap.NewNop(),
		Interval: time.Hour,
		Timeout:  5 * time.Second,
	}
	err := u.update()
	if err != nil {
		t.Fatalf("expected nil error for 304, got: %v", err)
	}
}

func TestUpdater_200UpdatesTable(t *testing.T) {
	archive := makeTarGz(t, map[string][]string{
		"us.zone": {"1.2.3.0/24"},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Thu, 01 Jan 2025 00:00:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write(archive)
	}))
	defer ts.Close()

	cell := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:      context.Background(),
		Url:      ts.URL,
		Cell:     cell,
		Logger:   zap.NewNop(),
		Interval: time.Hour,
		Timeout:  5 * time.Second,
	}
	err := u.update()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	table := cell.Load()
	if table == nil {
		t.Fatal("table not set after update")
	}
	country, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found || country != "US" {
		t.Errorf("expected US in table, got %q (found=%v)", country, found)
	}
	if u.meta.ETag != `"abc123"` {
		t.Errorf("expected ETag to be stored, got %q", u.meta.ETag)
	}
	if u.meta.LastModified != "Thu, 01 Jan 2025 00:00:00 GMT" {
		t.Errorf("expected Last-Modified to be stored, got %q", u.meta.LastModified)
	}
}

func TestUpdater_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	u := updater{
		Ctx:      context.Background(),
		Url:      ts.URL,
		Cell:     &atomic.Pointer[bart.Table[string]]{},
		Logger:   zap.NewNop(),
		Interval: time.Hour,
		Timeout:  5 * time.Second,
	}
	err := u.update()
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestUpdater_ConditionalHeaders(t *testing.T) {
	var requestNum int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestNum++
		if requestNum == 1 {
			w.Header().Set("ETag", `"first-etag"`)
			w.Header().Set("Last-Modified", "Wed, 01 Jan 2025 00:00:00 GMT")
			w.WriteHeader(http.StatusOK)
			w.Write(makeTarGz(t, map[string][]string{"us.zone": {"1.2.3.0/24"}}))
			return
		}
		if requestNum == 2 {
			if r.Header.Get("If-None-Match") != `"first-etag"` {
				t.Errorf("expected If-None-Match header, got %q", r.Header.Get("If-None-Match"))
			}
			if r.Header.Get("If-Modified-Since") != "Wed, 01 Jan 2025 00:00:00 GMT" {
				t.Errorf("expected If-Modified-Since header, got %q", r.Header.Get("If-Modified-Since"))
			}
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}))

	cell := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:      context.Background(),
		Url:      ts.URL,
		Cell:     cell,
		Logger:   zap.NewNop(),
		Interval: time.Hour,
		Timeout:  5 * time.Second,
	}

	err := u.update()
	if err != nil {
		t.Fatalf("first update failed: %v", err)
	}

	err = u.update()
	if err != nil {
		t.Fatalf("second update failed: %v", err)
	}
}

func TestUpdater_UserAgent(t *testing.T) {
	var userAgent string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusNotModified)
	}))
	defer ts.Close()

	u := updater{
		Ctx:      context.Background(),
		Url:      ts.URL,
		Cell:     &atomic.Pointer[bart.Table[string]]{},
		Logger:   zap.NewNop(),
		Interval: time.Hour,
		Timeout:  5 * time.Second,
	}
	_ = u.update()

	if userAgent != "doors-caddy/1.0" {
		t.Errorf("expected User-Agent doors-caddy/1.0, got %q", userAgent)
	}
}

func TestUpdater_BodySizeLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		largeBody := make([]byte, maxBodySize+1024)
		w.Write(largeBody)
	}))
	defer ts.Close()

	u := updater{
		Ctx:      context.Background(),
		Url:      ts.URL,
		Cell:     &atomic.Pointer[bart.Table[string]]{},
		Logger:   zap.NewNop(),
		Interval: time.Hour,
		Timeout:  5 * time.Second,
	}
	err := u.update()
	// expect error because the truncated body is not valid gzip
	if err == nil {
		t.Fatal("expected error for oversized body (truncated to non-gzip)")
	}
}

func TestJitter(t *testing.T) {
	for range 20 {
		base := time.Second
		result := jitter(base)
		if result < base-base/10 || result > base+base/5 {
			t.Errorf("jitter(%v) = %v out of acceptable bounds", base, result)
		}
	}
}

func makeTarGz(t *testing.T, files map[string][]string) []byte {
	t.Helper()
	return makeTarGzWithDir(t, "", files)
}

func makeTarGzWithDir(t *testing.T, dir string, files map[string][]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	if dir != "" {
		hdr := &tar.Header{
			Name:     dir + "/",
			Typeflag: tar.TypeDir,
			Mode:     0755,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write dir header: %v", err)
		}
	}

	for name, lines := range files {
		body := strings.Join(lines, "\n")
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(body)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header for %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("write body for %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	return buf.Bytes()
}

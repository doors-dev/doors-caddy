package geo

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
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

func TestParseTarball_Valid(t *testing.T) {
	data := makeIPverseTarGz(t, map[string]ipverseTestFile{
		"us": {V4: []string{"1.2.3.0/24", "5.6.7.0/24"}},
		"gb": {V4: []string{"10.0.0.0/8"}, V6: []string{"2001:db8::/48"}},
	})
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball(data, v4, v6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	country, found := v4.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found || country != "US" {
		t.Errorf("expected US, got %q (found=%v)", country, found)
	}

	country, found = v4.Lookup(netip.MustParseAddr("10.0.0.1"))
	if !found || country != "GB" {
		t.Errorf("expected GB, got %q (found=%v)", country, found)
	}

	country, found = v6.Lookup(netip.MustParseAddr("2001:db8::1"))
	if !found || country != "GB" {
		t.Errorf("expected GB (v6), got %q (found=%v)", country, found)
	}
}

func TestParseTarball_SkipsNonJSON(t *testing.T) {
	data := makeIPverseTarGzWithExtra(t, map[string]ipverseTestFile{
		"us": {V4: []string{"1.2.3.0/24"}},
	}, map[string]string{
		"country/us/readme.txt": "hello",
	})
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball(data, v4, v6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := v4.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found {
		t.Error("expected US to be loaded")
	}
}

func TestParseTarball_SkipsDirs(t *testing.T) {
	data := makeIPverseTarGzWithExtra(t, map[string]ipverseTestFile{
		"us": {V4: []string{"1.2.3.0/24"}},
	}, map[string]string{})
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball(data, v4, v6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := v4.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found {
		t.Error("expected US to be loaded despite directory entries")
	}
}

func TestParseTarball_InvalidCountryCodeJSON(t *testing.T) {
	data := makeIPverseTarGzWithCustom(t, map[string]string{
		"country/usa/usa.json": `{"countryCode":"USA","prefixes":{"ipv4":["1.2.3.0/24"],"ipv6":[]}}`,
	})
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball(data, v4, v6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := v4.Lookup(netip.MustParseAddr("1.2.3.4"))
	if found {
		t.Error("usa directory should be skipped (cc path not 2 chars)")
	}
}

func TestParseTarball_BadPrefix(t *testing.T) {
	data := makeIPverseTarGzWithCustom(t, map[string]string{
		"country/us/us.json": `{"countryCode":"US","prefixes":{"ipv4":["not-a-cidr"],"ipv6":[]}}`,
	})
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball(data, v4, v6)
	if err == nil {
		t.Fatal("expected error for bad prefix")
	}
	if !strings.Contains(err.Error(), "bad ipv4 prefix") {
		t.Errorf("expected 'bad ipv4 prefix' in error, got: %v", err)
	}
}

func TestParseTarball_NotGzip(t *testing.T) {
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball([]byte("not gzip data"), v4, v6)
	if err == nil {
		t.Fatal("expected error for non-gzip data")
	}
}

func TestParseTarball_EmptyCountryCode(t *testing.T) {
	data := makeIPverseTarGzWithCustom(t, map[string]string{
		"country/us/us.json": `{"countryCode":"","prefixes":{"ipv4":["1.2.3.0/24"],"ipv6":[]}}`,
	})
	v4 := new(bart.Table[string])
	v6 := new(bart.Table[string])
	err := parseTarball(data, v4, v6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_, found := v4.Lookup(netip.MustParseAddr("1.2.3.4"))
	if found {
		t.Error("expected empty countryCode to be skipped")
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
	base2 := retryMin << 1
	if d2 < base2-base2/10 || d2 > base2+base2/10 {
		t.Errorf("retry 2 should be ~60s (+/-10%%), got %v", d2)
	}
	if u.failures != 2 {
		t.Errorf("expected failures=2 after second retry, got %d", u.failures)
	}

	d3 := u.retry()
	base3 := retryMin << 2
	if d3 < base3-base3/10 || d3 > base3+base3/10 {
		t.Errorf("retry 3 should be ~120s (+/-10%%), got %v", d3)
	}
}

func TestRetry_CapsAt1Hour(t *testing.T) {
	u := updater{failures: 20}
	delay := u.retry()
	maxWithJitter := retryMax + retryMax/10
	if delay > maxWithJitter {
		t.Errorf("retry should be capped at ~1h (+10%% jitter), got %v", delay)
	}
}

func TestRetry_JitterBounds(t *testing.T) {
	u := updater{failures: 0}
	baseDelay := retryMin
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

	v4 := &atomic.Pointer[bart.Table[string]]{}
	v6 := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:        context.Background(),
		TarballURL: ts.URL,
		V4:         v4,
		V6:         v6,
		Logger:     zap.NewNop(),
		Interval:   time.Hour,
		Timeout:    5 * time.Second,
	}
	err := u.update()
	if err != nil {
		t.Fatalf("expected nil error for 304, got: %v", err)
	}
}

func TestUpdater_200UpdatesTable(t *testing.T) {
	archive := makeIPverseTarGz(t, map[string]ipverseTestFile{
		"us": {V4: []string{"1.2.3.0/24"}},
	})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"abc123"`)
		w.Header().Set("Last-Modified", "Thu, 01 Jan 2025 00:00:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write(archive)
	}))
	defer ts.Close()

	v4Cell := &atomic.Pointer[bart.Table[string]]{}
	v6Cell := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:        context.Background(),
		TarballURL: ts.URL,
		V4:         v4Cell,
		V6:         v6Cell,
		Logger:     zap.NewNop(),
		Interval:   time.Hour,
		Timeout:    5 * time.Second,
	}
	err := u.update()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	table := v4Cell.Load()
	if table == nil {
		t.Fatal("v4 table not set after update")
	}
	country, found := table.Lookup(netip.MustParseAddr("1.2.3.4"))
	if !found || country != "US" {
		t.Errorf("expected US in v4 table, got %q (found=%v)", country, found)
	}

	v6table := v6Cell.Load()
	if v6table == nil {
		t.Fatal("v6 table not set after update")
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

	v4 := &atomic.Pointer[bart.Table[string]]{}
	v6 := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:        context.Background(),
		TarballURL: ts.URL,
		V4:         v4,
		V6:         v6,
		Logger:     zap.NewNop(),
		Interval:   time.Hour,
		Timeout:    5 * time.Second,
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
			w.Write(makeIPverseTarGz(t, map[string]ipverseTestFile{
				"us": {V4: []string{"1.2.3.0/24"}},
			}))
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

	v4 := &atomic.Pointer[bart.Table[string]]{}
	v6 := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:        context.Background(),
		TarballURL: ts.URL,
		V4:         v4,
		V6:         v6,
		Logger:     zap.NewNop(),
		Interval:   time.Hour,
		Timeout:    5 * time.Second,
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

	v4 := &atomic.Pointer[bart.Table[string]]{}
	v6 := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:        context.Background(),
		TarballURL: ts.URL,
		V4:         v4,
		V6:         v6,
		Logger:     zap.NewNop(),
		Interval:   time.Hour,
		Timeout:    5 * time.Second,
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

	v4 := &atomic.Pointer[bart.Table[string]]{}
	v6 := &atomic.Pointer[bart.Table[string]]{}
	u := updater{
		Ctx:        context.Background(),
		TarballURL: ts.URL,
		V4:         v4,
		V6:         v6,
		Logger:     zap.NewNop(),
		Interval:   time.Hour,
		Timeout:    5 * time.Second,
	}
	err := u.update()
	if err == nil {
		t.Fatal("expected error for oversized body (truncated to non-gzip)")
	}
}

func TestExtractCountry(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		country string
		ok      bool
	}{
		{"valid", "repo-branch/country/us/us.json", "us", true},
		{"valid nested", "deep/path/repo-branch/country/gb/gb.json", "gb", true},
		{"wrong dir", "repo-branch/data/us/us.json", "", false},
		{"bad cc length", "repo-branch/country/usa/usa.json", "", false},
		{"name mismatch", "repo-branch/country/us/gb.json", "", false},
		{"not json", "repo-branch/country/us/us.txt", "", false},
		{"too shallow", "country/us/us.json", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			country, ok := extractCountry(tt.path)
			if ok != tt.ok {
				t.Errorf("expected ok=%v, got ok=%v", tt.ok, ok)
			}
			if country != tt.country {
				t.Errorf("expected country=%q, got %q", tt.country, country)
			}
		})
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

type ipverseTestFile struct {
	V4 []string
	V6 []string
}

func makeIPverseTarGz(t *testing.T, files map[string]ipverseTestFile) []byte {
	t.Helper()
	return makeIPverseTarGzWithExtra(t, files, nil)
}

func makeIPverseTarGzWithExtra(t *testing.T, files map[string]ipverseTestFile, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	prefix := "pack-aaa111/"
	for cc, data := range files {
		hdr := &tar.Header{
			Name:     prefix + fmt.Sprintf("country/%s/", cc),
			Typeflag: tar.TypeDir,
			Mode:     0755,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write dir header for %s: %v", cc, err)
		}

		jsonBody := fmt.Sprintf(`{"countryCode":"%s","prefixes":{"ipv4":%s,"ipv6":%s}}`,
			strings.ToUpper(cc),
			stringSliceToJSON(data.V4),
			stringSliceToJSON(data.V6),
		)
		hdr = &tar.Header{
			Name: prefix + fmt.Sprintf("country/%s/%s.json", cc, cc),
			Size: int64(len(jsonBody)),
			Mode: 0644,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header for %s: %v", cc, err)
		}
		if _, err := tw.Write([]byte(jsonBody)); err != nil {
			t.Fatalf("write body for %s: %v", cc, err)
		}
	}

	for name, body := range extra {
		hdr := &tar.Header{
			Name: prefix + name,
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

func makeIPverseTarGzWithCustom(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	prefix := "pack-bbb222/"
	for name, body := range files {
		hdr := &tar.Header{
			Name: prefix + name,
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

func stringSliceToJSON(s []string) string {
	if s == nil {
		return "[]"
	}
	quoted := make([]string, len(s))
	for i, v := range s {
		quoted[i] = fmt.Sprintf(`"%s"`, v)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

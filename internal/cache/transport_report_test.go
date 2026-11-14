package cache

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"setup-go-faster/internal/cachebackend"
)

type privateDNSTransport struct{}

func (privateDNSTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.DNSError{Name: "private-host.invalid", Server: "private-proxy", Err: "private-token"}
}

func TestBackendTransportCategoryIsSafeInUnredactedReport(t *testing.T) {
	c := DefaultConfig()
	c.Strict = true
	s := state(t, c, "deps", roots(t))
	b := &cachebackend.Client{BaseURL: "https://private-host.invalid/?sig=private-token", Token: "private-token", HTTPClient: &http.Client{Transport: privateDNSTransport{}}}
	r, err := Restore(context.Background(), b, &s)
	if !errors.Is(err, cachebackend.ErrDNS) || len(r.Snapshots) == 0 || r.Snapshots[0].Error != "cache service request: DNS lookup failed" {
		t.Fatalf("lost safe category: %+v %v", r, err)
	}
	path := filepath.Join(t.TempDir(), "report.json")
	if err := writeReport(path, r); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"private-token", "private-host", "private-proxy", "sig="} {
		if strings.Contains(string(data), private) {
			t.Fatalf("unredacted report retained transport detail: %s", data)
		}
	}
}

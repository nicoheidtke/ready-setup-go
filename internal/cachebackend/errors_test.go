package cachebackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestTransportCategoriesDiscardRawCause(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		want  error
	}{
		{"dns", &net.DNSError{Name: secret, Server: secret, Err: secret}, ErrDNS},
		{"dns-timeout", &net.DNSError{Name: secret, Err: secret, IsTimeout: true}, context.DeadlineExceeded},
		{"tls-verification", &tls.CertificateVerificationError{Err: errors.New(secret)}, ErrTLS},
		{"tls-record", tls.RecordHeaderError{Msg: secret}, ErrTLS},
		{"tls-hostname", x509.HostnameError{Host: secret}, ErrTLS},
		{"tls-authority", x509.UnknownAuthorityError{}, ErrTLS},
		{"tls-invalid", x509.CertificateInvalidError{Detail: secret}, ErrTLS},
		{"connection", &net.OpError{Op: secret, Net: secret, Err: errors.New(secret)}, ErrConnection},
		{"refused", syscall.ECONNREFUSED, ErrConnection},
		{"reset", syscall.ECONNRESET, ErrConnection},
		{"pipe", syscall.EPIPE, ErrConnection},
		{"cancelled", context.Canceled, context.Canceled},
		{"deadline", context.DeadlineExceeded, context.DeadlineExceeded},
		{"unclassified", errors.New("TLS timeout DNS " + secret), nil},
		{"body-eof", io.ErrUnexpectedEOF, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cause := &url.Error{Op: secret, URL: "https://private.invalid/?sig=" + secret, Err: fmt.Errorf("%s: %w", secret, tc.cause)}
			err := transferError(context.Background(), "download", cause)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("lost safe category: %v", err)
			}
			if tc.want == nil && err.Error() != "cache download failed" {
				t.Fatalf("classified untrusted text: %v", err)
			}
			if errors.Is(err, cause) || errors.Is(err, tc.cause) && tc.want != context.Canceled && tc.want != context.DeadlineExceeded {
				t.Fatal("retained unsafe transport cause")
			}
			// Reports store Error() text without presentation redaction. Inspect
			// both that JSON and every retained error, including debug formatting.
			data, marshalErr := json.Marshal(map[string]string{"error": err.Error()})
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for current := err; current != nil; current = errors.Unwrap(current) {
				data = append(data, fmt.Sprintf("%+v %#v", current, current)...)
			}
			for _, private := range []string{secret, "private.invalid", "https://", "sig="} {
				if strings.Contains(string(data), private) {
					t.Fatalf("unsafe returned error: %s", data)
				}
			}
		})
	}
}

func TestContextCancellationOverridesTransportCategory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := transferError(ctx, "upload", &net.DNSError{Name: secret})
	if !errors.Is(err, context.Canceled) || errors.Is(err, ErrDNS) {
		t.Fatalf("lost caller cancellation: %v", err)
	}
}

func TestRequestsExposeOnlySafeTransportCategory(t *testing.T) {
	c := Client{BaseURL: "https://private.invalid", Token: secret, HTTPClient: &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return nil, &tls.CertificateVerificationError{Err: errors.New(secret)}
	})}}
	_, lookup := c.Lookup(context.Background(), "key", "version", nil)
	_, download := c.Download(context.Background(), Entry{URL: "https://private.invalid/?sig=" + secret}, io.Discard)
	upload := c.put(context.Background(), mustURL(t, "https://private.invalid/?sig="+secret), strings.NewReader("x"), 1, "application/octet-stream", true)
	for _, err := range []error{lookup, download, upload} {
		if !errors.Is(err, ErrTLS) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private.invalid") {
			t.Fatalf("lost safe request category: %v", err)
		}
	}
}

type privateErrorBody struct{}

func (privateErrorBody) Read([]byte) (int, error) {
	return 0, &net.DNSError{Name: secret, Server: secret, Err: secret}
}
func (privateErrorBody) Close() error { return nil }

func TestResponseBodiesExposeOnlySafeTransportCategory(t *testing.T) {
	c := Client{BaseURL: "https://private.invalid", Token: secret, HTTPClient: &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		status := http.StatusOK
		if r.Method == http.MethodPut {
			status = http.StatusCreated
		}
		return &http.Response{StatusCode: status, ContentLength: 1, Header: make(http.Header), Body: privateErrorBody{}}, nil
	})}}
	_, lookup := c.Lookup(context.Background(), "key", "version", nil)
	_, download := c.Download(context.Background(), Entry{URL: "https://private.invalid/?sig=" + secret}, io.Discard)
	upload := c.put(context.Background(), mustURL(t, "https://private.invalid/?sig="+secret), strings.NewReader("x"), 1, "application/octet-stream", true)
	_, ranged := consumeRange(context.Background(), &http.Response{StatusCode: http.StatusPartialContent, ContentLength: 1, Header: http.Header{"Content-Range": {"bytes 0-0/1"}}, Body: privateErrorBody{}}, io.Discard, 0, 0, 1, "")
	for _, err := range []error{lookup, download, upload, ranged} {
		if !errors.Is(err, ErrDNS) || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private.invalid") {
			t.Fatalf("lost safe body category: %v", err)
		}
	}
}

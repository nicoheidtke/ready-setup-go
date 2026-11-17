package cachebackend

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"syscall"
)

// These identities contain only fixed text. Never retain a raw transport cause:
// even unwrapping a net/url error can expose signed URLs, hosts or credentials.
var (
	ErrDNS        = errors.New("DNS lookup failed")
	ErrTLS        = errors.New("TLS handshake or certificate verification failed")
	ErrConnection = errors.New("network connection failed")
)

// transferError classifies types/identities, never error strings. The fixed
// operation names at call sites and allowlisted causes are also safe for the
// existing machine-readable report error fields; no raw cause escapes here.
func transferError(ctx context.Context, operation string, cause error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cache %s: %w", operation, err)
	}
	var safe error
	var dns *net.DNSError
	var certificate *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var unknownAuthority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalidCertificate x509.CertificateInvalidError
	var connection *net.OpError
	switch {
	case errors.Is(cause, context.Canceled):
		safe = context.Canceled
	case errors.Is(cause, context.DeadlineExceeded), transportTimeout(cause):
		safe = context.DeadlineExceeded
	case errors.As(cause, &dns):
		safe = ErrDNS
	case errors.As(cause, &certificate), errors.As(cause, &record), errors.As(cause, &unknownAuthority), errors.As(cause, &hostname), errors.As(cause, &invalidCertificate):
		safe = ErrTLS
	case errors.As(cause, &connection), errors.Is(cause, syscall.ECONNRESET), errors.Is(cause, syscall.ECONNREFUSED), errors.Is(cause, syscall.EPIPE):
		safe = ErrConnection
	}
	if safe != nil {
		return fmt.Errorf("cache %s: %w", operation, safe)
	}
	return fmt.Errorf("cache %s failed", operation)
}

func transportTimeout(err error) bool {
	// A url.Error may report false when a formatting wrapper separates it
	// from the underlying net.Error. Inspect each single-cause wrapper.
	for err != nil {
		if network, ok := err.(net.Error); ok && network.Timeout() {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

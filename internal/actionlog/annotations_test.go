package actionlog

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestNativeAnnotationsEscapeAndRedact(t *testing.T) {
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "private-runtime-token")
	var out bytes.Buffer
	l := New(&out, true)
	l.Warning("Cache,:\n::error::", "failed%\n::error::private-runtime-token https://blob.test/cache?sig=private-signature\x1b[31m")
	l.Error("Set up Go", "invalid input")
	got := out.String()
	if !strings.HasPrefix(got, "::warning title=Cache%2C%3A ") || !strings.Contains(got, "failed%25%0A::error::") || strings.Count(got, "\n") != 2 || !strings.Contains(got, "::error title=Set up Go::invalid input") {
		t.Fatalf("unsafe or missing annotations: %q", got)
	}
	for _, secret := range []string{"private-runtime-token", "private-signature", "https://", "\x1b"} {
		if strings.Contains(got, secret) {
			t.Fatalf("unsafe diagnostic %q: %q", secret, got)
		}
	}
}

func TestPlainPrintfCannotEmitCommandsOrUntrustedANSI(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "private-github-token")
	t.Setenv("NO_COLOR", "")
	var out bytes.Buffer
	l := New(&out, true)
	l.Printf("%s\n", "::error::fake\r\n::endgroup::\x1b[31mprivate-github-token https://signed.test/?sig=signature")
	l.Printf("failure: %s\n", errors.New("bad\x1b]8;;https://evil.test\x07text\x1b]8;;\x07\n::warning::bad"))
	l.Printf("%s\n", l.Status("ERROR"))
	got := out.String()
	if strings.HasPrefix(got, "::") || strings.Contains(got, "\n::") || strings.Count(got, "\n") != 3 || strings.ContainsAny(got, "\r\x07") || strings.Count(got, "\x1b") != 2 || !strings.HasSuffix(got, "\x1b[31mERROR\x1b[0m\n") {
		t.Fatalf("unsafe plain logs or stripped trusted color: %q", got)
	}
	for _, secret := range []string{"private-github-token", "signature", "https://", "\x1b]"} {
		if strings.Contains(got, secret) {
			t.Fatalf("unsafe diagnostic %q: %q", secret, got)
		}
	}
}

func TestStandaloneAnnotationsArePlain(t *testing.T) {
	var out bytes.Buffer
	l := New(&out, false)
	l.Warning("Go cache", "offline\n::error::bad")
	l.Error("Set up Go", "invalid")
	if got := out.String(); got != "WARNING Go cache: offline ::error::bad\nERROR Set up Go: invalid\n" {
		t.Fatal(got)
	}
}

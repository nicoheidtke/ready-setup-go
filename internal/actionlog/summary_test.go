package actionlog

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummaryEscapesUntrustedCellsAndRedactsCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	if err := os.WriteFile(path, []byte("previous section\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	t.Setenv("ACTIONS_RUNTIME_TOKEN", "private-token")
	New(io.Discard, true).Summary(false, "Go", []string{"Result", "Reason"}, []SummaryRow{{"ERROR", "<script>|\n[click](https://example.test/blob?sig=secret) private-token `*bad*`"}})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, "previous section\n### Go\n") || !strings.Contains(got, "&lt;script&gt;&#124;") || !strings.Contains(got, "&#91;click&#93;") || !strings.Contains(got, "&#96;&#42;bad&#42;&#96;") {
		t.Fatalf("unsafe summary structure: %s", got)
	}
	for _, secret := range []string{"private-token", "https://", "sig=secret", "<script>"} {
		if strings.Contains(got, secret) {
			t.Fatalf("unescaped or secret text %q in summary", secret)
		}
	}
}

func TestSummaryOptionalAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "summary")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	for _, tc := range []struct{ actions, disabled bool }{{true, true}, {false, false}} {
		New(io.Discard, tc.actions).Summary(tc.disabled, "Go", nil, nil)
	}
	if st, _ := os.Stat(path); st.Size() != 0 {
		t.Fatal("opt-out or standalone wrote summary")
	}
	l := New(io.Discard, true)
	l.Summary(false, "Go", []string{"Reason"}, []SummaryRow{{strings.Repeat("<|", 100000)}})
	if st, _ := os.Stat(path); st.Size() > 10000 {
		t.Fatal("untrusted cell was not bounded")
	}
	full := strings.Repeat("x", 256<<10)
	if err := os.WriteFile(path, []byte(full), 0600); err != nil {
		t.Fatal(err)
	}
	l.Summary(false, "Go", nil, nil)
	if b, _ := os.ReadFile(path); string(b) != full {
		t.Fatal("summary size cap changed existing content")
	}
	// Directories exercise an unwritable runner file without platform-specific chmod.
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		t.Setenv("GITHUB_STEP_SUMMARY", path)
		l.Summary(false, "Go", nil, nil)
	}
}

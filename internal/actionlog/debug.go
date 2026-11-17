package actionlog

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DebugEnabled follows the runner's runtime signal, also used by core.isDebug.
// ACTIONS_STEP_DEBUG configures this signal; ACTIONS_RUNNER_DEBUG is a different
// facility for the runner's own diagnostic archive.
func (l *Logger) DebugEnabled() bool {
	return l.actions && os.Getenv("RUNNER_DEBUG") == "1"
}

var debugURL = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
var ansi = regexp.MustCompile("\x1b(?:\\[[0-?]*[ -/]*[@-~]|\\][^\x07\x1b]*(?:\x07|\x1b\\\\))")

// debugText is defense in depth for diagnostic errors. Callers must still pass
// curated data, never request/response objects, credentials or environment maps.
func debugText(s string) string {
	for _, name := range []string{"ACTIONS_RUNTIME_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"} {
		if secret := os.Getenv(name); secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	s = debugURL.ReplaceAllString(s, "[redacted URL]")
	s = ansi.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if (r < 32 && r != '\r' && r != '\n') || r == 127 || (r >= 128 && r <= 159) {
			return ' '
		}
		return r
	}, s)
}

// Text renders untrusted diagnostic data on a single inert line. It is also
// used by annotations and summaries; JSON report files remain machine data.
func Text(s string) string {
	return strings.NewReplacer("\r", " ", "\n", " ").Replace(debugText(s))
}

func (l *Logger) Debugf(format string, args ...any) {
	if !l.DebugEnabled() {
		return
	}
	l.command("debug", "", debugText(fmt.Sprintf(format, args...)))
}

// DebugJSON skips serialization entirely when debug is off. These optional
// diagnostics never change the operation's result or the JSON report file.
func (l *Logger) DebugJSON(label string, value any) {
	if !l.DebugEnabled() {
		return
	}
	b, err := json.Marshal(value)
	if err != nil {
		l.Debugf("%s: diagnostic serialization failed", label)
		return
	}
	l.Debugf("%s: %s", label, b)
}

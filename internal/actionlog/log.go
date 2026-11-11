// Package actionlog provides serialized GitHub workflow logging without a runtime
// toolkit dependency. Diagnostics stay on stderr so CLI JSON stdout stays clean.
package actionlog

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

type Logger struct {
	mu      sync.Mutex
	out     io.Writer
	actions bool
	color   bool
}

func New(out io.Writer, actions bool) *Logger {
	return &Logger{out: out, actions: actions, color: actions && os.Getenv("NO_COLOR") == ""}
}

// Status colors only known result labels. Plain text still carries the meaning;
// ordinary misses/skips stay neutral. Native workflow annotations supply their
// own warning/error styling and must never be wrapped in ANSI sequences.
type statusText string

func (l *Logger) Status(label string) statusText {
	if !l.color {
		return statusText(Text(label))
	}
	code := ""
	switch label {
	case "READY", "HIT", "SAVED":
		code = "32"
	case "WARNING":
		code = "33"
	case "ERROR":
		code = "31"
	}
	if code == "" {
		return statusText(Text(label))
	}
	return statusText("\x1b[" + code + "m" + label + "\x1b[0m")
}

var Default = New(os.Stderr, os.Getenv("GITHUB_ACTIONS") == "true")

// Escape makes workflow-command data a single inert command payload.
func Escape(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// Printf emits one complete diagnostic under the same lock as stage commands.
func (l *Logger) Printf(format string, args ...any) {
	clean := make([]any, len(args))
	for i, arg := range args {
		switch v := arg.(type) {
		case statusText: // Only Status can construct intentionally colored text.
			clean[i] = string(v)
		case string:
			clean[i] = Text(v)
		case error:
			clean[i] = Text(v.Error())
		case fmt.Stringer:
			clean[i] = Text(v.String())
		default:
			clean[i] = arg
		}
	}
	message := fmt.Sprintf(format, clean...)
	// Even a caller using a bare %s format cannot turn data into a command.
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimLeft(line, " \t"), "::") {
			lines[i] = "setup-go-better: " + line
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprint(l.out, strings.Join(lines, "\n"))
}

// Stage returns an idempotent close function. Callers own stage lifetimes and
// must not open groups from concurrent workers: GitHub groups are a flat stream.
func (l *Logger) Stage(title string) func() {
	l.mu.Lock()
	if l.actions {
		fmt.Fprintln(l.out, "::group::"+Escape(debugText(title)))
	}
	l.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.actions {
				fmt.Fprintln(l.out, "::endgroup::")
			}
		})
	}
}

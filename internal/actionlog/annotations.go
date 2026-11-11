package actionlog

import (
	"fmt"
	"strings"
)

// command is the sole native annotation serializer. General Printf treats
// command-looking content as inert text instead.
func (l *Logger) command(kind, title, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	properties := ""
	if title != "" {
		properties = " title=" + strings.NewReplacer(":", "%3A", ",", "%2C").Replace(Escape(Text(title)))
	}
	fmt.Fprintf(l.out, "::%s%s::%s\n", kind, properties, Escape(debugText(message)))
}

func (l *Logger) Warning(title, message string) {
	if l.actions {
		l.command("warning", title, message)
		return
	}
	l.Printf("WARNING %s: %s\n", title, message)
}

func (l *Logger) Error(title, message string) {
	if l.actions {
		l.command("error", title, message)
		return
	}
	l.Printf("ERROR %s: %s\n", title, message)
}

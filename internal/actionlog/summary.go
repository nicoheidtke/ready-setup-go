package actionlog

import (
	"fmt"
	"html"
	"os"
	"strings"
)

// SummaryRow is a table row. Every cell is treated as untrusted text.
type SummaryRow []string

// SummaryCell keeps untrusted text out of Markdown/HTML structure. Summaries
// intentionally omit paths, cache keys and URLs; redaction is defense in depth.
func SummaryCell(s string) string {
	s = Text(s)
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
	runes := []rune(s)
	if len(runes) > 400 {
		s = string(runes[:400]) + "…"
	}
	s = html.EscapeString(s)
	return strings.NewReplacer("\\", "&#92;", "|", "&#124;", "`", "&#96;", "*", "&#42;", "_", "&#95;", "[", "&#91;", "]", "&#93;", "!", "&#33;", "~", "&#126;").Replace(s)
}

// Summary appends a bounded section to this step's runner-owned summary file.
// Post-job calls append to the post step's file, never to a previous step's
// uploaded summary. Presentation failures never alter setup/cache success.
func (l *Logger) Summary(disabled bool, title string, headers []string, rows []SummaryRow, notes ...string) {
	if disabled || !l.actions || os.Getenv("GITHUB_STEP_SUMMARY") == "" {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### %s\n\n", SummaryCell(title))
	if len(headers) > 0 {
		writeRow := func(cells []string) {
			b.WriteString("| ")
			for _, cell := range cells {
				b.WriteString(SummaryCell(cell))
				b.WriteString(" | ")
			}
			b.WriteByte('\n')
		}
		writeRow(headers)
		b.WriteString("|" + strings.Repeat(" --- |", len(headers)) + "\n")
		for i, row := range rows {
			if i == 16 {
				break
			}
			writeRow(row)
		}
		b.WriteByte('\n')
	}
	for i, note := range notes {
		if i == 8 {
			break
		}
		b.WriteString(SummaryCell(note) + "\n\n")
	}
	if b.Len() > 48<<10 {
		l.Debugf("job summary omitted: section size limit")
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(os.Getenv("GITHUB_STEP_SUMMARY"), os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		// Do not include a runner path or underlying error in presentation logs.
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size()+int64(b.Len()) > 256<<10 {
		return
	}
	_, _ = f.WriteString(b.String())
}

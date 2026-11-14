package installer

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/scanner"
)

// ResolveVersionInput gives an explicit selector priority without touching the
// file. Relative action inputs are based on the workspace; standalone inputs use
// the current directory. It does not run Go or fetch release metadata.
func ResolveVersionInput(version, file, workspace string) (string, error) {
	version, file = strings.TrimSpace(version), strings.TrimSpace(file)
	if version != "" {
		return version, nil
	}
	if file == "" {
		return "", fmt.Errorf("go-version or go-version-file is required")
	}
	if !filepath.IsAbs(file) && workspace != "" {
		file = filepath.Join(workspace, file)
	}
	f, err := os.Open(file)
	if err != nil {
		return "", fmt.Errorf("go-version-file: %w", err)
	}
	defer f.Close()
	const maxFileBytes = 1 << 20
	b, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("go-version-file: %w", err)
	}
	if len(b) > maxFileBytes {
		return "", fmt.Errorf("go-version-file exceeds 1 MiB")
	}
	version, err = parseVersionFile(string(b))
	if err != nil {
		return "", fmt.Errorf("go-version-file: %w", err)
	}
	return version, nil
}

// Parse only top-level go/toolchain directives, leaving unrelated module syntax
// to Go. The standard-library lexer keeps comments and quoted module paths from
// masquerading as directives without adding a module parser dependency.
func parseVersionFile(contents string) (string, error) {
	var lex scanner.Scanner
	lex.Init(strings.NewReader(contents))
	lex.Mode = scanner.ScanIdents | scanner.ScanStrings | scanner.ScanRawStrings | scanner.ScanComments | scanner.SkipComments
	lex.Whitespace &^= 1 << '\n'
	var scanErr error
	lex.Error = func(_ *scanner.Scanner, message string) { scanErr = fmt.Errorf("invalid file: %s", message) }
	var fields []string
	values := map[string]string{}
	depth, previousEnd := 0, -1
	flush := func() error {
		if len(fields) > 0 && depth == 0 && (fields[0] == "go" || fields[0] == "toolchain") {
			name := fields[0]
			if len(fields) != 2 || values[name] != "" {
				return fmt.Errorf("invalid or duplicate %s directive", name)
			}
			value := fields[1]
			if strings.HasPrefix(value, "\"") || strings.HasPrefix(value, "`") {
				var err error
				value, err = strconv.Unquote(value)
				if err != nil {
					return fmt.Errorf("invalid %s directive", name)
				}
			}
			values[name] = value
		}
		fields = nil
		previousEnd = -1
		return nil
	}
	for token := lex.Scan(); token != scanner.EOF; token = lex.Scan() {
		if token == '\n' {
			if err := flush(); err != nil {
				return "", err
			}
			continue
		}
		text := lex.TokenText()
		// Parentheses in quoted tokens are not block delimiters.
		if token == '(' {
			depth++
		} else if token == ')' {
			depth--
			if depth < 0 {
				return "", fmt.Errorf("unbalanced directive block")
			}
		}
		if len(fields) > 0 && lex.Position.Offset == previousEnd {
			fields[len(fields)-1] += text
		} else {
			fields = append(fields, text)
		}
		previousEnd = lex.Position.Offset + len(text)
	}
	if err := flush(); err != nil {
		return "", err
	}
	if scanErr != nil {
		return "", scanErr
	}
	if depth != 0 {
		return "", fmt.Errorf("unbalanced directive block")
	}
	goVersion := values["go"]
	if goVersion == "" {
		return "", fmt.Errorf("missing go directive")
	}
	minimum, err := parseSelector(goVersion, 0)
	if err != nil || strings.HasSuffix(goVersion, ".x") {
		return "", fmt.Errorf("unsupported go directive %q: require a release version", goVersion)
	}
	toolchain := values["toolchain"]
	if toolchain == "" || toolchain == "default" {
		if _, err := ParseSelector(goVersion); err != nil {
			return "", fmt.Errorf("unsupported go directive %q: select a Go 1.26+ toolchain", goVersion)
		}
		return goVersion, nil
	}
	if !strings.HasPrefix(toolchain, "go") {
		return "", fmt.Errorf("unsupported toolchain directive %q", toolchain)
	}
	preferred := strings.TrimPrefix(toolchain, "go")
	selected, err := ParseSelector(preferred)
	if err != nil || !selected.Exact {
		return "", fmt.Errorf("unsupported toolchain directive %q: require an exact Go 1.26+ release", toolchain)
	}
	if selected.Version.Less(minimum.Version) {
		return "", fmt.Errorf("toolchain directive %q is older than go %s", toolchain, goVersion)
	}
	return preferred, nil
}

// check-docs validates local documentation links and action reference tables.
// It intentionally supports the repository's Markdown and action.yml subset,
// without network requests, Git, or third-party parsers.
package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

var (
	inlineLink   = regexp.MustCompile(`!?\[[^\]\n]*\]\(\s*(<[^>]*>|[^\s)]+)(?:\s+"[^"]*")?\s*\)`)
	referenceDef = regexp.MustCompile(`^\s{0,3}\[([^\]]+)\]:\s*(<[^>]*>|\S+)`)
	referenceUse = regexp.MustCompile(`!?\[([^\]\n]+)\]\[([^\]\n]*)\]`)
	heading      = regexp.MustCompile(`^ {0,3}#{1,6}\s+(.+?)\s*$`)
	metadataName = regexp.MustCompile(`^  ([a-z][a-z0-9-]*):\s*$`)
)

func main() {
	if len(os.Args) > 1 {
		if len(os.Args) == 2 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
			fmt.Println("Usage: go run ./cmd/check-docs\n\nChecks source Markdown local links/ATX heading anchors, explicit reference links,\nCLAUDE.md imports, action input/default/output tables, and recommended distribution\nexample pins. Run from repository root. Link checks skip external URLs and fenced\ncode; distribution checks include examples. No network requests or Git dependency.")
			return
		}
		fmt.Fprintln(os.Stderr, "check-docs takes no arguments; use --help")
		os.Exit(2)
	}
	problems := checkRepository(".")
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	if len(problems) != 0 {
		os.Exit(1)
	}
	fmt.Println("Documentation links and action reference passed.")
}

func sourceMarkdown(root string) ([]string, error) {
	var names []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == "." {
				return nil
			}
			first := strings.Split(filepath.ToSlash(rel), "/")[0]
			if first != "docs" && first != ".github" && first != "internal" {
				return filepath.SkipDir
			}
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".md") {
			names = append(names, path)
		}
		return nil
	})
	slices.Sort(names)
	return names, err
}

// prose removes fenced blocks and inline code so example syntax is not a link.
// ATX headings and explicit reference links are the documented Markdown subset.
func prose(text string) []string {
	var lines []string
	var fence byte
	var width int
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trim := strings.TrimSpace(line)
		if len(trim) >= 3 && (trim[0] == '`' || trim[0] == '~') {
			n := 0
			for n < len(trim) && trim[n] == trim[0] {
				n++
			}
			if n >= 3 {
				if fence == 0 {
					fence, width = trim[0], n
				} else if fence == trim[0] && n >= width && strings.TrimSpace(trim[n:]) == "" {
					fence = 0
				}
				lines = append(lines, "")
				continue
			}
		}
		if fence != 0 {
			lines = append(lines, "")
			continue
		}
		var b strings.Builder
		for i := 0; i < len(line); {
			if line[i] != '`' {
				b.WriteByte(line[i])
				i++
				continue
			}
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			marker := line[i:j]
			end := strings.Index(line[j:], marker)
			if end < 0 {
				b.WriteString(line[i:])
				break
			}
			// Preserve code text for heading IDs while suppressing link-shaped syntax.
			code := line[j : j+end]
			b.WriteString(strings.NewReplacer("[", "", "]", "").Replace(code))
			i = j + end + len(marker)
		}
		lines = append(lines, b.String())
	}
	return lines
}

func headingIDs(text string) map[string]bool {
	ids, used := map[string]bool{}, map[string]int{}
	for _, line := range prose(text) {
		match := heading.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		title := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(match[1]), "#"))
		var b strings.Builder
		for _, ch := range strings.ToLower(title) {
			if unicode.IsLetter(ch) || unicode.IsNumber(ch) || ch == '_' || ch == '-' {
				b.WriteRune(ch)
			} else if ch == ' ' {
				b.WriteByte('-')
			}
		}
		slug := b.String()
		if used[slug] != 0 {
			ids[fmt.Sprintf("%s-%d", slug, used[slug])] = true
		} else {
			ids[slug] = true
		}
		used[slug]++
	}
	return ids
}

func checkLinks(root, file, text string) []string {
	var problems []string
	refs := map[string]string{}
	lines := prose(text)
	for _, line := range lines {
		if m := referenceDef.FindStringSubmatch(line); m != nil {
			refs[strings.ToLower(strings.TrimSpace(m[1]))] = m[2]
		}
	}
	check := func(target string, line int) {
		target = strings.Trim(target, "<>")
		parsed, err := url.Parse(target)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: invalid link %q", file, line, target))
			return
		}
		if parsed.IsAbs() || parsed.Host != "" {
			return
		}
		dest := filepath.Join(filepath.Dir(file), filepath.FromSlash(parsed.Path))
		if parsed.Path == "" {
			dest = file
		} else if strings.HasPrefix(parsed.Path, "/") {
			dest = filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(parsed.Path, "/")))
		}
		absRoot, _ := filepath.Abs(root)
		absDest, _ := filepath.Abs(dest)
		rel, err := filepath.Rel(absRoot, absDest)
		if err != nil || !filepath.IsLocal(rel) {
			problems = append(problems, fmt.Sprintf("%s:%d: link escapes repository: %s", file, line, target))
			return
		}
		stat, err := os.Stat(dest)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: missing link target: %s", file, line, target))
			return
		}
		if parsed.Fragment != "" && strings.EqualFold(filepath.Ext(dest), ".md") && !stat.IsDir() {
			data, err := os.ReadFile(dest)
			if err != nil {
				problems = append(problems, err.Error())
				return
			}
			if !headingIDs(string(data))[parsed.Fragment] {
				problems = append(problems, fmt.Sprintf("%s:%d: missing heading: %s", file, line, target))
			}
		}
	}
	for i, line := range lines {
		if m := referenceDef.FindStringSubmatch(line); m != nil {
			check(m[2], i+1)
			continue
		}
		for _, m := range inlineLink.FindAllStringSubmatch(line, -1) {
			check(m[1], i+1)
		}
		for _, m := range referenceUse.FindAllStringSubmatch(line, -1) {
			key := m[2]
			if key == "" {
				key = m[1]
			}
			if _, ok := refs[strings.ToLower(strings.TrimSpace(key))]; !ok {
				problems = append(problems, fmt.Sprintf("%s:%d: undefined link reference: %s", file, i+1, key))
			}
		}
	}
	return problems
}

type actionReference struct {
	inputs  map[string]string
	outputs map[string]string
}

func readAction(text string) (actionReference, error) {
	ref := actionReference{map[string]string{}, map[string]string{}}
	section, name := "", ""
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			section = strings.TrimSuffix(line, ":")
			name = ""
			continue
		}
		if section != "inputs" && section != "outputs" {
			continue
		}
		if m := metadataName.FindStringSubmatch(line); m != nil {
			name = m[1]
			dest := ref.inputs
			if section == "outputs" {
				dest = ref.outputs
			}
			if _, ok := dest[name]; ok {
				return ref, fmt.Errorf("duplicate action %s name %s", section, name)
			}
			dest[name] = ""
		} else if strings.HasPrefix(line, "    default:") && section == "inputs" {
			if name == "" {
				return ref, fmt.Errorf("default without input name")
			}
			value := strings.TrimSpace(strings.TrimPrefix(line, "    default:"))
			if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
				value = strings.ReplaceAll(value[1:len(value)-1], "''", "'")
			} else if strings.HasPrefix(value, `"`) {
				decoded, err := strconv.Unquote(value)
				if err != nil {
					return ref, err
				}
				value = decoded
			} else if strings.ContainsAny(value, "'#|>\t ") {
				return ref, fmt.Errorf("unsupported action default for %s: %q", name, value)
			}
			ref.inputs[name] = value
		}
	}
	if len(ref.inputs) == 0 || len(ref.outputs) == 0 {
		return ref, fmt.Errorf("action metadata must contain inputs and outputs")
	}
	return ref, scanner.Err()
}

func readTables(text string) (actionReference, error) {
	ref := actionReference{map[string]string{}, map[string]string{}}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			section = strings.TrimSpace(strings.TrimPrefix(line, "## "))
		}
		if section != "Inputs" && section != "Outputs and environment" {
			continue
		}
		cols := strings.Split(strings.TrimSpace(line), "|")
		if len(cols) < 4 {
			continue
		}
		name := strings.TrimSpace(cols[1])
		if !strings.HasPrefix(name, "`") || !strings.HasSuffix(name, "`") {
			continue
		}
		name = strings.Trim(name, "`")
		dest := ref.inputs
		if section != "Inputs" {
			dest = ref.outputs
		}
		if _, ok := dest[name]; ok {
			return ref, fmt.Errorf("duplicate documented %s name %s", section, name)
		}
		value := strings.TrimSpace(cols[2])
		value = strings.Trim(value, "`")
		// These prose labels denote the empty metadata default's documented behavior.
		if value == "Empty" || name == "cache-dependency-path" && value == "Automatic scan" {
			value = ""
		}
		dest[name] = value
	}
	return ref, nil
}

func checkReference(action, usage string) []string {
	var problems []string
	actual, err := readAction(action)
	if err != nil {
		return []string{"action.yml: " + err.Error()}
	}
	documented, err := readTables(usage)
	if err != nil {
		return []string{"docs/USAGE.md: " + err.Error()}
	}
	for _, section := range []string{"inputs", "outputs"} {
		source, doc := actual.inputs, documented.inputs
		if section == "outputs" {
			source, doc = actual.outputs, documented.outputs
		}
		for name, value := range source {
			got, ok := doc[name]
			if !ok {
				problems = append(problems, fmt.Sprintf("docs/USAGE.md: undocumented %s name %s", section, name))
			} else if section == "inputs" && got != value {
				problems = append(problems, fmt.Sprintf("docs/USAGE.md: default for %s is %q, action.yml has %q", name, got, value))
			}
		}
		for name := range doc {
			if _, ok := source[name]; !ok {
				problems = append(problems, fmt.Sprintf("docs/USAGE.md: unknown %s name %s", section, name))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// The usage guide owns the recommended major alias. Release notes carry exact
// source/distribution provenance, avoiding a documentation commit per release.
func checkDistributionExamples(documents map[string]string) []string {
	major := regexp.MustCompile("Examples use the `([^`]+)` major version tag\\.").FindStringSubmatch(documents["docs/USAGE.md"])
	if major == nil || !regexp.MustCompile(`^v[1-9][0-9]*$`).MatchString(major[1]) {
		return []string{"docs/USAGE.md: require recommended major version tag"}
	}
	uses := regexp.MustCompile(`nicoheidtke/setup-go-faster@([^\s'"` + "`" + `]+)`)
	sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
	exact := regexp.MustCompile(`^` + major[1] + `\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	var problems []string
	for file, contents := range documents {
		for i, line := range strings.Split(contents, "\n") {
			for _, match := range uses.FindAllStringSubmatch(line, -1) {
				if match[1] != major[1] && !sha.MatchString(match[1]) && !exact.MatchString(match[1]) {
					problems = append(problems, fmt.Sprintf("%s:%d: action example must use %s, an exact release in that major, or a full distribution SHA", file, i+1, major[1]))
				}
			}
		}
	}
	slices.Sort(problems)
	return problems
}

func checkRepository(root string) []string {
	files, err := sourceMarkdown(root)
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	documents := map[string]string{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		problems = append(problems, checkLinks(root, file, string(data))...)
		rel, _ := filepath.Rel(root, file)
		documents[filepath.ToSlash(rel)] = string(data)
	}
	problems = append(problems, checkDistributionExamples(documents)...)
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		problems = append(problems, err.Error())
	} else if string(claude) != "@AGENTS.md\n" {
		problems = append(problems, "CLAUDE.md must contain only @AGENTS.md followed by LF")
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		problems = append(problems, "CLAUDE.md: missing AGENTS.md import target")
	}
	action, err := os.ReadFile(filepath.Join(root, "action.yml"))
	if err != nil {
		return append(problems, err.Error())
	}
	usage, err := os.ReadFile(filepath.Join(root, "docs", "USAGE.md"))
	if err != nil {
		return append(problems, err.Error())
	}
	return append(problems, checkReference(string(action), string(usage))...)
}

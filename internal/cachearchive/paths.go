package cachearchive

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
)

func portable(name string) error {
	if name == "" || name == "." || path.Clean(name) != name || strings.HasPrefix(name, "/") || strings.ContainsAny(name, "\\:\x00") {
		return fmt.Errorf("unsafe cache path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsAny(part, "<>\"|?*") {
			return fmt.Errorf("nonportable cache path %q", name)
		}
		for _, c := range part {
			if c < 32 {
				return fmt.Errorf("control character in cache path %q", name)
			}
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		reserved := base == "CON" || base == "PRN" || base == "AUX" || base == "NUL"
		for _, prefix := range []string{"COM", "LPT"} {
			if strings.HasPrefix(base, prefix) {
				suffix := strings.TrimPrefix(base, prefix)
				if len([]rune(suffix)) == 1 && strings.Contains("123456789¹²³", suffix) {
					reserved = true
				}
			}
		}
		if reserved {
			return fmt.Errorf("reserved cache path %q", name)
		}
	}
	return nil
}
func noSymlinkPath(name string) error {
	abs, err := filepath.Abs(name)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(abs)
	rest := strings.TrimPrefix(abs[len(volume):], string(filepath.Separator))
	current := volume + string(filepath.Separator)
	parts := strings.Split(rest, string(filepath.Separator))
	for i, part := range parts {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || (i < len(parts)-1 && !info.IsDir()) {
			return fmt.Errorf("unsafe cache destination component %q", current)
		}
	}
	return nil
}

func caseKey(s string) string {
	return strings.Map(func(r rune) rune {
		lowest := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < lowest {
				lowest = next
			}
		}
		return lowest
	}, s)
}

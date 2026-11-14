package cachearchive

import (
	"path/filepath"
	"strings"
	"syscall"
)

const treeMovesSupported = true

// MoveFileW moves directories on the same volume and fails if the destination
// already exists. Do not replace this with os.Rename (which allows replacement).
func moveTreeNoReplace(source, destination string) error {
	longPath := func(name string) (string, error) {
		name, err := filepath.Abs(name)
		if err != nil {
			return "", err
		}
		if strings.HasPrefix(name, `\\?\`) {
			return name, nil
		}
		if strings.HasPrefix(name, `\\`) {
			return `\\?\UNC\` + name[2:], nil
		}
		return `\\?\` + name, nil
	}
	source, err := longPath(source)
	if err != nil {
		return err
	}
	destination, err = longPath(destination)
	if err != nil {
		return err
	}
	return syscall.Rename(source, destination)
}

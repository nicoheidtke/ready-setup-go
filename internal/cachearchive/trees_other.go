//go:build !windows

package cachearchive

const treeMovesSupported = false

func moveTreeNoReplace(source, destination string) error {
	panic("directory publication is unavailable on this platform")
}

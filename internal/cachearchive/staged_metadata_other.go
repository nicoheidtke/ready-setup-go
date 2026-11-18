//go:build !windows

package cachearchive

import (
	"archive/tar"
	"os"
)

func finalizeStagedFile(*os.File, *tar.Header) (bool, error) { return false, nil }

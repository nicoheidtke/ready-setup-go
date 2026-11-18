package cachearchive

import (
	"archive/tar"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	fileBasicInfoClass                    = 0
	errorInvalidFunction    syscall.Errno = 1
	errorNotSupported       syscall.Errno = 50
	errorInvalidParameter   syscall.Errno = 87
	errorCallNotImplemented syscall.Errno = 120
	errorInvalidLevel       syscall.Errno = 124
)

// FILE_BASIC_INFO has four LARGE_INTEGER fields, attributes, and alignment
// padding on both supported Windows architectures.
type fileBasicInfo struct {
	creation, access, write, change int64
	attributes                      uint32
	_                               uint32
}

var setFileInformationByHandle = syscall.NewLazyDLL("kernel32.dll").NewProc("SetFileInformationByHandle")

// Apply metadata to a private regular file while its output handle is already
// open. Whole-tree publication then avoids reopening every file for Chtimes and
// separately querying/updating its readonly attribute. Directories remain
// writable until all stream validation finishes; their metadata is applied
// after their children. Failed restores remove readonly private files normally.
func finalizeStagedFile(f *os.File, h *tar.Header) (bool, error) {
	var info fileBasicInfo
	if !h.ModTime.IsZero() {
		stamp := syscall.NsecToFiletime(h.ModTime.UnixNano())
		info.access = int64(uint64(stamp.HighDateTime)<<32 | uint64(stamp.LowDateTime))
		info.write = info.access
	}
	// New 0600 staging files are already writable on Windows. Zero attributes
	// retain their defaults; readonly is the only archived permission Windows
	// supports. All other metadata is intentionally left unchanged.
	if h.Mode&0200 == 0 {
		info.attributes = syscall.FILE_ATTRIBUTE_READONLY
	}
	ok, _, err := setFileInformationByHandle.Call(f.Fd(), fileBasicInfoClass, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	runtime.KeepAlive(f)
	if ok == 0 {
		if unsupportedStagedMetadata(err) {
			// Keep the existing path-based metadata operations for filesystems
			// or redirectors that cannot apply FILE_BASIC_INFO through a handle.
			return false, nil
		}
		return false, &os.PathError{Op: "set staged file metadata", Path: f.Name(), Err: err}
	}
	return true, nil
}

func unsupportedStagedMetadata(err error) bool {
	// The structure and class are fixed and checked by native tests. Rejection
	// of this valid operation can therefore select the legacy implementation;
	// permission, invalid-handle and other I/O failures must still be surfaced.
	switch err {
	case errorInvalidFunction, errorNotSupported, errorInvalidParameter, errorCallNotImplemented, errorInvalidLevel:
		return true
	default:
		return false
	}
}

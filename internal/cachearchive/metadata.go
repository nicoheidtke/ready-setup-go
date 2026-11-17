package cachearchive

import (
	"archive/tar"
	"errors"
)

// These safety limits are independent of payload size and cache identities.
// Charging every prefix, including repeated ancestors, also bounds the work
// needed to construct/check implicit directories without another retained map.
type metadataLimits struct {
	entries, pathBytes, headerBytes int64
	depth                           int
}

var defaultMetadataLimits = metadataLimits{entries: 250_000, pathBytes: 128 << 20, headerBytes: 16 << 20, depth: 128}

type metadataBudget struct {
	limits                          metadataLimits
	entries, pathBytes, headerBytes int64
}

func (b *metadataBudget) add(name string) error {
	if b.entries >= b.limits.entries {
		return errors.New("cache metadata exceeds entry limit")
	}
	depth := 1
	var size int64
	for i := range len(name) {
		if name[i] == '/' {
			depth++
			size += int64(i)
		}
	}
	size += int64(len(name))
	if depth > b.limits.depth {
		return errors.New("cache metadata exceeds path depth limit")
	}
	if size > b.limits.pathBytes-b.pathBytes {
		return errors.New("cache metadata exceeds path byte limit")
	}
	b.entries++
	b.pathBytes += size
	return nil
}

func (b *metadataBudget) header(h *tar.Header) error {
	size := int64(len(h.Linkname) + len(h.Uname) + len(h.Gname))
	for k, v := range h.PAXRecords {
		// Standard fields have already been parsed into the header. Names
		// are charged by add; numeric/time fields have fixed-size storage.
		switch k {
		case "path", "linkpath", "size", "uid", "gid", "uname", "gname", "mtime", "atime", "ctime":
			continue
		}
		size += int64(len(k) + len(v))
	}
	for k, v := range h.Xattrs {
		size += int64(len(k) + len(v))
	}
	if size > b.limits.headerBytes-b.headerBytes {
		return errors.New("cache metadata exceeds header byte limit")
	}
	b.headerBytes += size
	return nil
}

// The merge only uses file type, permissions, size and times. Keep validated
// names separately and release unused strings/maps before retaining a header.
func retainedHeader(h *tar.Header) tar.Header {
	copy := *h
	copy.Name, copy.Linkname, copy.Uname, copy.Gname = "", "", "", ""
	copy.PAXRecords, copy.Xattrs = nil, nil
	return copy
}

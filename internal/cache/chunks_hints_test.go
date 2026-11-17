package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"ready-setup-go/internal/cachebackend"
)

func TestVerifiedChunkHintsReuseBeforeCompressionAndEvictionFallback(t *testing.T) {
	for _, compression := range []string{"gzip", "zstd", "none"} {
		t.Run(compression, func(t *testing.T) {
			c := DefaultConfig()
			c.Storage, c.Compression = "chunked", compression
			snap := state(t, c, "deps", roots(t)).Snapshots[0]
			b := &memoryBackend{}
			payload := bytes.Repeat([]byte("verified unchanged chunk"), 1000)
			if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &Metric{}); err != nil {
				t.Fatal(err)
			}
			original := append([]byte(nil), b.entries[len(b.entries)-1].data...)
			var hints []chunkHint
			if err := downloadChunks(context.Background(), b, snap, c, cachebackend.Entry{Key: snap.Key}, chunkTestFile(t, nil), &Metric{}, &hints); err != nil {
				t.Fatal(err)
			}
			if len(hints) != 1 {
				t.Fatalf("missing verified hints: %+v", hints)
			}
			snap.RestoredChunks = hints
			snap.Key += "-reused"
			// Invalid encoder level intentionally proves that the verified hit never
			// enters the gzip encoder; direct chunk helpers bypass Config.Validate.
			if compression == "gzip" {
				c.GzipLevel = 100
			}
			var m Metric
			if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &m); err != nil {
				t.Fatal(err)
			}
			if m.ReusedChunks != 1 || !bytes.Equal(original, b.entries[len(b.entries)-1].data) {
				t.Fatalf("hint changed manifest or failed reuse: %+v", m)
			}
			// An evicted hint must be compressed/uploaded again before any manifest.
			b.entries = b.entries[1:]
			c.GzipLevel = 0
			snap.Key += "-fallback"
			m = Metric{}
			if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &m); err != nil {
				t.Fatal(err)
			}
			if m.ReusedChunks != 0 || m.Bytes <= int64(len(original)) {
				t.Fatalf("evicted chunk was treated as present: %+v", m)
			}
			if !bytes.Equal(original, b.entries[len(b.entries)-1].data) {
				t.Fatal("fallback changed legacy manifest")
			}
			// Equal sizes are insufficient: changed bytes must not use the hint.
			payload[0] ^= 1
			snap.Key += "-changed"
			m = Metric{}
			if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, payload), &m); err != nil {
				t.Fatal(err)
			}
			if m.ReusedChunks != 0 {
				t.Fatal("changed contents reused hint")
			}
		})
	}
}

func TestChunkHintsPersistOnlyAfterFullArchiveValidation(t *testing.T) {
	c := DefaultConfig()
	c.Storage, c.Strict = "chunked", true
	rs := roots(t)
	put(t, filepath.Join(rs[1].Path, "object"), "build object")
	b := &memoryBackend{}
	if _, err := Save(context.Background(), b, state(t, c, "deps", rs)); err != nil {
		t.Fatal(err)
	}
	restored := state(t, c, "deps", roots(t))
	if _, err := Restore(context.Background(), b, &restored); err != nil {
		t.Fatal(err)
	}
	if len(restored.Snapshots[1].RestoredChunks) == 0 {
		t.Fatal("validated archive omitted hints")
	}
	data, err := json.Marshal(restored)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip State
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatal(err)
	}
	if len(roundtrip.Snapshots[1].RestoredChunks) == 0 {
		t.Fatal("hints lost across main/post state")
	}
	// Valid transport chunks containing an invalid tar must not retain hints.
	snap := restored.Snapshots[1]
	snap.Key += "-bad-tar"
	if err := saveChunks(context.Background(), b, snap, c, chunkTestFile(t, []byte("not a tar archive")), &Metric{}); err != nil {
		t.Fatal(err)
	}
	dest := state(t, c, "deps", roots(t))
	if _, err := Restore(context.Background(), b, &dest); err == nil {
		t.Fatal("invalid tar accepted")
	}
	if len(dest.Snapshots[1].RestoredChunks) != 0 {
		t.Fatal("unvalidated archive retained hints")
	}
}

func TestChunkHintStateRemainsBounded(t *testing.T) {
	c := DefaultConfig()
	s := state(t, c, "deps", roots(t))
	for i := range s.Snapshots {
		s.Snapshots[i].RestoredChunks = make([]chunkHint, maxChunkHints)
		for j := range s.Snapshots[i].RestoredChunks {
			s.Snapshots[i].RestoredChunks[j] = chunkHint{RawHash: string(bytes.Repeat([]byte("a"), 64)), Ref: chunkRef{Hash: string(bytes.Repeat([]byte("b"), 64)), Size: chunkMaximum, Raw: chunkMaximum}}
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) >= 1<<20 {
		t.Fatalf("bounded hints exceed post-state limit: %d", len(data))
	}
	// Ensure old states without hints continue to decode naturally.
	s = State{}
	if err := json.Unmarshal([]byte(`{"format":"legacy","snapshots":[{"kind":"build"}]}`), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Snapshots[0].RestoredChunks) != 0 {
		t.Fatal("old state acquired hints")
	}
}

package cache

import "testing"

func TestPackagedCancellation(t *testing.T) {
	t.Skip("Windows process termination is forceful; this fixture requires POSIX signals")
}

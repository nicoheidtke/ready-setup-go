package packages_test

import (
	"testing"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

func TestDependencies(t *testing.T) {
	var group errgroup.Group
	for range 4 {
		group.Go(func() error {
			_, err := uuid.Parse("6ba7b810-9dad-11d1-80b4-00c04fd430c8")
			return err
		})
	}
	if err := group.Wait(); err != nil {
		t.Fatal(err)
	}
}

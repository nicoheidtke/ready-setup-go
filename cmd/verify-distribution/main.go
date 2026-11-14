// verify-distribution checks local release artifacts; it never contacts GitHub.
package main

import (
	"flag"
	"fmt"
	"os"
	"setup-go-faster/internal/distribution"
)

func main() {
	root := flag.String("dist", "dist", "packaged action directory")
	archives := flag.String("archives", "", "optional release asset directory")
	allowDirty := flag.Bool("allow-dirty", false, "permit explicitly marked local dirty-source diagnostics")
	platform := flag.String("platform", "", "optional single platform; default requires all six")
	flag.Parse()
	if err := distribution.Verify(*root, *archives, *allowDirty, *platform); err != nil {
		fmt.Fprintln(os.Stderr, "distribution verification:", err)
		os.Exit(1)
	}
	fmt.Println("Distribution integrity and provenance metadata verified (not an origin attestation).")
}

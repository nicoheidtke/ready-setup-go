package main

import (
	"os"
	"strings"

	"setup-go-better/internal/actionlog"
	"setup-go-better/internal/cache"
)

func reportError(err error) {
	title := "Set up Go"
	hint := "Check the selected Go version, runner network access and writable toolchain directory."
	if len(os.Args) > 1 && os.Args[1] == "post" {
		title = "Save Go caches"
		hint = cache.DiagnosticHint(err)
	} else if strings.Contains(err.Error(), "cache") || strings.Contains(err.Error(), "GITHUB_STATE") {
		title = "Set up Go caches"
		hint = cache.DiagnosticHint(err)
	} else if strings.Contains(err.Error(), "go-version") || strings.Contains(err.Error(), "Go version") || strings.Contains(err.Error(), "flag") || strings.Contains(err.Error(), "summary:") {
		hint = "Check action inputs; choose Go 1.26+ using go-version or a workspace-relative go.mod/go.work file."
	}
	actionlog.Default.Error(title, err.Error()+" "+hint)
}

package actionenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func appendFile(env, text string) error {
	path := os.Getenv(env)
	if path == "" {
		return nil
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(text)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Output writes a single-line value using GitHub's file protocol.
func Output(name, value string) error { return field("GITHUB_OUTPUT", name, value) }

// State is private to this action instance and is made available to its post hook.
func State(name, value string) error { return field("GITHUB_STATE", name, value) }

func Environment(name, value string) error { return field("GITHUB_ENV", name, value) }

func field(env, name, value string) error {
	if name == "" || strings.ContainsAny(name, "=\r\n\x00") || strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("unsafe action field")
	}
	return appendFile(env, name+"="+value+"\n")
}

func Export(version, root string) error {
	if strings.ContainsAny(version+root, "\r\n\x00") {
		return fmt.Errorf("unsafe newline in action output")
	}
	outputs := []struct{ name, text string }{
		{"GITHUB_PATH", filepath.Join(root, "bin") + "\n"},
		{"GITHUB_ENV", "GOROOT=" + root + "\n"},
		{"GITHUB_OUTPUT", "go-version=" + version + "\ngoroot=" + root + "\n"},
	}
	for _, output := range outputs {
		if err := appendFile(output.name, output.text); err != nil {
			return fmt.Errorf("%s: %w", output.name, err)
		}
	}
	return nil
}

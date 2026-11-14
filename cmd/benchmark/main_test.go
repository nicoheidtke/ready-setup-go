package main

import (
	"strings"
	"testing"
)

func TestInstallationSamplesDisableCacheAndPostState(t *testing.T) {
	for _, key := range []string{"INPUT_CACHE", "INPUT_CACHE-SCOPE", "INPUT_CACHE-STRICT", "GITHUB_STATE", "STATE_cache_state"} {
		t.Setenv(key, "unexpected")
	}
	env := cleanEnv(map[string]string{})
	var cacheOff bool
	for _, item := range env {
		key, value, _ := strings.Cut(item, "=")
		if key == "INPUT_CACHE" {
			cacheOff = value == "off"
		}
		if (strings.HasPrefix(key, "INPUT_CACHE-") || key == "GITHUB_STATE" || key == "STATE_cache_state") && value != "" {
			t.Fatalf("cache settings leaked into installer sample %s", item)
		}
	}
	if !cacheOff {
		t.Fatal("installer sample cache not explicitly disabled")
	}
}

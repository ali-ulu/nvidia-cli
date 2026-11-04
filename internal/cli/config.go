// Package cli holds the configuration and presentation helpers used by the nv
// command.
package cli

import (
	"fmt"
	"os"
)

// EnvAPIKey is the environment variable read when --api-key is not given.
const EnvAPIKey = "NVIDIA_API_KEY"

// APIKey resolves the key to use. An explicit flag always wins.
func APIKey(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv(EnvAPIKey); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("no API key: pass --api-key or set %s", EnvAPIKey)
}

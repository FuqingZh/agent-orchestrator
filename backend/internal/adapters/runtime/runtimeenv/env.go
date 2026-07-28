// Package runtimeenv owns environment values that may belong to the AO daemon
// but must not cross into coding-agent runtime processes.
package runtimeenv

import (
	"fmt"
	"strings"
)

const (
	LinearAPIKey     = "AO_LINEAR_API_KEY"
	LinearOAuthToken = "AO_LINEAR_OAUTH_TOKEN"
)

// DaemonOnlyKeys returns environment keys that runtime processes must remove.
func DaemonOnlyKeys() []string {
	return []string{LinearAPIKey, LinearOAuthToken}
}

// IsDaemonOnly reports whether key is reserved for the AO daemon.
func IsDaemonOnly(key string) bool {
	for _, reserved := range DaemonOnlyKeys() {
		if strings.EqualFold(key, reserved) {
			return true
		}
	}
	return false
}

// WithoutDaemonOnly copies base without daemon-only credential entries.
func WithoutDaemonOnly(base []string) []string {
	env := make([]string, 0, len(base))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if !IsDaemonOnly(key) {
			env = append(env, entry)
		}
	}
	return env
}

// ValidateWorkerMap rejects explicit worker configuration of daemon-only keys.
func ValidateWorkerMap(env map[string]string) error {
	for key := range env {
		if IsDaemonOnly(key) {
			return fmt.Errorf("daemon-only env key %q", key)
		}
	}
	return nil
}

// ValidateWorkerAssignments rejects daemon-only keys in env-style assignments.
func ValidateWorkerAssignments(assignments []string) error {
	for _, assignment := range assignments {
		key, _, _ := strings.Cut(assignment, "=")
		if IsDaemonOnly(key) {
			return fmt.Errorf("daemon-only env key %q", key)
		}
	}
	return nil
}

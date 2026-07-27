package runtimeenv

import (
	"reflect"
	"strings"
	"testing"
)

func TestWithoutDaemonOnly(t *testing.T) {
	base := []string{
		"PATH=/bin",
		LinearAPIKey + "=secret",
		"EMPTY=",
		LinearOAuthToken + "=oauth-secret",
		"SHELL=/bin/sh",
	}
	want := []string{"PATH=/bin", "EMPTY=", "SHELL=/bin/sh"}
	if got := WithoutDaemonOnly(base); !reflect.DeepEqual(got, want) {
		t.Fatalf("WithoutDaemonOnly = %#v, want %#v", got, want)
	}
	if got := WithoutDaemonOnly(nil); got == nil || len(got) != 0 {
		t.Fatalf("WithoutDaemonOnly(nil) = %#v, want non-nil empty slice", got)
	}
}

func TestValidateWorkerSourcesRejectDaemonOnlyKeys(t *testing.T) {
	for _, key := range append(DaemonOnlyKeys(), strings.ToLower(LinearAPIKey)) {
		t.Run(key, func(t *testing.T) {
			if err := ValidateWorkerMap(map[string]string{key: "secret"}); err == nil ||
				!strings.Contains(err.Error(), "daemon-only env key") {
				t.Fatalf("ValidateWorkerMap error = %v", err)
			}
			if err := ValidateWorkerAssignments([]string{key + "=secret"}); err == nil ||
				!strings.Contains(err.Error(), "daemon-only env key") {
				t.Fatalf("ValidateWorkerAssignments error = %v", err)
			}
		})
	}
}

func TestValidateWorkerSourcesAllowOrdinaryKeys(t *testing.T) {
	if err := ValidateWorkerMap(map[string]string{"PATH": "/bin"}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWorkerAssignments([]string{"OPENCODE_CONFIG=C:/cfg.json"}); err != nil {
		t.Fatal(err)
	}
}

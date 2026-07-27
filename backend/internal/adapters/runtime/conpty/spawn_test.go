package conpty

import (
	"reflect"
	"strings"
	"testing"
)

func TestStripEnvAssignments(t *testing.T) {
	tests := []struct {
		name            string
		argv            []string
		wantAssignments []string
		wantRest        []string
	}{
		{
			name:            "no env prefix returns argv unchanged",
			argv:            []string{"opencode", "--agent", "ao-x"},
			wantAssignments: nil,
			wantRest:        []string{"opencode", "--agent", "ao-x"},
		},
		{
			name:            "env prefix is split from the real command",
			argv:            []string{"env", "OPENCODE_CONFIG=C:/cfg.json", "opencode", "--agent", "ao-x"},
			wantAssignments: []string{"OPENCODE_CONFIG=C:/cfg.json"},
			wantRest:        []string{"opencode", "--agent", "ao-x"},
		},
		{
			name:            "env with no command left is untouched",
			argv:            []string{"env", "A=1", "B=2"},
			wantAssignments: nil,
			wantRest:        []string{"env", "A=1", "B=2"},
		},
		{
			name:            "a binary merely starting with env is not treated as a prefix",
			argv:            []string{"envoy", "--config", "x"},
			wantAssignments: nil,
			wantRest:        []string{"envoy", "--config", "x"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotAssignments, gotRest := stripEnvAssignments(tt.argv)
			if !reflect.DeepEqual(gotAssignments, tt.wantAssignments) {
				t.Errorf("assignments = %#v, want %#v", gotAssignments, tt.wantAssignments)
			}
			if !reflect.DeepEqual(gotRest, tt.wantRest) {
				t.Errorf("rest = %#v, want %#v", gotRest, tt.wantRest)
			}
		})
	}
}

func TestWorkerEnvironmentStripsAndRejectsDaemonOnlyKeys(t *testing.T) {
	got, err := workerEnvironment(
		[]string{"PATH=C:/bin", "AO_LINEAR_API_KEY=ambient-secret"},
		map[string]string{"ORDINARY": "value"},
		[]string{"OPENCODE_CONFIG=C:/cfg.json"},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"PATH=C:/bin",
		"ORDINARY=value",
		"OPENCODE_CONFIG=C:/cfg.json",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("workerEnvironment = %#v, want %#v", got, want)
	}

	for _, tc := range []struct {
		name        string
		env         map[string]string
		assignments []string
	}{
		{
			name: "map",
			env:  map[string]string{"AO_LINEAR_OAUTH_TOKEN": "secret"},
		},
		{
			name:        "assignment",
			assignments: []string{"AO_LINEAR_API_KEY=secret"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := workerEnvironment(nil, tc.env, tc.assignments)
			if err == nil || !strings.Contains(err.Error(), "daemon-only env key") {
				t.Fatalf("workerEnvironment error = %v", err)
			}
		})
	}
}

package githubappauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewFromEnvironmentNotConfigured(t *testing.T) {
	source, configured, err := NewFromEnvironment(func(string) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if configured || source != nil {
		t.Fatalf("source = %#v, configured = %v; want nil, false", source, configured)
	}
}

func TestNewFromEnvironmentRejectsPartialConfiguration(t *testing.T) {
	values := envValues{appIDEnv: "1"}
	source, configured, err := NewFromEnvironment(values.get)
	if !configured || source != nil || err == nil {
		t.Fatalf("source = %#v, configured = %v, err = %v; want nil, true, error", source, configured, err)
	}
	if !strings.Contains(err.Error(), "must be configured together") {
		t.Fatalf("error = %q", err)
	}
}

func TestNewFromEnvironmentRejectsInvalidIDs(t *testing.T) {
	key := writeTestKey(t, 0o600)
	for _, tc := range []struct {
		name           string
		appID          string
		installationID string
	}{
		{name: "invalid app", appID: "abc", installationID: "2"},
		{name: "zero app", appID: "0", installationID: "2"},
		{name: "negative installation", appID: "1", installationID: "-2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := envValues{
				appIDEnv:          tc.appID,
				installationIDEnv: tc.installationID,
				privateKeyFileEnv: key,
			}
			_, configured, err := NewFromEnvironment(values.get)
			if !configured || err == nil {
				t.Fatalf("configured = %v, err = %v; want true, error", configured, err)
			}
		})
	}
}

func TestNewFromEnvironmentAcceptsCompleteConfiguration(t *testing.T) {
	key := writeValidTestKey(t)
	values := envValues{
		appIDEnv:          "4402372",
		installationIDEnv: "149256970",
		privateKeyFileEnv: key,
	}
	source, configured, err := NewFromEnvironment(values.get)
	if err != nil {
		t.Fatal(err)
	}
	if !configured || source == nil {
		t.Fatalf("source = %#v, configured = %v; want non-nil, true", source, configured)
	}
}

func TestNewRejectsUnsafePrivateKeyPaths(t *testing.T) {
	secure := writeTestKey(t, 0o600)
	symlink := filepath.Join(t.TempDir(), "key.pem")
	if err := os.Symlink(secure, symlink); err != nil {
		t.Fatal(err)
	}
	insecure := writeTestKey(t, 0o644)

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{name: "relative", path: "key.pem", want: "absolute"},
		{name: "symlink", path: symlink, want: "symlink"},
		{name: "insecure permissions", path: insecure, want: "permissions"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(Config{AppID: 1, InstallationID: 2, PrivateKeyFile: tc.path})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestSourceDelegatesToken(t *testing.T) {
	wantErr := errors.New("refresh failed")
	source := &Source{provider: stubTokenProvider{token: "installation-token"}}
	token, err := source.Token(context.Background())
	if err != nil || token != "installation-token" {
		t.Fatalf("Token() = %q, %v", token, err)
	}

	source = &Source{provider: stubTokenProvider{err: wantErr}}
	_, err = source.Token(context.Background())
	if !errors.Is(err, wantErr) {
		t.Fatalf("Token() error = %v, want %v", err, wantErr)
	}
}

type stubTokenProvider struct {
	token string
	err   error
}

func (s stubTokenProvider) Token(context.Context) (string, error) {
	return s.token, s.err
}

type envValues map[string]string

func (v envValues) get(name string) string {
	return v[name]
}

func writeTestKey(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, []byte("not parsed by validation-only tests"), mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeValidTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	encoded := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

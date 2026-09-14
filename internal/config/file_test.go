package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestLoadFile_Empty(t *testing.T) {
	fc, err := LoadFile("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc != nil {
		t.Error("expected nil config for empty path")
	}
}

func TestLoadFile_NotFound(t *testing.T) {
	_, err := LoadFile("/nonexistent/path.toml")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFile_InvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")
	if err := os.WriteFile(path, []byte("not valid toml [[["), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(path)
	if err == nil {
		t.Error("expected error for invalid TOML")
	}
}

func TestLoadFile_FullConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.toml")
	if err := os.WriteFile(path, []byte(`
[server]
listen_addr = ":7070"
pprof = true

[server.sse]
batch_interval = "50ms"

[docker]
host = "tcp://myhost:2375"

[prometheus]
url = "http://myprom:9090"

[logging]
level = "debug"
format = "text"

[storage]
data_dir = "/var/lib/cetacean"
snapshot = false
`), 0644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.Server == nil || *fc.Server.ListenAddr != ":7070" {
		t.Error("ListenAddr not parsed")
	}
	if fc.Server.Pprof == nil || *fc.Server.Pprof != true {
		t.Error("Pprof not parsed")
	}
	if fc.Server.SSE == nil || *fc.Server.SSE.BatchInterval != "50ms" {
		t.Error("SSE BatchInterval not parsed")
	}
	if fc.Docker == nil || *fc.Docker.Host != "tcp://myhost:2375" {
		t.Error("Docker Host not parsed")
	}
	if fc.Prom == nil || *fc.Prom.URL != "http://myprom:9090" {
		t.Error("Prometheus URL not parsed")
	}
	if fc.Logging == nil || *fc.Logging.Level != "debug" {
		t.Error("Logging Level not parsed")
	}
	if fc.Logging.Format == nil || *fc.Logging.Format != "text" {
		t.Error("Logging Format not parsed")
	}
	if fc.Storage == nil || *fc.Storage.DataDir != "/var/lib/cetacean" {
		t.Error("Storage DataDir not parsed")
	}
	if fc.Storage.Snapshot == nil || *fc.Storage.Snapshot != false {
		t.Error("Storage Snapshot not parsed")
	}
}

func TestLoadFile_TLSConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tls.toml")
	if err := os.WriteFile(path, []byte(`
[tls]
cert = "/etc/cetacean/cert.pem"
key = "/etc/cetacean/key.pem"
`), 0644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.TLS == nil || *fc.TLS.Cert != "/etc/cetacean/cert.pem" {
		t.Error("TLS Cert not parsed")
	}
	if *fc.TLS.Key != "/etc/cetacean/key.pem" {
		t.Error("TLS Key not parsed")
	}
}

func TestLoadFile_AuthConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.toml")
	if err := os.WriteFile(path, []byte(`
[auth]
mode = "oidc"

[auth.oidc]
issuer = "https://idp.example.com"
client_id = "cetacean"
client_secret = "secret"
redirect_url = "https://app.example.com/auth/callback"
scopes = "openid,profile,email,groups"
session_key = "abcdef"

[auth.tailscale]
mode = "tsnet"
authkey = "tskey-123"
hostname = "myhost"
state_dir = "/var/lib/ts"
capability = "example.com/cap/cetacean"

[auth.cert]
ca = "/etc/ca.pem"

[auth.headers]
subject = "X-User"
name = "X-Name"
email = "X-Email"
groups = "X-Groups"
secret_header = "X-Secret"
secret_value = "s3cret"
`), 0644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.Auth == nil || *fc.Auth.Mode != "oidc" {
		t.Error("Auth Mode not parsed")
	}
	if fc.Auth.OIDC == nil || *fc.Auth.OIDC.Issuer != "https://idp.example.com" {
		t.Error("OIDC Issuer not parsed")
	}
	if *fc.Auth.OIDC.ClientID != "cetacean" {
		t.Error("OIDC ClientID not parsed")
	}
	if fc.Auth.Tailscale == nil || *fc.Auth.Tailscale.AuthKey != "tskey-123" {
		t.Error("Tailscale AuthKey not parsed")
	}
	if fc.Auth.Cert == nil || *fc.Auth.Cert.CA != "/etc/ca.pem" {
		t.Error("Cert CA not parsed")
	}
	if fc.Auth.Headers == nil || *fc.Auth.Headers.Subject != "X-User" {
		t.Error("Headers Subject not parsed")
	}
}

func TestDiscoverConfigFile_WorkingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cetacean.toml")
	if err := os.WriteFile(path, []byte("[server]\n"), 0644); err != nil {
		t.Fatal(err)
	}

	original, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original) //nolint:errcheck // best-effort restore

	got := DiscoverConfigFile()
	if got != "cetacean.toml" {
		t.Errorf("got %q, want %q", got, "cetacean.toml")
	}
}

func TestDiscoverConfigFile_XDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	xdgDir := filepath.Join(dir, "cetacean")
	if err := os.MkdirAll(xdgDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(xdgDir, "cetacean.toml"),
		[]byte("[server]\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	t.Setenv("XDG_CONFIG_HOME", dir)

	// Run from a dir with no config file
	empty := t.TempDir()
	original, _ := os.Getwd()
	if err := os.Chdir(empty); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original) //nolint:errcheck // best-effort restore

	got := DiscoverConfigFile()
	want := filepath.Join(xdgDir, "cetacean.toml")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDiscoverConfigFile_None(t *testing.T) {
	dir := t.TempDir()
	original, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original) //nolint:errcheck // best-effort restore

	// Clear env to prevent XDG/HOME discovery
	t.Setenv("XDG_CONFIG_HOME", dir) // points to temp dir with no cetacean subdir

	got := DiscoverConfigFile()
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestLoadFile_PartialConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "partial.toml")
	if err := os.WriteFile(path, []byte(`
[prometheus]
url = "http://prom:9090"
`), 0644); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fc.Server != nil {
		t.Error("Server should be nil for partial config")
	}
	if fc.Prom == nil || *fc.Prom.URL != "http://prom:9090" {
		t.Error("Prometheus URL not parsed")
	}
}

// A key the schema does not know is refused rather than ignored. Settings move
// between releases, and the ones that move here mostly exist to switch a
// capability off — silently ignoring one hands the operator the permissive
// default while their file says otherwise.
func TestLoadFileRefusesUnknownKeys(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "a section that moved",
			body: "[mcp.oauth]\ndcr_enabled = false\ncimd_enabled = false\n",
			want: "mcp.oauth.dcr_enabled",
		},
		{
			name: "a key that moved out of its section",
			body: "[mcp]\nsigning_key = \"x\"\n",
			want: "mcp.signing_key",
		},
		{
			// The setting is listen_addr; this is the guess someone makes when
			// they have not looked it up.
			name: "a plausible wrong name",
			body: "[server]\nlisten_address = \":9000\"\n",
			want: "server.listen_address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cetacean.toml")
			if err := os.WriteFile(path, []byte(tt.body), 0600); err != nil {
				t.Fatal(err)
			}

			_, err := LoadFile(path)
			if err == nil {
				t.Fatal("unknown keys were accepted")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error does not name %s: %v", tt.want, err)
			}
		})
	}
}

// The refusal must not fire on a file that only uses settings the schema knows.
func TestLoadFileAcceptsTheCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cetacean.toml")
	body := "[server]\nlisten_addr = \":9000\"\n\n" +
		"[mcp]\nenabled = true\nauth_bypass = [\"cert\"]\n\n" +
		"[oauth]\nenabled = true\ndcr_enabled = false\ncimd_enabled = false\n"
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}

	fc, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if fc.OAuth == nil || fc.OAuth.DCREnabled == nil || *fc.OAuth.DCREnabled {
		t.Error("oauth.dcr_enabled did not reach the config")
	}
	if fc.MCP == nil || len(fc.MCP.AuthBypass) != 1 {
		t.Error("mcp.auth_bypass did not reach the config")
	}
}

// The reference file is documentation an operator copies from, so it has to
// survive the same refusal their own file would. Now that an unknown key is an
// error, a setting renamed in code and not in the reference fails here rather
// than in someone's deployment.
// Every setting in the reference is commented out, so loading the file as it
// stands decodes nothing and LoadFile's refusal of an unknown key never fires.
// The settings are uncommented first, which is what makes a rename in code and
// not in the reference fail here rather than in someone's deployment.
func TestReferenceConfigMatchesTheSchema(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "config.reference.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	live := uncommentReference(string(raw))
	if !strings.Contains(live, "dcr_max_clients") {
		t.Fatal("uncommenting produced no settings, so this test proves nothing")
	}

	dir := t.TempDir()
	uncommented := filepath.Join(dir, "reference.toml")
	if err := os.WriteFile(uncommented, []byte(live), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadFile(uncommented); err != nil {
		t.Fatalf("docs/config.reference.toml does not match the schema: %v", err)
	}
}

// uncommentReference strips one level of comment from the reference's settings
// and section headers, dropping its prose: a line only survives if it spells a
// key or a section once uncommented.
func uncommentReference(src string) string {
	var live []string
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(trimmed, "# "); ok {
			if candidate := strings.TrimSpace(after); referenceKey.MatchString(candidate) ||
				referenceSection.MatchString(candidate) {
				live = append(live, candidate)
			}

			continue
		}
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			live = append(live, trimmed)
		}
	}

	return strings.Join(live, "\n")
}

var (
	referenceKey     = regexp.MustCompile(`^[A-Za-z0-9_]+\s*=`)
	referenceSection = regexp.MustCompile(`^\[[A-Za-z0-9_.]+\]$`)
)

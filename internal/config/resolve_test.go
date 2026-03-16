package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolve(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("TEST_RES", "env")
		flag := "flag"
		file := "file"
		got := resolve(&flag, "TEST_RES", &file, "default")
		if got != "flag" {
			t.Errorf("got %s, want flag", got)
		}
	})

	t.Run("env wins when no flag", func(t *testing.T) {
		t.Setenv("TEST_RES", "env")
		file := "file"
		got := resolve(nil, "TEST_RES", &file, "default")
		if got != "env" {
			t.Errorf("got %s, want env", got)
		}
	})

	t.Run("file wins when no flag or env", func(t *testing.T) {
		t.Setenv("TEST_RES", "")
		file := "file"
		got := resolve(nil, "TEST_RES", &file, "default")
		if got != "file" {
			t.Errorf("got %s, want file", got)
		}
	})

	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv("TEST_RES", "")
		got := resolve(nil, "TEST_RES", nil, "default")
		if got != "default" {
			t.Errorf("got %s, want default", got)
		}
	})
}

func TestResolveSecret(t *testing.T) {
	t.Run("flag wins over everything", func(t *testing.T) {
		t.Setenv("TEST_SEC", "env")
		flag := "flag"
		file := "file"
		got, err := resolveSecret(&flag, "TEST_SEC", &file, "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != "flag" {
			t.Errorf("got %s, want flag", got)
		}
	})

	t.Run("env wins over _FILE", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "secret")
		os.WriteFile(path, []byte("from-file"), 0600)
		t.Setenv("TEST_SEC", "env")
		t.Setenv("TEST_SEC_FILE", path)
		got, err := resolveSecret(nil, "TEST_SEC", nil, "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != "env" {
			t.Errorf("got %s, want env", got)
		}
	})

	t.Run("_FILE reads file contents", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "secret")
		os.WriteFile(path, []byte("s3cret\n"), 0600)
		t.Setenv("TEST_SEC", "")
		t.Setenv("TEST_SEC_FILE", path)
		got, err := resolveSecret(nil, "TEST_SEC", nil, "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != "s3cret" {
			t.Errorf("got %q, want s3cret (trailing newline should be trimmed)", got)
		}
	})

	t.Run("_FILE missing file returns error", func(t *testing.T) {
		t.Setenv("TEST_SEC", "")
		t.Setenv("TEST_SEC_FILE", "/nonexistent/secret")
		_, err := resolveSecret(nil, "TEST_SEC", nil, "default")
		if err == nil {
			t.Error("expected error for missing secret file")
		}
	})

	t.Run("config file fallback", func(t *testing.T) {
		t.Setenv("TEST_SEC", "")
		t.Setenv("TEST_SEC_FILE", "")
		file := "from-config"
		got, err := resolveSecret(nil, "TEST_SEC", &file, "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != "from-config" {
			t.Errorf("got %s, want from-config", got)
		}
	})

	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv("TEST_SEC", "")
		t.Setenv("TEST_SEC_FILE", "")
		got, err := resolveSecret(nil, "TEST_SEC", nil, "default")
		if err != nil {
			t.Fatal(err)
		}
		if got != "default" {
			t.Errorf("got %s, want default", got)
		}
	})
}

func TestResolveBool(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("TEST_BOOL", "false")
		flag := true
		file := false
		got := resolveBool(&flag, "TEST_BOOL", &file, false)
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("env true", func(t *testing.T) {
		t.Setenv("TEST_BOOL", "true")
		got := resolveBool(nil, "TEST_BOOL", nil, false)
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("env false", func(t *testing.T) {
		t.Setenv("TEST_BOOL", "0")
		got := resolveBool(nil, "TEST_BOOL", nil, true)
		if got != false {
			t.Errorf("got %v, want false", got)
		}
	})

	t.Run("env invalid falls through to file", func(t *testing.T) {
		t.Setenv("TEST_BOOL", "maybe")
		file := true
		got := resolveBool(nil, "TEST_BOOL", &file, false)
		if got != true {
			t.Errorf("got %v, want true (from file)", got)
		}
	})

	t.Run("file wins when no flag or env", func(t *testing.T) {
		t.Setenv("TEST_BOOL", "")
		file := true
		got := resolveBool(nil, "TEST_BOOL", &file, false)
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})

	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv("TEST_BOOL", "")
		got := resolveBool(nil, "TEST_BOOL", nil, true)
		if got != true {
			t.Errorf("got %v, want true", got)
		}
	})
}

func TestResolveDuration(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("TEST_DUR", "200ms")
		flag := "50ms"
		file := "300ms"
		got, err := resolveDuration(&flag, "TEST_DUR", &file, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if got != 50*time.Millisecond {
			t.Errorf("got %v, want 50ms", got)
		}
	})

	t.Run("env wins when no flag", func(t *testing.T) {
		t.Setenv("TEST_DUR", "200ms")
		got, err := resolveDuration(nil, "TEST_DUR", nil, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if got != 200*time.Millisecond {
			t.Errorf("got %v, want 200ms", got)
		}
	})

	t.Run("file wins when no flag or env", func(t *testing.T) {
		t.Setenv("TEST_DUR", "")
		file := "300ms"
		got, err := resolveDuration(nil, "TEST_DUR", &file, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if got != 300*time.Millisecond {
			t.Errorf("got %v, want 300ms", got)
		}
	})

	t.Run("default when nothing set", func(t *testing.T) {
		t.Setenv("TEST_DUR", "")
		got, err := resolveDuration(nil, "TEST_DUR", nil, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if got != time.Second {
			t.Errorf("got %v, want 1s", got)
		}
	})

	t.Run("invalid duration returns error", func(t *testing.T) {
		t.Setenv("TEST_DUR", "invalid")
		_, err := resolveDuration(nil, "TEST_DUR", nil, time.Second)
		if err == nil {
			t.Error("expected error for invalid duration")
		}
	})

	t.Run("non-positive duration returns error", func(t *testing.T) {
		t.Setenv("TEST_DUR", "-5ms")
		_, err := resolveDuration(nil, "TEST_DUR", nil, time.Second)
		if err == nil {
			t.Error("expected error for negative duration")
		}
	})
}

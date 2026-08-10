package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureAndResetPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "admin.password")
	t.Setenv("KC_BOOTSTRAP_PASSWORD_FILE", path)

	pw1, created, err := EnsurePassword()
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected created on first run")
	}
	if pw1 == "" {
		t.Fatal("empty password")
	}

	pw2, created, err := EnsurePassword()
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("should not recreate")
	}
	if pw1 != pw2 {
		t.Fatalf("password changed: %q vs %q", pw1, pw2)
	}

	pw3, err := ResetPassword()
	if err != nil {
		t.Fatal(err)
	}
	if pw3 == pw1 {
		t.Fatal("reset should change password")
	}

	got, err := ReadPassword(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != pw3 {
		t.Fatalf("file mismatch: %q vs %q", got, pw3)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("unexpected mode: %o", info.Mode().Perm())
	}
}

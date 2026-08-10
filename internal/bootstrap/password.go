package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const defaultPasswordFile = "/data/admin.password"

func PasswordFile() string {
	if p := strings.TrimSpace(os.Getenv("KC_BOOTSTRAP_PASSWORD_FILE")); p != "" {
		return p
	}
	return defaultPasswordFile
}

func ReadPassword(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	pw := strings.TrimSpace(string(b))
	if pw == "" {
		return "", fmt.Errorf("password file %s is empty", path)
	}
	return pw, nil
}

func WritePassword(path, password string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(password+"\n"), 0o600)
}

func GeneratePassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// EnsurePassword returns the active bootstrap password, creating the file when missing.
func EnsurePassword() (string, bool, error) {
	if override := strings.TrimSpace(os.Getenv("KC_BOOTSTRAP_ADMIN_PASSWORD")); override != "" {
		path := PasswordFile()
		if err := WritePassword(path, override); err != nil {
			return "", false, err
		}
		return override, false, nil
	}

	path := PasswordFile()
	if pw, err := ReadPassword(path); err == nil {
		return pw, false, nil
	}

	pw, err := GeneratePassword()
	if err != nil {
		return "", false, err
	}
	if err := WritePassword(path, pw); err != nil {
		return "", false, err
	}
	return pw, true, nil
}

func ResetPassword() (string, error) {
	pw, err := GeneratePassword()
	if err != nil {
		return "", err
	}
	if err := WritePassword(PasswordFile(), pw); err != nil {
		return "", err
	}
	return pw, nil
}

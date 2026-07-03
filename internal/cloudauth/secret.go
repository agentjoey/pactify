package cloudauth

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoMasterSecret is returned by LoadMasterSecret when no source yields a
// secret.
var ErrNoMasterSecret = errors.New(
	"cloudauth: no master secret found (checked $PACTIFY_MASTER_SECRET, $LINX_MASTER_SECRET, " +
		"~/.config/pactify/master-secret, ~/.config/linx/master-secret)")

// LoadMasterSecret loads the 32-byte account master secret, hex-encoded, from
// the first available source (shared-architecture read order):
//
//  1. env PACTIFY_MASTER_SECRET
//  2. env LINX_MASTER_SECRET            (compat)
//  3. ~/.config/pactify/master-secret   (mode 0600)
//  4. ~/.config/linx/master-secret      (compat fallback)
//
// It never creates or migrates files. A source that exists but holds invalid
// hex or the wrong length is an error, not a fall-through — a corrupt secret
// should be surfaced, not silently shadowed by a lower-precedence one.
func LoadMasterSecret() ([]byte, error) {
	if v := os.Getenv("PACTIFY_MASTER_SECRET"); v != "" {
		return decodeMasterSecret(v, "$PACTIFY_MASTER_SECRET")
	}
	if v := os.Getenv("LINX_MASTER_SECRET"); v != "" {
		return decodeMasterSecret(v, "$LINX_MASTER_SECRET")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cloudauth: resolve home dir: %w", err)
	}
	for _, path := range []string{
		filepath.Join(home, ".config", "pactify", "master-secret"),
		filepath.Join(home, ".config", "linx", "master-secret"),
	} {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cloudauth: read master secret: %w", err)
		}
		return decodeMasterSecret(string(raw), path)
	}
	return nil, ErrNoMasterSecret
}

// PactifySecretPath is the canonical location pactify writes the master secret
// to (~/.config/pactify/master-secret).
func PactifySecretPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cloudauth: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "pactify", "master-secret"), nil
}

// SaveMasterSecret writes a 32-byte secret as hex to PactifySecretPath() with
// 0600 perms. Refuses to overwrite an existing file unless force is set (so a
// stray `pair` never clobbers an established identity).
func SaveMasterSecret(secret []byte, force bool) error {
	if len(secret) != masterSecretLen {
		return fmt.Errorf("cloudauth: master secret must be %d bytes, got %d", masterSecretLen, len(secret))
	}
	path, err := PactifySecretPath()
	if err != nil {
		return err
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("cloudauth: %s already exists (pass force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(secret)), 0o600)
}

func decodeMasterSecret(v, source string) ([]byte, error) {
	secret, err := hex.DecodeString(strings.TrimSpace(v))
	if err != nil {
		return nil, fmt.Errorf("cloudauth: master secret from %s is not valid hex: %w", source, err)
	}
	if len(secret) != masterSecretLen {
		return nil, fmt.Errorf("cloudauth: master secret from %s must be %d bytes, got %d", source, masterSecretLen, len(secret))
	}
	return secret, nil
}

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

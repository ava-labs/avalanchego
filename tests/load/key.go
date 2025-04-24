// Copyright (C) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

func loadKeysFromDir(dir string) ([]*ecdsa.PrivateKey, error) {
	_, err := os.Stat(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		return nil, nil
	}

	var paths []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		} else if path == dir || info.IsDir() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", dir, err)
	}

	keys := make([]*ecdsa.PrivateKey, len(paths))
	for i, path := range paths {
		keys[i], err = ethcrypto.LoadECDSA(path)
		if err != nil {
			return nil, fmt.Errorf("loading private key from %s: %w", path, err)
		}
	}
	return keys, nil
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package key

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ava-labs/libevm/common"

	ethcrypto "github.com/ava-labs/libevm/crypto"
)

type Key struct {
	PrivKey *ecdsa.PrivateKey
	Address common.Address
}

func CreateKey(pk *ecdsa.PrivateKey) *Key {
	return &Key{pk, ethcrypto.PubkeyToAddress(pk.PublicKey)}
}

// Load attempts to open a [Key] stored at [file].
func Load(file string) (*Key, error) {
	pk, err := ethcrypto.LoadECDSA(file)
	if err != nil {
		return nil, fmt.Errorf("problem loading private key from %s: %w", file, err)
	}
	return CreateKey(pk), nil
}

// LoadAll loads all keys in [dir].
func LoadAll(ctx context.Context, dir string) ([]*Key, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("unable to create %s: %w", dir, err)
		}

		return nil, nil
	}

	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if path == dir {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not walk %s: %w", dir, err)
	}

	ks := make([]*Key, len(files))
	for i, file := range files {
		k, err := Load(file)
		if err != nil {
			return nil, fmt.Errorf("could not load key at %s: %w", file, err)
		}

		ks[i] = k
	}
	return ks, nil
}

// Save persists a [Key] to [dir] (where the filename is the hex-encoded
// address).
func (k *Key) Save(dir string) error {
	fp := filepath.Join(dir, k.Address.Hex())
	return ethcrypto.SaveECDSA(fp, k.PrivKey)
}

// Generate creates a new [Key] and returns it.
func Generate() (*Key, error) {
	pk, err := ethcrypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot generate key", err)
	}
	return CreateKey(pk), nil
}

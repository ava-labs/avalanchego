package worker

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	workerDir = ".simulator/keys"
)

type Key struct {
	pk   *ecdsa.PrivateKey
	addr common.Address
}

func createKey(pk *ecdsa.PrivateKey) *Key {
	return &Key{pk, ethcrypto.PubkeyToAddress(pk.PublicKey)}
}

func LoadKey(file string) (*Key, error) {
	pk, err := ethcrypto.LoadECDSA(file)
	if err != nil {
		return nil, fmt.Errorf("problem loading private key from %s: %w", file, err)
	}
	return createKey(pk), nil
}

func LoadAvailableKeys(ctx context.Context) ([]*Key, error) {
	if _, err := os.Stat(workerDir); os.IsNotExist(err) {
		if err := os.MkdirAll(workerDir, 0755); err != nil {
			return nil, fmt.Errorf("unable to create %s: %w", workerDir, err)
		}

		return nil, nil
	}

	var files []string

	err := filepath.Walk(workerDir, func(path string, info os.FileInfo, err error) error {
		if path == workerDir {
			return nil
		}

		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not walk %s: %w", workerDir, err)
	}

	ks := make([]*Key, len(files))
	for i, file := range files {
		k, err := LoadKey(file)
		if err != nil {
			return nil, fmt.Errorf("could not load key at %s: %w", file, err)
		}

		ks[i] = k
	}
	return ks, nil
}

func SaveKey(k *Key) error {
	fp := filepath.Join(workerDir, k.addr.Hex())
	return ethcrypto.SaveECDSA(fp, k.pk)
}

func GenerateKey() (*Key, error) {
	pk, err := ethcrypto.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("%w: cannot generate key", err)
	}
	return createKey(pk), nil
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/rpcsigner"
	"github.com/ava-labs/avalanchego/utils/perms"
)

// newStakingSigner returns a BLS signer based on the provided configuration.
func newStakingSigner(
	config any,
) (bls.Signer, error) {
	switch cfg := config.(type) {
	case node.EphemeralSignerConfig:
		signer, err := localsigner.New()
		if err != nil {
			return nil, fmt.Errorf("could not generate ephemeral signer: %w", err)
		}

		return signer, nil

	case node.ContentKeyConfig:
		signerKeyContent, err := base64.StdEncoding.DecodeString(cfg.SignerKeyRawContent)
		if err != nil {
			return nil, fmt.Errorf("unable to decode base64 content: %w", err)
		}

		signer, err := localsigner.FromBytes(signerKeyContent)
		if err != nil {
			return nil, fmt.Errorf("could not parse signing key: %w", err)
		}

		return signer, nil

	case node.RPCSignerConfig:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		signer, err := rpcsigner.NewClient(ctx, cfg.StakingSignerRPC)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("could not create rpc signer client: %w", err)
		}

		return signer, nil

	case node.SignerPathConfig:
		return createSignerFromFile(cfg.SignerKeyPath)

	case node.DefaultSignerConfig:
		_, err := os.Stat(cfg.SignerKeyPath)
		if !errors.Is(err, fs.ErrNotExist) {
			return createSignerFromFile(cfg.SignerKeyPath)
		}
		return createSignerFromNewKey(cfg.SignerKeyPath)

	default:
		return nil, fmt.Errorf("unsupported signer type: %T", cfg)
	}
}

func createSignerFromFile(signerKeyPath string) (bls.Signer, error) {
	signingKeyBytes, err := os.ReadFile(signerKeyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read signing key from %s: %w", signerKeyPath, err)
	}

	signer, err := localsigner.FromBytes(signingKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse signing key: %w", err)
	}

	return signer, nil
}

func createSignerFromNewKey(signerKeyPath string) (bls.Signer, error) {
	signer, err := localsigner.New()
	if err != nil {
		return nil, fmt.Errorf("could not generate new signing key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(signerKeyPath), perms.ReadWriteExecute); err != nil {
		return nil, fmt.Errorf("could not create path for signing key at %s: %w", signerKeyPath, err)
	}

	keyBytes := signer.ToBytes()
	if err := os.WriteFile(signerKeyPath, keyBytes, perms.ReadWrite); err != nil {
		return nil, fmt.Errorf("could not write new signing key to %s: %w", signerKeyPath, err)
	}
	if err := os.Chmod(signerKeyPath, perms.ReadOnly); err != nil {
		return nil, fmt.Errorf("could not restrict permissions on new signing key at %s: %w", signerKeyPath, err)
	}
	return signer, nil
}

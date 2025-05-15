package signer

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

var (
	errMissingStakingSigningKeyFile = errors.New("missing staking signing key file")
)

// GetStakingSigner returns a BLS signer based on the provided configuration.
func GetStakingSigner(
	config interface{},
) (bls.Signer, error) {

	switch cfg := config.(type) {
	case node.EphemeralSignerConfig:
		signer, err := localsigner.New()
		if err != nil {
			return nil, fmt.Errorf("couldn't generate ephemeral signer: %w", err)
		}

		return signer, nil

	case node.ContentKeyConfig:
		signerKeyContent, err := base64.StdEncoding.DecodeString(cfg.SignerKeyRawContent)
		if err != nil {
			return nil, fmt.Errorf("unable to decode base64 content: %w", err)
		}

		signer, err := localsigner.FromBytes(signerKeyContent)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse signing key: %w", err)
		}

		return signer, nil

	case node.RPCSignerConfig:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		signer, err := rpcsigner.NewClient(ctx, cfg.StakingSignerRPC)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("couldn't create rpc signer client: %w", err)
		}

		return signer, nil

	case node.SignerPathConfig:
		_, err := os.Stat(cfg.SigningKeyPath)
		if !errors.Is(err, fs.ErrNotExist) {
			signingKeyBytes, err := os.ReadFile(cfg.SigningKeyPath)
			if err != nil {
				return nil, err
			}
			signer, err := localsigner.FromBytes(signingKeyBytes)
			if err != nil {
				return nil, fmt.Errorf("couldn't parse signing key: %w", err)
			}
			return signer, nil
		}

		if cfg.SignerPathIsSet {
			return nil, errMissingStakingSigningKeyFile
		}

		signer, err := localsigner.New()
		if err != nil {
			return nil, fmt.Errorf("couldn't generate new signing key: %w", err)
		}

		if err := os.MkdirAll(filepath.Dir(cfg.SigningKeyPath), perms.ReadWriteExecute); err != nil {
			return nil, fmt.Errorf("couldn't create path for signing key at %s: %w", cfg.SigningKeyPath, err)
		}

		keyBytes := signer.ToBytes()
		if err := os.WriteFile(cfg.SigningKeyPath, keyBytes, perms.ReadWrite); err != nil {
			return nil, fmt.Errorf("couldn't write new signing key to %s: %w", cfg.SigningKeyPath, err)
		}
		if err := os.Chmod(cfg.SigningKeyPath, perms.ReadOnly); err != nil {
			return nil, fmt.Errorf("couldn't restrict permissions on new signing key at %s: %w", cfg.SigningKeyPath, err)
		}
		return signer, nil

	default:
		return nil, fmt.Errorf("unsupported signer type: %T", cfg)
	}
}

func createSignerFromFile(signingKeyPath string) (bls.Signer, error) {
	signingKeyBytes, err := os.ReadFile(signingKeyPath)
	if err != nil {
		return nil, err
	}

	signer, err := localsigner.FromBytes(signingKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't parse signing key: %w", err)
	}

	return signer, nil
}

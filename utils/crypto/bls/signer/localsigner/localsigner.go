// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/perms"

	blst "github.com/supranational/blst/bindings/go"
)

var (
	ErrFailedSecretKeyDeserialize            = errors.New("couldn't deserialize secret key")
	_                             bls.Signer = (*LocalSigner)(nil)
)

type secretKey = blst.SecretKey

type LocalSigner struct {
	sk *secretKey
	pk *bls.PublicKey
}

// NewSecretKey generates a new secret key from the local source of
// cryptographically secure randomness.
func New() (*LocalSigner, error) {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	if err != nil {
		return nil, err
	}
	sk := blst.KeyGen(ikm[:])
	ikm = [32]byte{} // zero out the ikm
	pk := new(bls.PublicKey).From(sk)

	return &LocalSigner{sk: sk, pk: pk}, nil
}

// ToBytes returns the big-endian format of the secret key.
func (s *LocalSigner) ToBytes() []byte {
	return s.sk.Serialize()
}

// FromBytes parses the big-endian format of the secret key into a
// secret key.
func FromBytes(skBytes []byte) (*LocalSigner, error) {
	sk := new(secretKey).Deserialize(skBytes)
	if sk == nil {
		return nil, ErrFailedSecretKeyDeserialize
	}
	runtime.SetFinalizer(sk, func(sk *secretKey) {
		sk.Zeroize()
	})
	pk := new(bls.PublicKey).From(sk)

	return &LocalSigner{sk: sk, pk: pk}, nil
}

func FromFile(keyPath string) (bls.Signer, error) {
	signingKeyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("could not read signing key from %s: %w", keyPath, err)
	}

	signer, err := FromBytes(signingKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("could not parse signing key: %w", err)
	}

	return signer, nil
}

func (s *LocalSigner) ToFile(keyPath string) error {
	if err := os.MkdirAll(filepath.Dir(keyPath), perms.ReadWriteExecute); err != nil {
		return fmt.Errorf("could not create path for signing key at %s: %w", keyPath, err)
	}

	if err := os.WriteFile(
		keyPath,
		s.ToBytes(),
		perms.ReadWrite,
	); err != nil {
		return fmt.Errorf("could not write new signing key to %s: %w", keyPath, err)
	}

	if err := os.Chmod(keyPath, perms.ReadOnly); err != nil {
		return fmt.Errorf("could not restrict permissions on new signing key at %s: %w", keyPath, err)
	}

	return nil
}

func FromFileOrPersistNew(keyPath string) (bls.Signer, error) {
	_, err := os.Stat(keyPath)
	if !errors.Is(err, fs.ErrNotExist) {
		return FromFile(keyPath)
	}

	signer, err := New()
	if err != nil {
		return nil, fmt.Errorf("could not generate new signing key: %w", err)
	}

	if err := signer.ToFile(keyPath); err != nil {
		return nil, fmt.Errorf("could not persist new signer: %w", err)
	}

	return signer, nil
}

// PublicKey returns the public key that corresponds to this secret
// key.
func (s *LocalSigner) PublicKey() *bls.PublicKey {
	return s.pk
}

// Sign [msg] to authorize this message
func (s *LocalSigner) Sign(msg []byte) (*bls.Signature, error) {
	return new(bls.Signature).Sign(s.sk, msg, bls.CiphersuiteSignature.Bytes()), nil
}

// Sign [msg] to prove the ownership
func (s *LocalSigner) SignProofOfPossession(msg []byte) (*bls.Signature, error) {
	return new(bls.Signature).Sign(s.sk, msg, bls.CiphersuiteProofOfPossession.Bytes()), nil
}

func (*LocalSigner) Shutdown() error {
	return nil
}

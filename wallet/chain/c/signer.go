// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package c

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/libevm/common"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const version = 0

var (
	_ Signer = (*txSigner)(nil)

	errUnknownInputType      = errors.New("unknown input type")
	errUnknownCredentialType = errors.New("unknown credential type")
	errUnknownOutputType     = errors.New("unknown output type")
	errInvalidUTXOSigIndex   = errors.New("invalid UTXO signature index")

	emptySig [secp256k1.SignatureLen]byte
)

type Signer interface {
	// SignAtomic adds as many missing signatures as possible to the provided
	// transaction.
	//
	// If there are already some signatures on the transaction, those signatures
	// will not be removed.
	//
	// If the signer doesn't have the ability to provide a required signature,
	// the signature slot will be skipped without reporting an error.
	SignAtomic(ctx context.Context, tx *atomic.Tx) error
}

type EthKeychain interface {
	// The returned Signer can provide a signature for [addr]
	GetEth(addr common.Address) (keychain.Signer, bool)
	// Returns the set of addresses for which the accessor keeps an associated
	// signer
	EthAddresses() set.Set[common.Address]
}

type SignerBackend interface {
	GetUTXO(ctx context.Context, chainID, utxoID ids.ID) (*avax.UTXO, error)
}

type txSigner struct {
	avaxKC  keychain.Keychain
	ethKC   EthKeychain
	backend SignerBackend
}

func NewSigner(avaxKC keychain.Keychain, ethKC EthKeychain, backend SignerBackend) Signer {
	return &txSigner{
		avaxKC:  avaxKC,
		ethKC:   ethKC,
		backend: backend,
	}
}

func (s *txSigner) SignAtomic(ctx context.Context, tx *atomic.Tx) error {
	switch utx := tx.UnsignedAtomicTx.(type) {
	case *atomic.UnsignedImportTx:
		signers, err := s.getImportSigners(ctx, utx.SourceChain, utx.ImportedInputs)
		if err != nil {
			return err
		}
		return sign(tx, signers)
	case *atomic.UnsignedExportTx:
		signers := s.getExportSigners(utx.Ins)
		return sign(tx, signers)
	default:
		return fmt.Errorf("%w: %T", errUnknownTxType, tx)
	}
}

func (s *txSigner) getImportSigners(ctx context.Context, sourceChainID ids.ID, ins []*avax.TransferableInput) ([][]keychain.Signer, error) {
	txSigners := make([][]keychain.Signer, len(ins))
	for credIndex, transferInput := range ins {
		input, ok := transferInput.In.(*secp256k1fx.TransferInput)
		if !ok {
			return nil, errUnknownInputType
		}

		inputSigners := make([]keychain.Signer, len(input.SigIndices))
		txSigners[credIndex] = inputSigners

		utxoID := transferInput.InputID()
		utxo, err := s.backend.GetUTXO(ctx, sourceChainID, utxoID)
		if err == database.ErrNotFound {
			// If we don't have access to the UTXO, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		if err != nil {
			return nil, err
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, errUnknownOutputType
		}

		for sigIndex, addrIndex := range input.SigIndices {
			if addrIndex >= uint32(len(out.Addrs)) {
				return nil, errInvalidUTXOSigIndex
			}

			addr := out.Addrs[addrIndex]
			key, ok := s.avaxKC.Get(addr)
			if !ok {
				// If we don't have access to the key, then we can't sign this
				// transaction. However, we can attempt to partially sign it.
				continue
			}
			inputSigners[sigIndex] = key
		}
	}
	return txSigners, nil
}

func (s *txSigner) getExportSigners(ins []atomic.EVMInput) [][]keychain.Signer {
	txSigners := make([][]keychain.Signer, len(ins))
	for credIndex, input := range ins {
		inputSigners := make([]keychain.Signer, 1)
		txSigners[credIndex] = inputSigners

		key, ok := s.ethKC.GetEth(input.Address)
		if !ok {
			// If we don't have access to the key, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		inputSigners[0] = key
	}
	return txSigners
}

func SignUnsignedAtomic(ctx context.Context, signer Signer, utx atomic.UnsignedAtomicTx) (*atomic.Tx, error) {
	tx := &atomic.Tx{UnsignedAtomicTx: utx}
	return tx, signer.SignAtomic(ctx, tx)
}

func sign(tx *atomic.Tx, txSigners [][]keychain.Signer) error {
	unsignedBytes, err := atomic.Codec.Marshal(version, &tx.UnsignedAtomicTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal unsigned tx: %w", err)
	}

	if expectedLen := len(txSigners); expectedLen != len(tx.Creds) {
		tx.Creds = make([]verify.Verifiable, expectedLen)
	}

	sigCache := make(map[ids.ShortID][secp256k1.SignatureLen]byte)
	for credIndex, inputSigners := range txSigners {
		credIntf := tx.Creds[credIndex]
		if credIntf == nil {
			credIntf = &secp256k1fx.Credential{}
			tx.Creds[credIndex] = credIntf
		}

		cred, ok := credIntf.(*secp256k1fx.Credential)
		if !ok {
			return errUnknownCredentialType
		}
		if expectedLen := len(inputSigners); expectedLen != len(cred.Sigs) {
			cred.Sigs = make([][secp256k1.SignatureLen]byte, expectedLen)
		}

		for sigIndex, signer := range inputSigners {
			if signer == nil {
				// If we don't have access to the key, then we can't sign this
				// transaction. However, we can attempt to partially sign it.
				continue
			}
			addr := signer.Address()
			if sig := cred.Sigs[sigIndex]; sig != emptySig {
				// If this signature has already been populated, we can just
				// copy the needed signature for the future.
				sigCache[addr] = sig
				continue
			}

			if sig, exists := sigCache[addr]; exists {
				// If this key has already produced a signature, we can just
				// copy the previous signature.
				cred.Sigs[sigIndex] = sig
				continue
			}

			sig, err := signer.Sign(unsignedBytes, keychain.WithChainAlias(Alias))
			if err != nil {
				return fmt.Errorf("problem signing tx: %w", err)
			}
			copy(cred.Sigs[sigIndex][:], sig)
			sigCache[addr] = cred.Sigs[sigIndex]
		}
	}

	signedBytes, err := atomic.Codec.Marshal(version, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal tx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

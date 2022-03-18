// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"errors"
	"fmt"

	stdcontext "context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/hashing"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	errUnknownTxType         = errors.New("unknown tx type")
	errUnknownInputType      = errors.New("unknown input type")
	errUnknownCredentialType = errors.New("unknown credential type")
	errUnknownOutputType     = errors.New("unknown output type")
	errUnknownSubnetAuthType = errors.New("unknown subnet auth type")
	errInvalidUTXOSigIndex   = errors.New("invalid UTXO signature index")

	emptySig [crypto.SECP256K1RSigLen]byte

	_ Signer = &signer{}
)

type Signer interface {
	SignUnsigned(ctx stdcontext.Context, tx platformvm.UnsignedTx) (*platformvm.Tx, error)
	Sign(ctx stdcontext.Context, tx *platformvm.Tx) error
}

type SignerBackend interface {
	GetUTXO(ctx stdcontext.Context, chainID, utxoID ids.ID) (*avax.UTXO, error)
	GetTx(ctx stdcontext.Context, txID ids.ID) (*platformvm.Tx, error)
}

type signer struct {
	kc      *secp256k1fx.Keychain
	backend SignerBackend
}

func NewSigner(kc *secp256k1fx.Keychain, backend SignerBackend) Signer {
	return &signer{
		kc:      kc,
		backend: backend,
	}
}

func (s *signer) SignUnsigned(ctx stdcontext.Context, utx platformvm.UnsignedTx) (*platformvm.Tx, error) {
	tx := &platformvm.Tx{
		UnsignedTx: utx,
	}
	return tx, s.Sign(ctx, tx)
}

func (s *signer) Sign(ctx stdcontext.Context, tx *platformvm.Tx) error {
	switch utx := tx.UnsignedTx.(type) {
	case *platformvm.UnsignedAddValidatorTx:
		return s.signAddValidatorTx(ctx, tx, utx)
	case *platformvm.UnsignedAddSubnetValidatorTx:
		return s.signAddSubnetValidatorTx(ctx, tx, utx)
	case *platformvm.UnsignedAddDelegatorTx:
		return s.signAddDelegatorTx(ctx, tx, utx)
	case *platformvm.UnsignedCreateChainTx:
		return s.signCreateChainTx(ctx, tx, utx)
	case *platformvm.UnsignedCreateSubnetTx:
		return s.signCreateSubnetTx(ctx, tx, utx)
	case *platformvm.UnsignedImportTx:
		return s.signImportTx(ctx, tx, utx)
	case *platformvm.UnsignedExportTx:
		return s.signExportTx(ctx, tx, utx)
	default:
		return fmt.Errorf("%w: %T", errUnknownTxType, tx.UnsignedTx)
	}
}

func (s *signer) signAddValidatorTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedAddValidatorTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	return s.sign(tx, txSigners)
}

func (s *signer) signAddSubnetValidatorTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedAddSubnetValidatorTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	subnetAuthSigners, err := s.getSubnetSigners(ctx, utx.Validator.Subnet, utx.SubnetAuth)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, subnetAuthSigners)
	return s.sign(tx, txSigners)
}

func (s *signer) signAddDelegatorTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedAddDelegatorTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	return s.sign(tx, txSigners)
}

func (s *signer) signCreateChainTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedCreateChainTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	subnetAuthSigners, err := s.getSubnetSigners(ctx, utx.SubnetID, utx.SubnetAuth)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, subnetAuthSigners)
	return s.sign(tx, txSigners)
}

func (s *signer) signCreateSubnetTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedCreateSubnetTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	return s.sign(tx, txSigners)
}

func (s *signer) signImportTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedImportTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	txImportSigners, err := s.getSigners(ctx, utx.SourceChain, utx.ImportedInputs)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, txImportSigners...)
	return s.sign(tx, txSigners)
}

func (s *signer) signExportTx(ctx stdcontext.Context, tx *platformvm.Tx, utx *platformvm.UnsignedExportTx) error {
	txSigners, err := s.getSigners(ctx, constants.PlatformChainID, utx.Ins)
	if err != nil {
		return err
	}
	return s.sign(tx, txSigners)
}

func (s *signer) getSigners(ctx stdcontext.Context, sourceChainID ids.ID, ins []*avax.TransferableInput) ([][]*crypto.PrivateKeySECP256K1R, error) {
	txSigners := make([][]*crypto.PrivateKeySECP256K1R, len(ins))
	for credIndex, transferInput := range ins {
		input, ok := transferInput.In.(*secp256k1fx.TransferInput)
		if !ok {
			return nil, errUnknownInputType
		}

		inputSigners := make([]*crypto.PrivateKeySECP256K1R, len(input.SigIndices))
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

		outIntf := utxo.Out
		if stakeableOut, ok := outIntf.(*platformvm.StakeableLockOut); ok {
			outIntf = stakeableOut.TransferableOut
		}

		out, ok := outIntf.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, errUnknownOutputType
		}

		for sigIndex, addrIndex := range input.SigIndices {
			if addrIndex >= uint32(len(out.Addrs)) {
				return nil, errInvalidUTXOSigIndex
			}

			addr := out.Addrs[addrIndex]
			key, ok := s.kc.Get(addr)
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

func (s *signer) getSubnetSigners(ctx stdcontext.Context, subnetID ids.ID, subnetAuth verify.Verifiable) ([]*crypto.PrivateKeySECP256K1R, error) {
	subnetInput, ok := subnetAuth.(*secp256k1fx.Input)
	if !ok {
		return nil, errUnknownSubnetAuthType
	}

	subnetTx, err := s.backend.GetTx(ctx, subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch subnet %q: %w",
			subnetID,
			err,
		)
	}
	subnet, ok := subnetTx.UnsignedTx.(*platformvm.UnsignedCreateSubnetTx)
	if !ok {
		return nil, errWrongTxType
	}

	owner, ok := subnet.Owner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, errUnknownOwnerType
	}

	authSigners := make([]*crypto.PrivateKeySECP256K1R, len(subnetInput.SigIndices))
	for sigIndex, addrIndex := range subnetInput.SigIndices {
		if addrIndex >= uint32(len(owner.Addrs)) {
			return nil, errInvalidUTXOSigIndex
		}

		addr := owner.Addrs[addrIndex]
		key, ok := s.kc.Get(addr)
		if !ok {
			// If we don't have access to the key, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		authSigners[sigIndex] = key
	}
	return authSigners, nil
}

func (s *signer) sign(tx *platformvm.Tx, txSigners [][]*crypto.PrivateKeySECP256K1R) error {
	unsignedBytes, err := platformvm.Codec.Marshal(platformvm.CodecVersion, &tx.UnsignedTx)
	if err != nil {
		return fmt.Errorf("couldn't marshal unsigned tx: %w", err)
	}
	unsignedHash := hashing.ComputeHash256(unsignedBytes)

	if expectedLen := len(txSigners); expectedLen != len(tx.Creds) {
		tx.Creds = make([]verify.Verifiable, expectedLen)
	}

	sigCache := make(map[ids.ShortID][crypto.SECP256K1RSigLen]byte)
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
			cred.Sigs = make([][crypto.SECP256K1RSigLen]byte, expectedLen)
		}

		for sigIndex, signer := range inputSigners {
			if signer == nil {
				// If we don't have access to the key, then we can't sign this
				// transaction. However, we can attempt to partially sign it.
				continue
			}
			addr := signer.PublicKey().Address()
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

			sig, err := signer.SignHash(unsignedHash)
			if err != nil {
				return fmt.Errorf("problem signing tx: %w", err)
			}
			copy(cred.Sigs[sigIndex][:], sig)
			sigCache[addr] = cred.Sigs[sigIndex]
		}
	}

	signedBytes, err := platformvm.Codec.Marshal(platformvm.CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal tx: %w", err)
	}
	tx.Initialize(unsignedBytes, signedBytes)
	return nil
}

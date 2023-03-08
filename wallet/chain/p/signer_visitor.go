// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"errors"
	"fmt"

	stdcontext "context"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/platformvm/stakeable"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

var (
	_ txs.Visitor = (*signerVisitor)(nil)

	errUnsupportedTxType     = errors.New("unsupported tx type")
	errUnknownInputType      = errors.New("unknown input type")
	errUnknownCredentialType = errors.New("unknown credential type")
	errUnknownOutputType     = errors.New("unknown output type")
	errUnknownSubnetAuthType = errors.New("unknown subnet auth type")
	errInvalidUTXOSigIndex   = errors.New("invalid UTXO signature index")

	emptySig [secp256k1.SignatureLen]byte
)

// signerVisitor handles signing transactions for the signer
type signerVisitor struct {
	kc      keychain.Keychain
	backend SignerBackend
	ctx     stdcontext.Context
	tx      *txs.Tx
}

func (*signerVisitor) AdvanceTimeTx(*txs.AdvanceTimeTx) error {
	return errUnsupportedTxType
}

func (*signerVisitor) RewardValidatorTx(*txs.RewardValidatorTx) error {
	return errUnsupportedTxType
}

func (s *signerVisitor) AddValidatorTx(tx *txs.AddValidatorTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) AddSubnetValidatorTx(tx *txs.AddSubnetValidatorTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	subnetAuthSigners, err := s.getSubnetSigners(tx.SubnetValidator.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, subnetAuthSigners)
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) AddDelegatorTx(tx *txs.AddDelegatorTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) CreateChainTx(tx *txs.CreateChainTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	subnetAuthSigners, err := s.getSubnetSigners(tx.SubnetID, tx.SubnetAuth)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, subnetAuthSigners)
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) CreateSubnetTx(tx *txs.CreateSubnetTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) ImportTx(tx *txs.ImportTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	txImportSigners, err := s.getSigners(tx.SourceChain, tx.ImportedInputs)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, txImportSigners...)
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) ExportTx(tx *txs.ExportTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) RemoveSubnetValidatorTx(tx *txs.RemoveSubnetValidatorTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	subnetAuthSigners, err := s.getSubnetSigners(tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, subnetAuthSigners)
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) TransformSubnetTx(tx *txs.TransformSubnetTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	subnetAuthSigners, err := s.getSubnetSigners(tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}
	txSigners = append(txSigners, subnetAuthSigners)
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) AddPermissionlessValidatorTx(tx *txs.AddPermissionlessValidatorTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) AddPermissionlessDelegatorTx(tx *txs.AddPermissionlessDelegatorTx) error {
	txSigners, err := s.getSigners(constants.PlatformChainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txSigners)
}

func (s *signerVisitor) getSigners(sourceChainID ids.ID, ins []*avax.TransferableInput) ([][]keychain.Signer, error) {
	txSigners := make([][]keychain.Signer, len(ins))
	for credIndex, transferInput := range ins {
		inIntf := transferInput.In
		if stakeableIn, ok := inIntf.(*stakeable.LockIn); ok {
			inIntf = stakeableIn.TransferableIn
		}

		input, ok := inIntf.(*secp256k1fx.TransferInput)
		if !ok {
			return nil, errUnknownInputType
		}

		inputSigners := make([]keychain.Signer, len(input.SigIndices))
		txSigners[credIndex] = inputSigners

		utxoID := transferInput.InputID()
		utxo, err := s.backend.GetUTXO(s.ctx, sourceChainID, utxoID)
		if err == database.ErrNotFound {
			// If we don't have access to the UTXO, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		if err != nil {
			return nil, err
		}

		outIntf := utxo.Out
		if stakeableOut, ok := outIntf.(*stakeable.LockOut); ok {
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

func (s *signerVisitor) getSubnetSigners(subnetID ids.ID, subnetAuth verify.Verifiable) ([]keychain.Signer, error) {
	subnetInput, ok := subnetAuth.(*secp256k1fx.Input)
	if !ok {
		return nil, errUnknownSubnetAuthType
	}

	subnetTx, err := s.backend.GetTx(s.ctx, subnetID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch subnet %q: %w",
			subnetID,
			err,
		)
	}
	subnet, ok := subnetTx.Unsigned.(*txs.CreateSubnetTx)
	if !ok {
		return nil, errWrongTxType
	}

	owner, ok := subnet.Owner.(*secp256k1fx.OutputOwners)
	if !ok {
		return nil, errUnknownOwnerType
	}

	authSigners := make([]keychain.Signer, len(subnetInput.SigIndices))
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

func sign(tx *txs.Tx, txSigners [][]keychain.Signer) error {
	unsignedBytes, err := txs.Codec.Marshal(txs.Version, &tx.Unsigned)
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

			sig, err := signer.Sign(unsignedBytes)
			if err != nil {
				return fmt.Errorf("problem signing tx: %w", err)
			}
			copy(cred.Sigs[sigIndex][:], sig)
			sigCache[addr] = cred.Sigs[sigIndex]
		}
	}

	signedBytes, err := txs.Codec.Marshal(txs.Version, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal tx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}

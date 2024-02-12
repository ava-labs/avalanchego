// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package x

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/keychain"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/nftfx"
	"github.com/ava-labs/avalanchego/vms/propertyfx"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	stdcontext "context"
)

var (
	_ txs.Visitor = (*signerVisitor)(nil)

	errUnknownInputType      = errors.New("unknown input type")
	errUnknownOpType         = errors.New("unknown operation type")
	errInvalidNumUTXOsInOp   = errors.New("invalid number of UTXOs in operation")
	errUnknownCredentialType = errors.New("unknown credential type")
	errUnknownOutputType     = errors.New("unknown output type")
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

func (s *signerVisitor) BaseTx(tx *txs.BaseTx) error {
	txCreds, txSigners, err := s.getSigners(s.ctx, tx.BlockchainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txCreds, txSigners)
}

func (s *signerVisitor) CreateAssetTx(tx *txs.CreateAssetTx) error {
	txCreds, txSigners, err := s.getSigners(s.ctx, tx.BlockchainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txCreds, txSigners)
}

func (s *signerVisitor) OperationTx(tx *txs.OperationTx) error {
	txCreds, txSigners, err := s.getSigners(s.ctx, tx.BlockchainID, tx.Ins)
	if err != nil {
		return err
	}
	txOpsCreds, txOpsSigners, err := s.getOpsSigners(s.ctx, tx.BlockchainID, tx.Ops)
	if err != nil {
		return err
	}
	txCreds = append(txCreds, txOpsCreds...)
	txSigners = append(txSigners, txOpsSigners...)
	return sign(s.tx, txCreds, txSigners)
}

func (s *signerVisitor) ImportTx(tx *txs.ImportTx) error {
	txCreds, txSigners, err := s.getSigners(s.ctx, tx.BlockchainID, tx.Ins)
	if err != nil {
		return err
	}
	txImportCreds, txImportSigners, err := s.getSigners(s.ctx, tx.SourceChain, tx.ImportedIns)
	if err != nil {
		return err
	}
	txCreds = append(txCreds, txImportCreds...)
	txSigners = append(txSigners, txImportSigners...)
	return sign(s.tx, txCreds, txSigners)
}

func (s *signerVisitor) ExportTx(tx *txs.ExportTx) error {
	txCreds, txSigners, err := s.getSigners(s.ctx, tx.BlockchainID, tx.Ins)
	if err != nil {
		return err
	}
	return sign(s.tx, txCreds, txSigners)
}

func (s *signerVisitor) getSigners(ctx stdcontext.Context, sourceChainID ids.ID, ins []*avax.TransferableInput) ([]verify.Verifiable, [][]keychain.Signer, error) {
	txCreds := make([]verify.Verifiable, len(ins))
	txSigners := make([][]keychain.Signer, len(ins))
	for credIndex, transferInput := range ins {
		txCreds[credIndex] = &secp256k1fx.Credential{}
		input, ok := transferInput.In.(*secp256k1fx.TransferInput)
		if !ok {
			return nil, nil, errUnknownInputType
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
			return nil, nil, err
		}

		out, ok := utxo.Out.(*secp256k1fx.TransferOutput)
		if !ok {
			return nil, nil, errUnknownOutputType
		}

		for sigIndex, addrIndex := range input.SigIndices {
			if addrIndex >= uint32(len(out.Addrs)) {
				return nil, nil, errInvalidUTXOSigIndex
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
	return txCreds, txSigners, nil
}

func (s *signerVisitor) getOpsSigners(ctx stdcontext.Context, sourceChainID ids.ID, ops []*txs.Operation) ([]verify.Verifiable, [][]keychain.Signer, error) {
	txCreds := make([]verify.Verifiable, len(ops))
	txSigners := make([][]keychain.Signer, len(ops))
	for credIndex, op := range ops {
		var input *secp256k1fx.Input
		switch op := op.Op.(type) {
		case *secp256k1fx.MintOperation:
			txCreds[credIndex] = &secp256k1fx.Credential{}
			input = &op.MintInput
		case *nftfx.MintOperation:
			txCreds[credIndex] = &nftfx.Credential{}
			input = &op.MintInput
		case *nftfx.TransferOperation:
			txCreds[credIndex] = &nftfx.Credential{}
			input = &op.Input
		case *propertyfx.MintOperation:
			txCreds[credIndex] = &propertyfx.Credential{}
			input = &op.MintInput
		case *propertyfx.BurnOperation:
			txCreds[credIndex] = &propertyfx.Credential{}
			input = &op.Input
		default:
			return nil, nil, errUnknownOpType
		}

		inputSigners := make([]keychain.Signer, len(input.SigIndices))
		txSigners[credIndex] = inputSigners

		if len(op.UTXOIDs) != 1 {
			return nil, nil, errInvalidNumUTXOsInOp
		}
		utxoID := op.UTXOIDs[0].InputID()
		utxo, err := s.backend.GetUTXO(ctx, sourceChainID, utxoID)
		if err == database.ErrNotFound {
			// If we don't have access to the UTXO, then we can't sign this
			// transaction. However, we can attempt to partially sign it.
			continue
		}
		if err != nil {
			return nil, nil, err
		}

		var addrs []ids.ShortID
		switch out := utxo.Out.(type) {
		case *secp256k1fx.MintOutput:
			addrs = out.Addrs
		case *nftfx.MintOutput:
			addrs = out.Addrs
		case *nftfx.TransferOutput:
			addrs = out.Addrs
		case *propertyfx.MintOutput:
			addrs = out.Addrs
		case *propertyfx.OwnedOutput:
			addrs = out.Addrs
		default:
			return nil, nil, errUnknownOutputType
		}

		for sigIndex, addrIndex := range input.SigIndices {
			if addrIndex >= uint32(len(addrs)) {
				return nil, nil, errInvalidUTXOSigIndex
			}

			addr := addrs[addrIndex]
			key, ok := s.kc.Get(addr)
			if !ok {
				// If we don't have access to the key, then we can't sign this
				// transaction. However, we can attempt to partially sign it.
				continue
			}
			inputSigners[sigIndex] = key
		}
	}
	return txCreds, txSigners, nil
}

func sign(tx *txs.Tx, creds []verify.Verifiable, txSigners [][]keychain.Signer) error {
	codec := Parser.Codec()
	unsignedBytes, err := codec.Marshal(txs.CodecVersion, &tx.Unsigned)
	if err != nil {
		return fmt.Errorf("couldn't marshal unsigned tx: %w", err)
	}

	if expectedLen := len(txSigners); expectedLen != len(tx.Creds) {
		tx.Creds = make([]*fxs.FxCredential, expectedLen)
	}

	sigCache := make(map[ids.ShortID][secp256k1.SignatureLen]byte)
	for credIndex, inputSigners := range txSigners {
		fxCred := tx.Creds[credIndex]
		if fxCred == nil {
			fxCred = &fxs.FxCredential{}
			tx.Creds[credIndex] = fxCred
		}
		credIntf := fxCred.Credential
		if credIntf == nil {
			credIntf = creds[credIndex]
			fxCred.Credential = credIntf
		}

		var cred *secp256k1fx.Credential
		switch credImpl := credIntf.(type) {
		case *secp256k1fx.Credential:
			fxCred.FxID = secp256k1fx.ID
			cred = credImpl
		case *nftfx.Credential:
			fxCred.FxID = nftfx.ID
			cred = &credImpl.Credential
		case *propertyfx.Credential:
			fxCred.FxID = propertyfx.ID
			cred = &credImpl.Credential
		default:
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

	signedBytes, err := codec.Marshal(txs.CodecVersion, tx)
	if err != nil {
		return fmt.Errorf("couldn't marshal tx: %w", err)
	}
	tx.SetBytes(unsignedBytes, signedBytes)
	return nil
}

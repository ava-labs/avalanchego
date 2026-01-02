// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/components/avax"
)

const (
	minNameLen      = 1
	maxNameLen      = 128
	minSymbolLen    = 1
	maxSymbolLen    = 4
	maxDenomination = 32
)

var (
	_ txs.Visitor = (*SyntacticVerifier)(nil)

	errWrongNumberOfCredentials     = errors.New("wrong number of credentials")
	errInitialStatesNotSortedUnique = errors.New("initial states not sorted and unique")
	errNameTooShort                 = fmt.Errorf("name is too short, minimum size is %d", minNameLen)
	errNameTooLong                  = fmt.Errorf("name is too long, maximum size is %d", maxNameLen)
	errSymbolTooShort               = fmt.Errorf("symbol is too short, minimum size is %d", minSymbolLen)
	errSymbolTooLong                = fmt.Errorf("symbol is too long, maximum size is %d", maxSymbolLen)
	errNoFxs                        = errors.New("assets must support at least one Fx")
	errIllegalNameCharacter         = errors.New("asset's name must be made up of only letters and numbers")
	errIllegalSymbolCharacter       = errors.New("asset's symbol must be all upper case letters")
	errUnexpectedWhitespace         = errors.New("unexpected whitespace provided")
	errDenominationTooLarge         = errors.New("denomination is too large")
	errOperationsNotSortedUnique    = errors.New("operations not sorted and unique")
	errNoOperations                 = errors.New("an operationTx must have at least one operation")
	errDoubleSpend                  = errors.New("inputs attempt to double spend an input")
	errNoImportInputs               = errors.New("no import inputs")
	errNoExportOutputs              = errors.New("no export outputs")
)

type SyntacticVerifier struct {
	*Backend
	Tx *txs.Tx
}

func (v *SyntacticVerifier) BaseTx(tx *txs.BaseTx) error {
	if err := tx.BaseTx.Verify(v.Ctx); err != nil {
		return err
	}

	err := avax.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*avax.TransferableInput{tx.Ins},
		[][]*avax.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) CreateAssetTx(tx *txs.CreateAssetTx) error {
	switch {
	case len(tx.Name) < minNameLen:
		return errNameTooShort
	case len(tx.Name) > maxNameLen:
		return errNameTooLong
	case len(tx.Symbol) < minSymbolLen:
		return errSymbolTooShort
	case len(tx.Symbol) > maxSymbolLen:
		return errSymbolTooLong
	case len(tx.States) == 0:
		return errNoFxs
	case tx.Denomination > maxDenomination:
		return errDenominationTooLarge
	case strings.TrimSpace(tx.Name) != tx.Name:
		return errUnexpectedWhitespace
	}

	for _, r := range tx.Name {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsNumber(r) && r != ' ') {
			return errIllegalNameCharacter
		}
	}
	for _, r := range tx.Symbol {
		if r > unicode.MaxASCII || !unicode.IsUpper(r) {
			return errIllegalSymbolCharacter
		}
	}

	if err := tx.BaseTx.BaseTx.Verify(v.Ctx); err != nil {
		return err
	}

	err := avax.VerifyTx(
		v.Config.CreateAssetTxFee,
		v.FeeAssetID,
		[][]*avax.TransferableInput{tx.Ins},
		[][]*avax.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, state := range tx.States {
		if err := state.Verify(v.Codec, len(v.Fxs)); err != nil {
			return err
		}
	}
	if !utils.IsSortedAndUnique(tx.States) {
		return errInitialStatesNotSortedUnique
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) OperationTx(tx *txs.OperationTx) error {
	if len(tx.Ops) == 0 {
		return errNoOperations
	}

	if err := tx.BaseTx.BaseTx.Verify(v.Ctx); err != nil {
		return err
	}

	err := avax.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*avax.TransferableInput{tx.Ins},
		[][]*avax.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	inputs := set.NewSet[ids.ID](len(tx.Ins))
	for _, in := range tx.Ins {
		inputs.Add(in.InputID())
	}

	for _, op := range tx.Ops {
		if err := op.Verify(); err != nil {
			return err
		}
		for _, utxoID := range op.UTXOIDs {
			inputID := utxoID.InputID()
			if inputs.Contains(inputID) {
				return errDoubleSpend
			}
			inputs.Add(inputID)
		}
	}
	if !txs.IsSortedAndUniqueOperations(tx.Ops, v.Codec) {
		return errOperationsNotSortedUnique
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins) + len(tx.Ops)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) ImportTx(tx *txs.ImportTx) error {
	if len(tx.ImportedIns) == 0 {
		return errNoImportInputs
	}

	if err := tx.BaseTx.BaseTx.Verify(v.Ctx); err != nil {
		return err
	}

	err := avax.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*avax.TransferableInput{
			tx.Ins,
			tx.ImportedIns,
		},
		[][]*avax.TransferableOutput{tx.Outs},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins) + len(tx.ImportedIns)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

func (v *SyntacticVerifier) ExportTx(tx *txs.ExportTx) error {
	if len(tx.ExportedOuts) == 0 {
		return errNoExportOutputs
	}

	if err := tx.BaseTx.BaseTx.Verify(v.Ctx); err != nil {
		return err
	}

	err := avax.VerifyTx(
		v.Config.TxFee,
		v.FeeAssetID,
		[][]*avax.TransferableInput{tx.Ins},
		[][]*avax.TransferableOutput{
			tx.Outs,
			tx.ExportedOuts,
		},
		v.Codec,
	)
	if err != nil {
		return err
	}

	for _, cred := range v.Tx.Creds {
		if err := cred.Verify(); err != nil {
			return err
		}
	}

	numCreds := len(v.Tx.Creds)
	numInputs := len(tx.Ins)
	if numCreds != numInputs {
		return fmt.Errorf("%w: %d != %d",
			errWrongNumberOfCredentials,
			numCreds,
			numInputs,
		)
	}

	return nil
}

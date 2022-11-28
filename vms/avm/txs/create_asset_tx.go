// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
)

const (
	minNameLen      = 1
	maxNameLen      = 128
	minSymbolLen    = 1
	maxSymbolLen    = 4
	maxDenomination = 32
)

var (
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

	_ UnsignedTx             = (*CreateAssetTx)(nil)
	_ secp256k1fx.UnsignedTx = (*CreateAssetTx)(nil)
)

// CreateAssetTx is a transaction that creates a new asset.
type CreateAssetTx struct {
	BaseTx       `serialize:"true"`
	Name         string          `serialize:"true" json:"name"`
	Symbol       string          `serialize:"true" json:"symbol"`
	Denomination byte            `serialize:"true" json:"denomination"`
	States       []*InitialState `serialize:"true" json:"initialStates"`
}

func (t *CreateAssetTx) InitCtx(ctx *snow.Context) {
	for _, state := range t.States {
		state.InitCtx(ctx)
	}
	t.BaseTx.InitCtx(ctx)
}

// InitialStates track which virtual machines, and the initial state of these
// machines, this asset uses. The returned array should not be modified.
func (t *CreateAssetTx) InitialStates() []*InitialState {
	return t.States
}

func (t *CreateAssetTx) SyntacticVerify(
	ctx *snow.Context,
	c codec.Manager,
	txFeeAssetID ids.ID,
	_ uint64,
	txFee uint64,
	numFxs int,
) error {
	switch {
	case t == nil:
		return errNilTx
	case len(t.Name) < minNameLen:
		return errNameTooShort
	case len(t.Name) > maxNameLen:
		return errNameTooLong
	case len(t.Symbol) < minSymbolLen:
		return errSymbolTooShort
	case len(t.Symbol) > maxSymbolLen:
		return errSymbolTooLong
	case len(t.States) == 0:
		return errNoFxs
	case t.Denomination > maxDenomination:
		return errDenominationTooLarge
	case strings.TrimSpace(t.Name) != t.Name:
		return errUnexpectedWhitespace
	}

	for _, r := range t.Name {
		if r > unicode.MaxASCII || !(unicode.IsLetter(r) || unicode.IsNumber(r) || r == ' ') {
			return errIllegalNameCharacter
		}
	}
	for _, r := range t.Symbol {
		if r > unicode.MaxASCII || !unicode.IsUpper(r) {
			return errIllegalSymbolCharacter
		}
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, txFeeAssetID, txFee, txFee, numFxs); err != nil {
		return err
	}

	for _, state := range t.States {
		if err := state.Verify(c, numFxs); err != nil {
			return err
		}
	}
	if !utils.IsSortedAndUniqueSortable(t.States) {
		return errInitialStatesNotSortedUnique
	}

	return nil
}

func (t *CreateAssetTx) Visit(v Visitor) error {
	return v.CreateAssetTx(t)
}

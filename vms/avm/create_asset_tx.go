// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/vms/components/codec"
)

const (
	maxNameLen      = 128
	maxSymbolLen    = 4
	maxDenomination = 32
)

var (
	errInitialStatesNotSortedUnique = errors.New("initial states not sorted and unique")
	errNameTooLong                  = fmt.Errorf("name is too long, maximum size is %d", maxNameLen)
	errSymbolTooLong                = fmt.Errorf("symbol is too long, maximum size is %d", maxSymbolLen)
	errNoFxs                        = errors.New("assets must support at least one Fx")
	errUnprintableASCIICharacter    = errors.New("unprintable ascii character was provided")
	errUnexpectedWhitespace         = errors.New("unexpected whitespace provided")
	errDenominationTooLarge         = errors.New("denomination is too large")
)

// CreateAssetTx is a transaction that creates a new asset.
type CreateAssetTx struct {
	BaseTx       `serialize:"true"`
	Name         string          `serialize:"true"`
	Symbol       string          `serialize:"true"`
	Denomination byte            `serialize:"true"`
	States       []*InitialState `serialize:"true"`
}

// InitialStates track which virtual machines, and the initial state of these
// machines, this asset uses. The returned array should not be modified.
func (t *CreateAssetTx) InitialStates() []*InitialState { return t.States }

// UTXOs returns the UTXOs transaction is producing.
func (t *CreateAssetTx) UTXOs() []*UTXO {
	txID := t.ID()
	utxos := t.BaseTx.UTXOs()

	for _, state := range t.States {
		for _, out := range state.Outs {
			utxos = append(utxos, &UTXO{
				UTXOID: UTXOID{
					TxID:        txID,
					OutputIndex: uint32(len(utxos)),
				},
				Asset: Asset{
					ID: txID,
				},
				Out: out,
			})
		}
	}

	return utxos
}

// SyntacticVerify that this transaction is well-formed.
func (t *CreateAssetTx) SyntacticVerify(ctx *snow.Context, c codec.Codec, numFxs int) error {
	switch {
	case t == nil:
		return errNilTx
	case len(t.Name) > maxNameLen:
		return errNameTooLong
	case len(t.Symbol) > maxSymbolLen:
		return errSymbolTooLong
	case len(t.States) == 0:
		return errNoFxs
	case t.Denomination > maxDenomination:
		return errDenominationTooLarge
	case strings.TrimSpace(t.Name) != t.Name:
		return errUnexpectedWhitespace
	case strings.TrimSpace(t.Symbol) != t.Symbol:
		return errUnexpectedWhitespace
	}

	for _, r := range t.Name {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return errUnprintableASCIICharacter
		}
	}
	for _, r := range t.Symbol {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return errUnprintableASCIICharacter
		}
	}

	if err := t.BaseTx.SyntacticVerify(ctx, c, numFxs); err != nil {
		return err
	}

	for _, state := range t.States {
		if err := state.Verify(c, numFxs); err != nil {
			return err
		}
	}
	if !isSortedAndUniqueInitialStates(t.States) {
		return errInitialStatesNotSortedUnique
	}
	return nil
}

// Sort ...
func (t *CreateAssetTx) Sort() { sortInitialStates(t.States) }

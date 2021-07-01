// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avax

import (
	"errors"

	"github.com/ava-labs/avalanchego/vms/components/verify"
)

var (
	errNilUTXO   = errors.New("nil utxo is not valid")
	errEmptyUTXO = errors.New("empty utxo is not valid")
)

// UTXO ...
type UTXO struct {
	UTXOID `serialize:"true"`
	Asset  `serialize:"true"`
	FxID   string       `serialize:"true" json:"fxID"`
	Out    verify.State `serialize:"true" json:"output"`
}

// Verify implements the verify.Verifiable interface
func (utxo *UTXO) Verify() error {
	switch {
	case utxo == nil:
		return errNilUTXO
	case utxo.Out == nil:
		return errEmptyUTXO
	default:
		return verify.All(&utxo.UTXOID, &utxo.Asset, utxo.Out)
	}
}

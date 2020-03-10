// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avalanche

import (
	"errors"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowstorm"
	"github.com/ava-labs/gecko/snow/engine/common"
)

var (
	errParseTx = errors.New("unexpectedly called ParseTx")
	errIssueTx = errors.New("unexpectedly called IssueTx")
	errGetTx   = errors.New("unexpectedly called GetTx")
)

// VMTest ...
type VMTest struct {
	common.VMTest

	CantPendingTxs, CantParseTx, CantIssueTx, CantGetTx bool

	PendingTxsF func() []snowstorm.Tx
	ParseTxF    func([]byte) (snowstorm.Tx, error)
	IssueTxF    func([]byte, func(choices.Status), func(choices.Status)) (ids.ID, error)
	GetTxF      func(ids.ID) (snowstorm.Tx, error)
}

// Default ...
func (vm *VMTest) Default(cant bool) {
	vm.VMTest.Default(cant)

	vm.CantPendingTxs = cant
	vm.CantParseTx = cant
	vm.CantIssueTx = cant
	vm.CantGetTx = cant
}

// PendingTxs ...
func (vm *VMTest) PendingTxs() []snowstorm.Tx {
	if vm.PendingTxsF != nil {
		return vm.PendingTxsF()
	}
	if vm.CantPendingTxs && vm.T != nil {
		vm.T.Fatalf("Unexpectedly called PendingTxs")
	}
	return nil
}

// ParseTx ...
func (vm *VMTest) ParseTx(b []byte) (snowstorm.Tx, error) {
	if vm.ParseTxF != nil {
		return vm.ParseTxF(b)
	}
	if vm.CantParseTx && vm.T != nil {
		vm.T.Fatal(errParseTx)
	}
	return nil, errParseTx
}

// IssueTx ...
func (vm *VMTest) IssueTx(b []byte, issued, finalized func(choices.Status)) (ids.ID, error) {
	if vm.IssueTxF != nil {
		return vm.IssueTxF(b, issued, finalized)
	}
	if vm.CantIssueTx && vm.T != nil {
		vm.T.Fatal(errIssueTx)
	}
	return ids.ID{}, errIssueTx
}

// GetTx ...
func (vm *VMTest) GetTx(txID ids.ID) (snowstorm.Tx, error) {
	if vm.GetTxF != nil {
		return vm.GetTxF(txID)
	}
	if vm.CantGetTx && vm.T != nil {
		vm.T.Fatal(errGetTx)
	}
	return nil, errGetTx
}

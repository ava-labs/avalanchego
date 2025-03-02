// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"reflect"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/avm/fxs"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
)

var _ txs.Visitor = (*txInit)(nil)

// txInit initializes FxID where required
type txInit struct {
	tx            *txs.Tx
	ctx           *snow.Context
	typeToFxIndex map[reflect.Type]int
	fxs           []*fxs.ParsedFx
}

func (t *txInit) getFx(val interface{}) (int, error) {
	valType := reflect.TypeOf(val)
	fx, exists := t.typeToFxIndex[valType]
	if !exists {
		return 0, errUnknownFx
	}
	return fx, nil
}

// getParsedFx returns the parsedFx object for a given TransferableInput
// or TransferableOutput object
func (t *txInit) getParsedFx(val interface{}) (*fxs.ParsedFx, error) {
	idx, err := t.getFx(val)
	if err != nil {
		return nil, err
	}
	return t.fxs[idx], nil
}

func (t *txInit) init() error {
	t.tx.Unsigned.InitCtx(t.ctx)

	for _, cred := range t.tx.Creds {
		fx, err := t.getParsedFx(cred.Credential)
		if err != nil {
			return err
		}
		cred.FxID = fx.ID
	}
	return nil
}

func (t *txInit) BaseTx(tx *txs.BaseTx) error {
	if err := t.init(); err != nil {
		return err
	}

	for _, in := range tx.Ins {
		fx, err := t.getParsedFx(in.In)
		if err != nil {
			return err
		}
		in.FxID = fx.ID
	}

	for _, out := range tx.Outs {
		fx, err := t.getParsedFx(out.Out)
		if err != nil {
			return err
		}
		out.FxID = fx.ID
	}
	return nil
}

func (t *txInit) CreateAssetTx(tx *txs.CreateAssetTx) error {
	if err := t.init(); err != nil {
		return err
	}

	for _, state := range tx.States {
		fx := t.fxs[state.FxIndex]
		state.FxID = fx.ID
	}

	for _, in := range tx.Ins {
		fx, err := t.getParsedFx(in.In)
		if err != nil {
			return err
		}
		in.FxID = fx.ID
	}
	return t.BaseTx(&tx.BaseTx)
}

func (t *txInit) ImportTx(tx *txs.ImportTx) error {
	if err := t.init(); err != nil {
		return err
	}

	for _, in := range tx.ImportedIns {
		fx, err := t.getParsedFx(in.In)
		if err != nil {
			return err
		}
		in.FxID = fx.ID
	}
	return t.BaseTx(&tx.BaseTx)
}

func (t *txInit) ExportTx(tx *txs.ExportTx) error {
	if err := t.init(); err != nil {
		return err
	}

	for _, out := range tx.ExportedOuts {
		fx, err := t.getParsedFx(out.Out)
		if err != nil {
			return err
		}
		out.FxID = fx.ID
	}
	return t.BaseTx(&tx.BaseTx)
}

func (t *txInit) OperationTx(tx *txs.OperationTx) error {
	if err := t.init(); err != nil {
		return err
	}

	for _, op := range tx.Ops {
		fx, err := t.getParsedFx(op.Op)
		if err != nil {
			return err
		}
		op.FxID = fx.ID
	}
	return t.BaseTx(&tx.BaseTx)
}

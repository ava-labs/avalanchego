// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txs

import (
	"testing"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/codec/linearcodec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
)

type testOperable struct {
	avax.TestTransferable `serialize:"true"`

	Outputs []verify.State `serialize:"true"`
}

func (*testOperable) InitCtx(*snow.Context) {}

func (o *testOperable) Outs() []verify.State {
	return o.Outputs
}

func TestOperationVerifyNil(t *testing.T) {
	op := (*Operation)(nil)
	if err := op.Verify(); err == nil {
		t.Fatalf("Should have erred due to nil operation")
	}
}

func TestOperationVerifyEmpty(t *testing.T) {
	op := &Operation{
		Asset: avax.Asset{ID: ids.Empty},
	}
	if err := op.Verify(); err == nil {
		t.Fatalf("Should have erred due to empty operation")
	}
}

func TestOperationVerifyUTXOIDsNotSorted(t *testing.T) {
	op := &Operation{
		Asset: avax.Asset{ID: ids.Empty},
		UTXOIDs: []*avax.UTXOID{
			{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
		},
		Op: &testOperable{},
	}
	if err := op.Verify(); err == nil {
		t.Fatalf("Should have erred due to unsorted utxoIDs")
	}
}

func TestOperationVerify(t *testing.T) {
	assetID := ids.GenerateTestID()
	op := &Operation{
		Asset: avax.Asset{ID: assetID},
		UTXOIDs: []*avax.UTXOID{
			{
				TxID:        assetID,
				OutputIndex: 1,
			},
		},
		Op: &testOperable{},
	}
	if err := op.Verify(); err != nil {
		t.Fatal(err)
	}
}

func TestOperationSorting(t *testing.T) {
	c := linearcodec.NewDefault()
	if err := c.RegisterType(&testOperable{}); err != nil {
		t.Fatal(err)
	}

	m := codec.NewDefaultManager()
	if err := m.RegisterCodec(CodecVersion, c); err != nil {
		t.Fatal(err)
	}

	ops := []*Operation{
		{
			Asset: avax.Asset{ID: ids.Empty},
			UTXOIDs: []*avax.UTXOID{
				{
					TxID:        ids.Empty,
					OutputIndex: 1,
				},
			},
			Op: &testOperable{},
		},
		{
			Asset: avax.Asset{ID: ids.Empty},
			UTXOIDs: []*avax.UTXOID{
				{
					TxID:        ids.Empty,
					OutputIndex: 0,
				},
			},
			Op: &testOperable{},
		},
	}
	if IsSortedAndUniqueOperations(ops, m) {
		t.Fatalf("Shouldn't be sorted")
	}
	SortOperations(ops, m)
	if !IsSortedAndUniqueOperations(ops, m) {
		t.Fatalf("Should be sorted")
	}
	ops = append(ops, &Operation{
		Asset: avax.Asset{ID: ids.Empty},
		UTXOIDs: []*avax.UTXOID{
			{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
		},
		Op: &testOperable{},
	})
	if IsSortedAndUniqueOperations(ops, m) {
		t.Fatalf("Shouldn't be unique")
	}
}

func TestOperationTxNotState(t *testing.T) {
	intf := interface{}(&OperationTx{})
	if _, ok := intf.(verify.State); ok {
		t.Fatalf("shouldn't be marked as state")
	}
}

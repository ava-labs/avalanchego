// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/utils/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

type testOperable struct {
	ava.TestTransferable `serialize:"true"`

	Outputs []verify.Verifiable `serialize:"true"`
}

func (o *testOperable) Outs() []verify.Verifiable { return o.Outputs }

func TestOperationVerifyNil(t *testing.T) {
	c := codec.NewDefault()
	op := (*Operation)(nil)
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to nil operation")
	}
}

func TestOperationVerifyEmpty(t *testing.T) {
	c := codec.NewDefault()
	op := &Operation{
		Asset: ava.Asset{ID: ids.Empty},
	}
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to empty operation")
	}
}

func TestOperationVerifyUTXOIDsNotSorted(t *testing.T) {
	c := codec.NewDefault()
	op := &Operation{
		Asset: ava.Asset{ID: ids.Empty},
		UTXOIDs: []*ava.UTXOID{
			&ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			&ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
		},
		Op: &testOperable{},
	}
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to unsorted utxoIDs")
	}
}

func TestOperationVerify(t *testing.T) {
	c := codec.NewDefault()
	op := &Operation{
		Asset: ava.Asset{ID: ids.Empty},
		UTXOIDs: []*ava.UTXOID{
			&ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
		},
		Op: &testOperable{},
	}
	if err := op.Verify(c); err != nil {
		t.Fatal(err)
	}
}

func TestOperationSorting(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&testOperable{})

	ops := []*Operation{
		&Operation{
			Asset: ava.Asset{ID: ids.Empty},
			UTXOIDs: []*ava.UTXOID{
				&ava.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 1,
				},
			},
			Op: &testOperable{},
		},
		&Operation{
			Asset: ava.Asset{ID: ids.Empty},
			UTXOIDs: []*ava.UTXOID{
				&ava.UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 0,
				},
			},
			Op: &testOperable{},
		},
	}
	if isSortedAndUniqueOperations(ops, c) {
		t.Fatalf("Shouldn't be sorted")
	}
	sortOperations(ops, c)
	if !isSortedAndUniqueOperations(ops, c) {
		t.Fatalf("Should be sorted")
	}
	ops = append(ops, &Operation{
		Asset: ava.Asset{ID: ids.Empty},
		UTXOIDs: []*ava.UTXOID{
			&ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
		},
		Op: &testOperable{},
	})
	if isSortedAndUniqueOperations(ops, c) {
		t.Fatalf("Shouldn't be unique")
	}
}

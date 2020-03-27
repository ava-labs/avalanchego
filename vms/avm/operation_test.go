// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/codec"
	"github.com/ava-labs/gecko/vms/components/verify"
)

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
		Asset: Asset{
			ID: ids.Empty,
		},
	}
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to empty operation")
	}
}

func TestOperationVerifyInvalidInput(t *testing.T) {
	c := codec.NewDefault()
	op := &Operation{
		Asset: Asset{
			ID: ids.Empty,
		},
		Ins: []*OperableInput{
			&OperableInput{},
		},
	}
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to an invalid input")
	}
}

func TestOperationVerifyInputsNotSorted(t *testing.T) {
	c := codec.NewDefault()
	op := &Operation{
		Asset: Asset{
			ID: ids.Empty,
		},
		Ins: []*OperableInput{
			&OperableInput{
				UTXOID: UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 1,
				},
				In: &testVerifiable{},
			},
			&OperableInput{
				UTXOID: UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 0,
				},
				In: &testVerifiable{},
			},
		},
	}
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to unsorted inputs")
	}
}

func TestOperationVerifyOutputsNotSorted(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&TestTransferable{})

	op := &Operation{
		Asset: Asset{
			ID: ids.Empty,
		},
		Outs: []verify.Verifiable{
			&TestTransferable{Val: 1},
			&TestTransferable{Val: 0},
		},
	}
	if err := op.Verify(c); err == nil {
		t.Fatalf("Should have errored due to unsorted outputs")
	}
}

func TestOperationVerify(t *testing.T) {
	c := codec.NewDefault()
	op := &Operation{
		Asset: Asset{
			ID: ids.Empty,
		},
		Outs: []verify.Verifiable{
			&testVerifiable{},
		},
	}
	if err := op.Verify(c); err != nil {
		t.Fatal(err)
	}
}

func TestOperationSorting(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&testVerifiable{})

	ops := []*Operation{
		&Operation{
			Asset: Asset{
				ID: ids.Empty,
			},
			Ins: []*OperableInput{
				&OperableInput{
					UTXOID: UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 1,
					},
					In: &testVerifiable{},
				},
			},
		},
		&Operation{
			Asset: Asset{
				ID: ids.Empty,
			},
			Ins: []*OperableInput{
				&OperableInput{
					UTXOID: UTXOID{
						TxID:        ids.Empty,
						OutputIndex: 0,
					},
					In: &testVerifiable{},
				},
			},
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
		Asset: Asset{
			ID: ids.Empty,
		},
		Ins: []*OperableInput{
			&OperableInput{
				UTXOID: UTXOID{
					TxID:        ids.Empty,
					OutputIndex: 1,
				},
				In: &testVerifiable{},
			},
		},
	})
	if isSortedAndUniqueOperations(ops, c) {
		t.Fatalf("Shouldn't be unique")
	}
}

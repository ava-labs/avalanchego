// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
)

func TestOperableInputVerifyNil(t *testing.T) {
	oi := (*OperableInput)(nil)
	if err := oi.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil operable input")
	}
}

func TestOperableInputVerifyNilFx(t *testing.T) {
	oi := &OperableInput{}
	if err := oi.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil operable fx input")
	}
}

func TestOperableInputVerify(t *testing.T) {
	oi := &OperableInput{
		UTXOID: ava.UTXOID{TxID: ids.Empty},
		In:     &ava.TestVerifiable{},
	}
	if err := oi.Verify(); err != nil {
		t.Fatal(err)
	}
	if oi.Input() != oi.In {
		t.Fatalf("Should have returned the fx input")
	}
}

func TestOperableInputSorting(t *testing.T) {
	ins := []*OperableInput{
		&OperableInput{
			UTXOID: ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			In: &ava.TestVerifiable{},
		},
		&OperableInput{
			UTXOID: ava.UTXOID{
				TxID:        ids.NewID([32]byte{1}),
				OutputIndex: 1,
			},
			In: &ava.TestVerifiable{},
		},
		&OperableInput{
			UTXOID: ava.UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 0,
			},
			In: &ava.TestVerifiable{},
		},
		&OperableInput{
			UTXOID: ava.UTXOID{
				TxID:        ids.NewID([32]byte{1}),
				OutputIndex: 0,
			},
			In: &ava.TestVerifiable{},
		},
	}
	if isSortedAndUniqueOperableInputs(ins) {
		t.Fatalf("Shouldn't be sorted")
	}
	sortOperableInputs(ins)
	if !isSortedAndUniqueOperableInputs(ins) {
		t.Fatalf("Should be sorted")
	}
	if result := ins[0].OutputIndex; result != 0 {
		t.Fatalf("OutputIndex expected: %d ; result: %d", 0, result)
	}
	if result := ins[1].OutputIndex; result != 1 {
		t.Fatalf("OutputIndex expected: %d ; result: %d", 1, result)
	}
	if result := ins[2].OutputIndex; result != 0 {
		t.Fatalf("OutputIndex expected: %d ; result: %d", 0, result)
	}
	if result := ins[3].OutputIndex; result != 1 {
		t.Fatalf("OutputIndex expected: %d ; result: %d", 1, result)
	}
	if result := ins[0].TxID; !result.Equals(ids.Empty) {
		t.Fatalf("OutputIndex expected: %s ; result: %s", ids.Empty, result)
	}
	if result := ins[0].TxID; !result.Equals(ids.Empty) {
		t.Fatalf("OutputIndex expected: %s ; result: %s", ids.Empty, result)
	}
	ins = append(ins, &OperableInput{
		UTXOID: ava.UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		In: &ava.TestVerifiable{},
	})
	if isSortedAndUniqueOperableInputs(ins) {
		t.Fatalf("Shouldn't be unique")
	}
}

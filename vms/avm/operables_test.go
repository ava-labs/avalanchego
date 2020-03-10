// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/codec"
)

func TestOperableOutputVerifyNil(t *testing.T) {
	oo := (*OperableOutput)(nil)
	if err := oo.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil operable output")
	}
}

func TestOperableOutputVerifyNilFx(t *testing.T) {
	oo := &OperableOutput{}
	if err := oo.Verify(); err == nil {
		t.Fatalf("Should have errored due to nil operable fx output")
	}
}

func TestOperableOutputVerify(t *testing.T) {
	oo := &OperableOutput{
		Out: &testVerifiable{},
	}
	if err := oo.Verify(); err != nil {
		t.Fatal(err)
	}
	if oo.Output() != oo.Out {
		t.Fatalf("Should have returned the fx output")
	}
}

func TestOperableOutputSorting(t *testing.T) {
	c := codec.NewDefault()
	c.RegisterType(&TestTransferable{})
	c.RegisterType(&testVerifiable{})

	outs := []*OperableOutput{
		&OperableOutput{
			Out: &TestTransferable{Val: 1},
		},
		&OperableOutput{
			Out: &TestTransferable{Val: 0},
		},
		&OperableOutput{
			Out: &TestTransferable{Val: 0},
		},
		&OperableOutput{
			Out: &testVerifiable{},
		},
	}

	if isSortedOperableOutputs(outs, c) {
		t.Fatalf("Shouldn't be sorted")
	}
	sortOperableOutputs(outs, c)
	if !isSortedOperableOutputs(outs, c) {
		t.Fatalf("Should be sorted")
	}
	if result := outs[0].Out.(*TestTransferable).Val; result != 0 {
		t.Fatalf("Val expected: %d ; result: %d", 0, result)
	}
	if result := outs[1].Out.(*TestTransferable).Val; result != 0 {
		t.Fatalf("Val expected: %d ; result: %d", 0, result)
	}
	if result := outs[2].Out.(*TestTransferable).Val; result != 1 {
		t.Fatalf("Val expected: %d ; result: %d", 0, result)
	}
	if _, ok := outs[3].Out.(*testVerifiable); !ok {
		t.Fatalf("testVerifiable expected")
	}
}

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
		UTXOID: UTXOID{
			TxID: ids.Empty,
		},
		In: &testVerifiable{},
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
			UTXOID: UTXOID{
				TxID:        ids.Empty,
				OutputIndex: 1,
			},
			In: &testVerifiable{},
		},
		&OperableInput{
			UTXOID: UTXOID{
				TxID:        ids.NewID([32]byte{1}),
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
		&OperableInput{
			UTXOID: UTXOID{
				TxID:        ids.NewID([32]byte{1}),
				OutputIndex: 0,
			},
			In: &testVerifiable{},
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
		UTXOID: UTXOID{
			TxID:        ids.Empty,
			OutputIndex: 1,
		},
		In: &testVerifiable{},
	})
	if isSortedAndUniqueOperableInputs(ins) {
		t.Fatalf("Shouldn't be unique")
	}
}

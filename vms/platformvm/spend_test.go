package platformvm

import (
	"errors"
	"testing"

	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/vms/components/ava"
)

// MockTransferable implements Transferable
// For use in testing
type MockTransferable struct {
	// If true, Verify() returns an error
	FailVerify bool
	// Amount() returns AmountVal
	AmountVal uint64 `serialize:"true"`
}

func (mt MockTransferable) Verify() error {
	if mt.FailVerify {
		return errors.New("")
	}
	return nil
}

func (mt MockTransferable) Amount() uint64 {
	return mt.AmountVal
}

func TestSyntacticVerifySpend(t *testing.T) {
	avaAssetID := ids.NewID([32]byte{1, 2, 3, 4, 5, 4, 3, 2, 1})
	otherAssetID := ids.NewID([32]byte{1, 2, 3})
	txID1 := ids.NewID([32]byte{1})
	utxoID1 := ava.UTXOID{
		TxID:        txID1,
		OutputIndex: 0,
		Symbol:      false,
	}
	utxoID2 := ava.UTXOID{
		TxID:        txID1,
		OutputIndex: 1,
		Symbol:      false,
	}

	type spendTest struct {
		description string
		ins         []*ava.TransferableInput
		outs        []*ava.TransferableOutput
		txFee       uint64
		shouldErr   bool
	}
	tests := []spendTest{
		{
			"no inputs, no outputs, no tx fee",
			[]*ava.TransferableInput{},
			[]*ava.TransferableOutput{},
			0,
			false,
		},
		{
			"no inputs, no outputs, tx fee",
			[]*ava.TransferableInput{},
			[]*ava.TransferableOutput{},
			1,
			true,
		},
		{
			"one input, no outputs, sufficient funds, tx fee",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			1,
			false,
		},
		{
			"one input, no outputs, insufficient funds, tx fee",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			2,
			true,
		},
		{
			"one input, one outputs, sufficient funds, tx fee",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 2},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			1,
			false,
		},
		{
			"repeated input, insufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			2,
			true,
		},
		{
			"multiple inputs and outputs, insufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
				&ava.TransferableInput{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			1,
			true,
		},
		{
			"multiple inputs and outputs, sufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 2},
				},
				&ava.TransferableInput{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			1,
			false,
		},
		{
			"inputs unsorted",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 2},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			1,
			true,
		},
		{
			"wrong asset ID for input",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: otherAssetID},
					In:     MockTransferable{AmountVal: 10},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			1,
			true,
		},
		{
			"wrong asset ID for output",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaAssetID},
					In:     MockTransferable{AmountVal: 10},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: otherAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			1,
			true,
		},
	}

	for _, tt := range tests {
		if err := syntacticVerifySpend(tt.ins, tt.outs, tt.txFee, avaAssetID); err == nil && tt.shouldErr {
			t.Fatal("expected error but got none")
		} else if err != nil && !tt.shouldErr {
			t.Fatalf("expected no error but got %s", err)
		}
	}
}

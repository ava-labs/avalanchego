package platformvm

import (
	"errors"
	"math"
	"testing"

	"github.com/ava-labs/gecko/vms/components/verify"

	"github.com/ava-labs/gecko/ids"

	"github.com/ava-labs/gecko/vms/components/ava"
)

type MockCredential struct {
	// If true, Verify() returns an error
	FailVerify bool
}

func (mc MockCredential) Verify() error {
	if mc.FailVerify {
		return errors.New("erroring on purpose")
	}
	return nil
}

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
		return errors.New("erroring on purpose")
	}
	return nil
}

func (mt MockTransferable) VerifyState() error { return mt.Verify() }

func (mt MockTransferable) Amount() uint64 {
	return mt.AmountVal
}

type MockSpendTx struct {
	IDF    func() ids.ID
	InsF   func() []*ava.TransferableInput
	OutsF  func() []*ava.TransferableOutput
	CredsF func() []verify.Verifiable
}

func (tx MockSpendTx) ID() ids.ID {
	if tx.IDF != nil {
		return tx.IDF()
	}
	return ids.ID{}
}

func (tx MockSpendTx) Ins() []*ava.TransferableInput {
	if tx.InsF != nil {
		return tx.InsF()
	}
	return nil
}

func (tx MockSpendTx) Outs() []*ava.TransferableOutput {
	if tx.OutsF != nil {
		return tx.OutsF()
	}
	return nil
}

func (tx MockSpendTx) Creds() []verify.Verifiable {
	if tx.CredsF != nil {
		return tx.CredsF()
	}
	return nil
}

func TestSyntacticVerifySpend(t *testing.T) {
	avaxAssetID := ids.NewID([32]byte{1, 2, 3, 4, 5, 4, 3, 2, 1})
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
		description     string
		ins             []*ava.TransferableInput
		outs            []*ava.TransferableOutput
		creds           []MockCredential
		unlockedBurnAmt uint64
		lockedBurnAmt   uint64
		shouldErr       bool
	}
	tests := []spendTest{
		{
			"no inputs, no outputs, no burn",
			[]*ava.TransferableInput{},
			[]*ava.TransferableOutput{},
			[]MockCredential{},
			0,
			0,
			false,
		},
		{
			"no inputs, no outputs, non-zero burn",
			[]*ava.TransferableInput{},
			[]*ava.TransferableOutput{},
			[]MockCredential{},
			1,
			0,
			true,
		},
		{
			"one input, no outputs, sufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			make([]MockCredential, 1),
			1,
			0,
			false,
		},
		{
			"one input, no outputs, insufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			make([]MockCredential, 1),
			2,
			0,
			true,
		},
		{
			"one input, one outputs, sufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 2},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 1),
			1,
			0,
			false,
		},
		{
			"repeated input, insufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 2),
			2,
			0,
			true,
		},
		{
			"multiple inputs and outputs, insufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
				&ava.TransferableInput{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 2),
			1,
			0,
			true,
		},
		{
			"multiple inputs and outputs, sufficient funds",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 2},
				},
				&ava.TransferableInput{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 2),
			1,
			0,
			false,
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
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 1),
			1,
			0,
			true,
		},
		{
			"wrong asset ID for output",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 10},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: otherAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 1),
			1,
			0,
			true,
		},
		{
			"input amount overflow",
			[]*ava.TransferableInput{
				&ava.TransferableInput{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: math.MaxUint64 - 2},
				},
				&ava.TransferableInput{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: math.MaxUint64 - 2},
				},
			},
			[]*ava.TransferableOutput{
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				&ava.TransferableOutput{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			make([]MockCredential, 2),
			math.MaxUint64 - 1,
			0,
			true,
		},
	}

	for _, tt := range tests {
		creds := make([]verify.Verifiable, len(tt.creds)) // Convert tt.creds ([]MockCredential) to []verify.Verifiable
		for i := 0; i < len(tt.ins); i++ {
			creds[i] = tt.creds[i]
		}
		tx := MockSpendTx{
			InsF:   func() []*ava.TransferableInput { return tt.ins },
			OutsF:  func() []*ava.TransferableOutput { return tt.outs },
			CredsF: func() []verify.Verifiable { return creds },
		}
		if err := syntacticVerifySpend(tx.Ins(), tx.Outs(), tt.unlockedBurnAmt, tt.lockedBurnAmt, avaxAssetID); err == nil && tt.shouldErr {
			t.Fatalf("expected test '%s' error but got none", tt.description)
		} else if err != nil && !tt.shouldErr {
			t.Fatalf("unexpected error on test '%s': %s", tt.description, err)
		}
	}
}

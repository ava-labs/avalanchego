package platformvm

import (
	"errors"
	"math"
	"testing"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/vms/components/ava"
	"github.com/ava-labs/gecko/vms/components/verify"
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
		unlockedOuts    []*ava.TransferableOutput
		lockedOuts      []*ava.TransferableOutput
		lockedAmt       uint64
		unlockedBurnAmt uint64
		shouldErr       bool
	}
	tests := []spendTest{
		{
			"no inputs, no unlocked outputs, no locked outputs, no locked, no burn",
			[]*ava.TransferableInput{},
			[]*ava.TransferableOutput{},
			[]*ava.TransferableOutput{},
			0,
			0,
			false,
		},
		{
			"no inputs, no unlocked outputs, no locked outputs, no locked, non-zero burn",
			[]*ava.TransferableInput{},
			[]*ava.TransferableOutput{},
			[]*ava.TransferableOutput{},
			0,
			1,
			true,
		},
		{
			"one input, no unlocked outputs, no locked outputs, no locked, sufficient burn",
			[]*ava.TransferableInput{{
				UTXOID: utxoID1,
				Asset:  ava.Asset{ID: avaxAssetID},
				In:     MockTransferable{AmountVal: 1},
			}},
			[]*ava.TransferableOutput{},
			[]*ava.TransferableOutput{},
			0,
			1,
			false,
		},
		{
			"one input, no unlocked outputs, no locked outputs, no locked, insufficient burn",
			[]*ava.TransferableInput{{
				UTXOID: utxoID1,
				Asset:  ava.Asset{ID: avaxAssetID},
				In:     MockTransferable{AmountVal: 1},
			}},
			[]*ava.TransferableOutput{},
			[]*ava.TransferableOutput{},
			0,
			2,
			true,
		},
		{
			"one input, one unlocked output, no locked outputs, no locked, sufficient burn",
			[]*ava.TransferableInput{{
				UTXOID: utxoID1,
				Asset:  ava.Asset{ID: avaxAssetID},
				In:     MockTransferable{AmountVal: 2},
			}},
			[]*ava.TransferableOutput{{
				Asset: ava.Asset{ID: avaxAssetID},
				Out:   MockTransferable{AmountVal: 1},
			}},
			[]*ava.TransferableOutput{},
			0,
			1,
			false,
		},
		{
			"multiple inputs, multiple unlocked outputs, no locked outputs, no locked, insufficient burn",
			[]*ava.TransferableInput{
				{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
				{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			0,
			1,
			true,
		},
		{
			"multiple inputs, multiple unlocked outputs, no locked outputs, no locked, sufficient burn",
			[]*ava.TransferableInput{
				{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 2},
				},
				{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{
				{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
				{
					Asset: ava.Asset{ID: avaxAssetID},
					Out:   MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			0,
			1,
			false,
		},
		{
			"wrong asset ID for input",
			[]*ava.TransferableInput{{
				UTXOID: utxoID1,
				Asset:  ava.Asset{ID: otherAssetID},
				In:     MockTransferable{AmountVal: 1},
			}},
			[]*ava.TransferableOutput{},
			[]*ava.TransferableOutput{},
			0,
			1,
			true,
		},
		{
			"wrong asset ID for output",
			[]*ava.TransferableInput{{
				UTXOID: utxoID1,
				Asset:  ava.Asset{ID: avaxAssetID},
				In:     MockTransferable{AmountVal: 1},
			}},
			[]*ava.TransferableOutput{{
				Asset: ava.Asset{ID: otherAssetID},
				Out:   MockTransferable{AmountVal: 1},
			}},
			[]*ava.TransferableOutput{},
			0,
			1,
			true,
		},
		{
			"input amount overflow",
			[]*ava.TransferableInput{
				{
					UTXOID: utxoID1,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: math.MaxUint64},
				},
				{
					UTXOID: utxoID2,
					Asset:  ava.Asset{ID: avaxAssetID},
					In:     MockTransferable{AmountVal: 1},
				},
			},
			[]*ava.TransferableOutput{},
			[]*ava.TransferableOutput{},
			0,
			0,
			true,
		},
	}

	for _, tt := range tests {
		if err := syntacticVerifySpend(tt.ins, tt.unlockedOuts, tt.lockedOuts, tt.lockedAmt, tt.unlockedBurnAmt, avaxAssetID); err == nil && tt.shouldErr {
			t.Fatalf("expected test '%s' error but got none", tt.description)
		} else if err != nil && !tt.shouldErr {
			t.Fatalf("unexpected error on test '%s': %s", tt.description, err)
		}
	}
}

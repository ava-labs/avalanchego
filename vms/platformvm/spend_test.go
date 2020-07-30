package platformvm

// type MockCredential struct {
// 	// If true, Verify() returns an error
// 	FailVerify bool
// }

// func (mc MockCredential) Verify() error {
// 	if mc.FailVerify {
// 		return errors.New("erroring on purpose")
// 	}
// 	return nil
// }

// // MockTransferable implements Transferable
// // For use in testing
// type MockTransferable struct {
// 	// If true, Verify() returns an error
// 	FailVerify bool
// 	// Amount() returns AmountVal
// 	AmountVal uint64 `serialize:"true"`
// }

// func (mt MockTransferable) Verify() error {
// 	if mt.FailVerify {
// 		return errors.New("erroring on purpose")
// 	}
// 	return nil
// }

// func (mt MockTransferable) VerifyState() error { return mt.Verify() }

// func (mt MockTransferable) Amount() uint64 {
// 	return mt.AmountVal
// }

// type MockSpendTx struct {
// 	IDF    func() ids.ID
// 	InsF   func() []*ava.TransferableInput
// 	OutsF  func() []*ava.TransferableOutput
// 	CredsF func() []verify.Verifiable
// }

// func (tx MockSpendTx) ID() ids.ID {
// 	if tx.IDF != nil {
// 		return tx.IDF()
// 	}
// 	return ids.ID{}
// }

// func (tx MockSpendTx) Ins() []*ava.TransferableInput {
// 	if tx.InsF != nil {
// 		return tx.InsF()
// 	}
// 	return nil
// }

// func (tx MockSpendTx) Outs() []*ava.TransferableOutput {
// 	if tx.OutsF != nil {
// 		return tx.OutsF()
// 	}
// 	return nil
// }

// func (tx MockSpendTx) Creds() []verify.Verifiable {
// 	if tx.CredsF != nil {
// 		return tx.CredsF()
// 	}
// 	return nil
// }

// func TestSyntacticVerifySpend(t *testing.T) {
// 	avaxAssetID := ids.NewID([32]byte{1, 2, 3, 4, 5, 4, 3, 2, 1})
// 	otherAssetID := ids.NewID([32]byte{1, 2, 3})
// 	txID1 := ids.NewID([32]byte{1})
// 	utxoID1 := ava.UTXOID{
// 		TxID:        txID1,
// 		OutputIndex: 0,
// 		Symbol:      false,
// 	}
// 	utxoID2 := ava.UTXOID{
// 		TxID:        txID1,
// 		OutputIndex: 1,
// 		Symbol:      false,
// 	}

// 	type spendTest struct {
// 		description string
// 		ins         []*ava.TransferableInput
// 		outs        []*ava.TransferableOutput
// 		creds       []MockCredential
// 		spendAmt    uint64
// 		shouldErr   bool
// 	}
// 	tests := []spendTest{
// 		{
// 			"no inputs, no outputs, no tx fee",
// 			[]*ava.TransferableInput{},
// 			[]*ava.TransferableOutput{},
// 			[]MockCredential{},
// 			0,
// 			false,
// 		},
// 		{
// 			"no inputs, no outputs, tx fee",
// 			[]*ava.TransferableInput{},
// 			[]*ava.TransferableOutput{},
// 			[]MockCredential{},
// 			1,
// 			true,
// 		},
// 		{
// 			"one input, no outputs, sufficient funds, tx fee",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]*ava.TransferableOutput{},
// 			make([]MockCredential, 1),
// 			1,
// 			false,
// 		},
// 		{
// 			"one input, no outputs, insufficient funds, tx fee",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]*ava.TransferableOutput{},
// 			make([]MockCredential, 1),
// 			2,
// 			true,
// 		},
// 		{
// 			"one input, one outputs, sufficient funds, tx fee",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 2},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 1),
// 			1,
// 			false,
// 		},
// 		{
// 			"repeated input, insufficient funds",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 2),
// 			2,
// 			true,
// 		},
// 		{
// 			"multiple inputs and outputs, insufficient funds",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableInput{
// 					UTXOID: utxoID2,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 2),
// 			1,
// 			true,
// 		},
// 		{
// 			"multiple inputs and outputs, sufficient funds",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 2},
// 				},
// 				&ava.TransferableInput{
// 					UTXOID: utxoID2,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 2),
// 			1,
// 			false,
// 		},
// 		{
// 			"inputs unsorted",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID2,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 2},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 2),
// 			1,
// 			true,
// 		},
// 		{
// 			"wrong asset ID for input",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: otherAssetID},
// 					In:     MockTransferable{AmountVal: 10},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 1),
// 			1,
// 			true,
// 		},
// 		{
// 			"wrong asset ID for output",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 10},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: otherAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 1),
// 			1,
// 			true,
// 		},
// 		{
// 			"a credential fails verify",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 2},
// 				},
// 				&ava.TransferableInput{
// 					UTXOID: utxoID2,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			[]MockCredential{
// 				MockCredential{},
// 				MockCredential{FailVerify: true},
// 			},
// 			1,
// 			true,
// 		},
// 		{
// 			"input amount overflow",
// 			[]*ava.TransferableInput{
// 				&ava.TransferableInput{
// 					UTXOID: utxoID1,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: math.MaxUint64 - 2},
// 				},
// 				&ava.TransferableInput{
// 					UTXOID: utxoID2,
// 					Asset:  ava.Asset{ID: avaxAssetID},
// 					In:     MockTransferable{AmountVal: math.MaxUint64 - 2},
// 				},
// 			},
// 			[]*ava.TransferableOutput{
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 				&ava.TransferableOutput{
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out:   MockTransferable{AmountVal: 1},
// 				},
// 			},
// 			make([]MockCredential, 2),
// 			math.MaxUint64 - 1,
// 			true,
// 		},
// 	}

// 	for _, tt := range tests {
// 		creds := make([]verify.Verifiable, len(tt.creds)) // Convert tt.creds ([]MockCredential) to []verify.Verifiable
// 		for i := 0; i < len(tt.ins); i++ {
// 			creds[i] = tt.creds[i]
// 		}
// 		tx := MockSpendTx{
// 			InsF:   func() []*ava.TransferableInput { return tt.ins },
// 			OutsF:  func() []*ava.TransferableOutput { return tt.outs },
// 			CredsF: func() []verify.Verifiable { return creds },
// 		}
// 		if err := syntacticVerifySpend(tx, tt.spendAmt, avaxAssetID); err == nil && tt.shouldErr {
// 			t.Fatal("expected error but got none")
// 		} else if err != nil && !tt.shouldErr {
// 			t.Fatalf("expected no error but got one")
// 		}
// 	}
// }

package platformvm

// // implements snow.SharedMemory
// type MockSharedMemory struct {
// 	GetDatabaseF func(ids.ID) database.Database
// }

// func (msm MockSharedMemory) GetDatabase(ID ids.ID) database.Database {
// 	if msm.GetDatabaseF != nil {
// 		return msm.GetDatabaseF(ID)
// 	}
// 	return &nodb.Database{}
// }

// func (msm MockSharedMemory) ReleaseDatabase(ID ids.ID) {}

// func TestNewImportTx(t *testing.T) {
// 	type test struct {
// 		description  string
// 		sharedMemory MockSharedMemory
// 		feeKeys      []*crypto.PrivateKeySECP256K1R
// 		recipientKey *crypto.PrivateKeySECP256K1R
// 		shouldErr    bool
// 	}

// 	factory := crypto.FactorySECP256K1R{}
// 	recipientKeyIntf, err := factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	recipientKey := recipientKeyIntf.(*crypto.PrivateKeySECP256K1R)

// 	// Returns a shared memory where GetDatabase returns a database
// 	// where [recipientKey] has a balance of [amt]
// 	fundedSharedMemory := func(amt uint64) MockSharedMemory {
// 		return MockSharedMemory{
// 			GetDatabaseF: func(ids.ID) database.Database {
// 				db := memdb.New()
// 				state := ava.NewPrefixedState(db, Codec)
// 				if err := state.FundAVMUTXO(&ava.UTXO{
// 					UTXOID: ava.UTXOID{
// 						TxID:        generateRandomID(),
// 						OutputIndex: rand.Uint32(),
// 					},
// 					Asset: ava.Asset{ID: avaxAssetID},
// 					Out: &secp256k1fx.TransferOutput{
// 						Amt: amt,
// 						OutputOwners: secp256k1fx.OutputOwners{
// 							Locktime:  0,
// 							Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
// 							Threshold: 1,
// 						},
// 					},
// 				}); err != nil {
// 					panic(err)
// 				}
// 				return db
// 			},
// 		}
// 	}

// 	vm := defaultVM()
// 	avmID := generateRandomID()
// 	vm.avm = avmID
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	tests := []test{
// 		{
// 			description:  "recipient key can't pay fee; no fee keys",
// 			sharedMemory: fundedSharedMemory(vm.txFee - 1),
// 			feeKeys:      nil,
// 			recipientKey: recipientKey,
// 			shouldErr:    true,
// 		},
// 		{

// 			description:  "recipient key pays fee",
// 			sharedMemory: fundedSharedMemory(vm.txFee),
// 			feeKeys:      nil,
// 			recipientKey: recipientKey,
// 			shouldErr:    false,
// 		},
// 		{
// 			description:  "no recipient keys provided",
// 			sharedMemory: fundedSharedMemory(0),
// 			feeKeys:      []*crypto.PrivateKeySECP256K1R{keys[0]},
// 			recipientKey: nil,
// 			shouldErr:    true,
// 		},
// 		{
// 			description:  "fee keys and recipient key both pay part of tx fee",
// 			sharedMemory: fundedSharedMemory(vm.txFee - 1),
// 			feeKeys:      []*crypto.PrivateKeySECP256K1R{keys[0]},
// 			recipientKey: recipientKey,
// 			shouldErr:    false,
// 		},
// 	}

// 	vdb := versiondb.New(vm.DB)
// 	for _, tt := range tests {
// 		vm.Ctx.SharedMemory = tt.sharedMemory
// 		tx, err := vm.newImportTx(tt.feeKeys, tt.recipientKey)
// 		if err != nil {
// 			if !tt.shouldErr {
// 				t.Fatalf("test '%s' errored but it shouldn't have", tt.description)
// 			}
// 			continue
// 		} else if tt.shouldErr {
// 			t.Fatalf("test '%s' didn't error but it should have", tt.description)
// 		} else if len(tx.Ins()) == 0 {
// 			t.Fatal("tx has no inputs")
// 		} else if len(tx.ImportedInputs) == 0 {
// 			t.Fatal("tx has no imported inputs")
// 		} else if len(tx.Outs()) == 0 {
// 			t.Fatal("tx has no outputs")
// 		} else if len(tx.Creds()) != len(tx.Ins()) {
// 			t.Fatal("should have same number of credentials as inputs")
// 		}
// 		totalIn := uint64(0)
// 		for _, in := range tx.Ins() {
// 			totalIn += in.Input().Amount()
// 		}
// 		totalOut := uint64(0)
// 		for _, out := range tx.Outs() {
// 			totalOut += out.Out.Amount()
// 		}
// 		if totalIn-totalOut != vm.txFee {
// 			t.Fatal("inputs should equal outputs + txFee")
// 		}
// 		vdb.Abort()
// 	}
// }

// // Ensure that calling Ins() doesn't modify the tx
// func TestImportTxInsDoesntModify(t *testing.T) {
// 	vm := defaultVM()
// 	avmID := generateRandomID()
// 	vm.avm = avmID
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	factory := crypto.FactorySECP256K1R{}
// 	recipientKeyIntf, err := factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	recipientKey := recipientKeyIntf.(*crypto.PrivateKeySECP256K1R)

// 	// Returns a shared memory where GetDatabase returns a database
// 	// where [recipientKey] has a balance of 50,000
// 	vm.Ctx.SharedMemory = MockSharedMemory{
// 		GetDatabaseF: func(ids.ID) database.Database {
// 			db := memdb.New()
// 			state := ava.NewPrefixedState(db, Codec)
// 			if err := state.FundAVMUTXO(&ava.UTXO{
// 				UTXOID: ava.UTXOID{
// 					TxID:        generateRandomID(),
// 					OutputIndex: rand.Uint32(),
// 				},
// 				Asset: ava.Asset{ID: avaxAssetID},
// 				Out: &secp256k1fx.TransferOutput{
// 					Amt: 50000,
// 					OutputOwners: secp256k1fx.OutputOwners{
// 						Locktime:  0,
// 						Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
// 						Threshold: 1,
// 					},
// 				},
// 			}); err != nil {
// 				panic(err)
// 			}
// 			return db
// 		},
// 	}

// 	tx, err := vm.newImportTx(keys, recipientKey)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	ins := tx.Ins()
// 	if len(ins) == 0 {
// 		t.Fatal("len(ins) should be at least 1")
// 	}
// 	ins[0] = nil // If ins points to tx.BaseTx.Inputs, then setting first element to nil will do the same in tx.BaseTx.Inputs
// 	if len(tx.BaseTx.Inputs) > 0 && tx.BaseTx.Inputs[0] == nil {
// 		t.Fatal("Ins() shouldn't return the same underlying slice as tx.BaseTx.Inputs")
// 	}
// }

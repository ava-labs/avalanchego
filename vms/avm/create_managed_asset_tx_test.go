package avm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/verify"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/stretchr/testify/require"
)

// Test managed asset functionality
func TestManagedAsset(t *testing.T) {
	type create struct {
		originalFrozen  bool
		originalManager secp256k1fx.OutputOwners
	}

	type transfer struct {
		amt  uint64
		from ids.ShortID
		to   ids.ShortID
		keys []*crypto.PrivateKeySECP256K1R
	}

	type updateStatus struct {
		manager      secp256k1fx.OutputOwners
		frozen, mint bool
		mintAmt      uint64
		mintTo       ids.ShortID
		keys         []*crypto.PrivateKeySECP256K1R
	}

	type step struct {
		op               interface{} // create, transfer, mint or updateStatus
		shouldFailVerify bool
	}

	type test struct {
		description string
		create      create
		steps       []step
	}

	tests := []test{
		{
			"create, wrong key mint",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[1]}, // not manager key
					},
					true,
				},
			},
		},
		{
			"create, mint, transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false,
				},
			},
		},
		{
			"create, mint, wrong transfer key",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // use wrong key to try to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					true,
				},
			},
		},
		{
			"create, mint, asset manager transfers",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // Note that the asset manager, not keys[1], is spending
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
			},
		},
		{
			"create, change manager, old manager mint fails, mint, transfer, old manager transfer fails, transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // Change owner to keys[1]
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
						frozen: false,
						mint:   false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // old manager tries to mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[1]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]}, // old key
					},
					true,
				},
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[1]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[2].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false,
				},
				{ // old manager tries to transfer; fails
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
			},
		},
		{
			"create, mint, freeze, transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // freeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					true, // Should error because asset is frozen
				},
			},
		},
		{
			"create, mint, freeze and change manager, manager transfer",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // freeze and change owner to keys[2]
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[2].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // old manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true, // asset is frozen
				},
				{ // manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					true, // asset is frozen
				},
				{ // unfreeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[2].PublicKey().Address()},
						},
						frozen: false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false,
				},
				{ // old manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true, // keys[0] is not the owner or the manager
				},
				{ // manager tries to transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false, // keys[2] is the owner
				},
			},
		},
		{
			"create, mint, multiple freeze/unfreeze",
			create{
				originalFrozen: false,
				originalManager: secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{addrs[0]},
				},
			},
			[]step{
				{ // mint
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{addrs[0]},
						},
						frozen:  false,
						mint:    true,
						mintTo:  keys[1].PublicKey().Address(),
						mintAmt: 1000,
						keys:    []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // freeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{ // transfer
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					true, // Should error because asset is frozen
				},
				{ // transfer should fail even if it's manager
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					true, // Should error because asset is frozen
				},
				{ // unfreeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
						},
						frozen: false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[1].PublicKey().Address(),
						to:   keys[2].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false, // Should not error because asset is unfrozen
				},
				{ // freeze and change manager
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
						frozen: true,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[0]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					true, // Should error because asset is frozen
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					true, // Should error because asset is frozen
				},
				{ // unfreeze
					updateStatus{
						manager: secp256k1fx.OutputOwners{
							Threshold: 1,
							Addrs:     []ids.ShortID{keys[1].PublicKey().Address()},
						},
						frozen: false,
						keys:   []*crypto.PrivateKeySECP256K1R{keys[1]},
					},
					false,
				},
				{
					transfer{
						amt:  1,
						from: keys[2].PublicKey().Address(),
						to:   keys[1].PublicKey().Address(),
						keys: []*crypto.PrivateKeySECP256K1R{keys[2]},
					},
					false, // Should not error because asset is unfrozen
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			// Initialize the VM
			genesisBytes, _, vm, _ := GenesisVM(t)
			ctx := vm.ctx
			defer func() {
				if err := vm.Shutdown(); err != nil {
					t.Fatal(err)
				}
				ctx.Lock.Unlock()
			}()

			genesisTx := GetAVAXTxFromGenesisTest(genesisBytes, t)
			avaxID := genesisTx.ID()

			// Create a create a managed asset
			assetStatusOutput := &secp256k1fx.ManagedAssetStatusOutput{
				Frozen:  test.create.originalFrozen,
				Manager: test.create.originalManager,
			}
			unsignedCreateManagedAssetTx := &CreateManagedAssetTx{
				CreateAssetTx: CreateAssetTx{
					BaseTx: BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    networkID,
						BlockchainID: chainID,
						Outs: []*avax.TransferableOutput{{
							Asset: avax.Asset{ID: avaxID},
							Out: &secp256k1fx.TransferOutput{
								Amt: startBalance - testTxFee,
								OutputOwners: secp256k1fx.OutputOwners{
									Threshold: 1,
									Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
								},
							},
						}},
						Ins: []*avax.TransferableInput{{
							// Creation tx paid for by keys[0] genesis balance
							UTXOID: avax.UTXOID{
								TxID:        avaxID,
								OutputIndex: 2,
							},
							Asset: avax.Asset{ID: avaxID},
							In: &secp256k1fx.TransferInput{
								Amt: startBalance,
								Input: secp256k1fx.Input{
									SigIndices: []uint32{0},
								},
							},
						}},
					}},
					Name:         "NormalName",
					Symbol:       "TICK",
					Denomination: byte(2),
					States: []*InitialState{
						{
							FxID: 0,
							Outs: []verify.State{
								assetStatusOutput,
							},
						},
					},
				},
			}
			createManagedAssetTx := Tx{
				UnsignedTx: unsignedCreateManagedAssetTx,
			}

			// Sign/initialize the transaction
			feeSigner := []*crypto.PrivateKeySECP256K1R{keys[0]}
			err := createManagedAssetTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner})
			require.NoError(t, err)

			// Verify and accept the transaction
			uniqueCreateManagedAssetTx, err := vm.parseTx(createManagedAssetTx.Bytes())
			require.NoError(t, err)
			err = uniqueCreateManagedAssetTx.Verify()
			require.NoError(t, err)
			err = uniqueCreateManagedAssetTx.Accept()
			require.NoError(t, err)
			// The asset has been created
			managedAssetID := uniqueCreateManagedAssetTx.ID()

			//updateStatusOutput := assetStatusOutput
			updateStatusUtxoID := avax.UTXOID{TxID: managedAssetID, OutputIndex: 1}

			avaxFundedUtxoID := &avax.UTXOID{TxID: managedAssetID, OutputIndex: 0}
			avaxFundedAmt := startBalance - testTxFee

			// Address --> UTXO containing the managed asset owned by that address
			managedAssetFundedUtxoIDs := map[[20]byte]avax.UTXOID{}

			// Address --> Balance of managed asset owned by that address
			managedAssetFundedAmt := map[[20]byte]uint64{}

			for _, step := range test.steps {
				switch op := step.op.(type) {
				case transfer:
					// Transfer some units of the managed asset
					transferTx := &Tx{UnsignedTx: &BaseTx{BaseTx: avax.BaseTx{
						NetworkID:    networkID,
						BlockchainID: chainID,
						Ins: []*avax.TransferableInput{
							{ // This input is for the tx fee
								UTXOID: *avaxFundedUtxoID,
								Asset:  avax.Asset{ID: avaxID},
								In: &secp256k1fx.TransferInput{
									Amt: avaxFundedAmt,
									Input: secp256k1fx.Input{
										SigIndices: []uint32{0},
									},
								},
							},
							{ // This input is to transfer the asset
								UTXOID: managedAssetFundedUtxoIDs[op.from.Key()],
								Asset:  avax.Asset{ID: managedAssetID},
								In: &secp256k1fx.TransferInput{
									Amt: managedAssetFundedAmt[op.from.Key()],
									Input: secp256k1fx.Input{
										SigIndices: []uint32{0},
									},
								},
							},
						},
						Outs: []*avax.TransferableOutput{
							{ // Send AVAX change back to keys[0]
								Asset: avax.Asset{ID: genesisTx.ID()},
								Out: &secp256k1fx.TransferOutput{
									Amt: avaxFundedAmt - testTxFee,
									OutputOwners: secp256k1fx.OutputOwners{
										Threshold: 1,
										Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
									},
								},
							},
							{ // Transfer the managed asset
								Asset: avax.Asset{ID: managedAssetID},
								Out: &secp256k1fx.TransferOutput{
									Amt: op.amt,
									OutputOwners: secp256k1fx.OutputOwners{
										Locktime:  0,
										Threshold: 1,
										Addrs:     []ids.ShortID{op.to},
									},
								},
							},
						},
					}}}

					avax.SortTransferableInputs(transferTx.UnsignedTx.(*BaseTx).Ins)
					avax.SortTransferableOutputs(transferTx.UnsignedTx.(*BaseTx).Outs, vm.codec)

					// One signature to spend the tx fee, one signature to transfer the managed asset
					if transferTx.UnsignedTx.(*BaseTx).Ins[0].AssetID() == avaxID {
						err = transferTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner, op.keys})
					} else {
						err = transferTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{op.keys, feeSigner})
					}
					require.NoError(t, err)

					// Verify and accept the transaction
					uniqueTransferTx, err := vm.parseTx(transferTx.Bytes())
					require.NoError(t, err)
					err = uniqueTransferTx.Verify()
					if !step.shouldFailVerify {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						continue
					}
					err = uniqueTransferTx.Accept()
					require.NoError(t, err)

					avaxOutputIndex := uint32(0)
					if transferTx.UnsignedTx.(*BaseTx).Outs[0].AssetID() == managedAssetID {
						avaxOutputIndex = uint32(1)
					}

					// Update test data
					avaxFundedAmt -= testTxFee
					avaxFundedUtxoID = &avax.UTXOID{TxID: uniqueTransferTx.txID, OutputIndex: avaxOutputIndex}

					managedAssetFundedAmt[op.to.Key()] = op.amt
					managedAssetFundedAmt[op.from.Key()] -= op.amt
					// TODO update from
					managedAssetFundedUtxoIDs[op.to.Key()] = avax.UTXOID{TxID: uniqueTransferTx.txID, OutputIndex: 1 - avaxOutputIndex}
				case updateStatus:
					unsignedTx := &OperationTx{
						BaseTx: BaseTx{
							avax.BaseTx{
								NetworkID:    networkID,
								BlockchainID: chainID,
								Outs: []*avax.TransferableOutput{{
									Asset: avax.Asset{ID: avaxID},
									Out: &secp256k1fx.TransferOutput{
										Amt: avaxFundedAmt - testTxFee,
										OutputOwners: secp256k1fx.OutputOwners{
											Threshold: 1,
											Addrs:     []ids.ShortID{keys[0].PublicKey().Address()},
										},
									},
								}},
								Ins: []*avax.TransferableInput{
									{ // This input is for the transaction fee
										UTXOID: *avaxFundedUtxoID,
										Asset:  avax.Asset{ID: avaxID},
										In: &secp256k1fx.TransferInput{
											Amt: avaxFundedAmt,
											Input: secp256k1fx.Input{
												SigIndices: []uint32{0},
											},
										},
									},
								},
							},
						},
						Ops: []*Operation{
							{
								Asset:   avax.Asset{ID: managedAssetID},
								UTXOIDs: []*avax.UTXOID{&updateStatusUtxoID},
								Op: &secp256k1fx.UpdateManagedAssetOperation{
									Input: secp256k1fx.Input{
										SigIndices: []uint32{0},
									},
									ManagedAssetStatusOutput: secp256k1fx.ManagedAssetStatusOutput{
										Frozen:  op.frozen,
										Manager: op.manager,
									},
								},
							},
						},
					}

					if op.mint {
						unsignedTx.Ops[0].Op.(*secp256k1fx.UpdateManagedAssetOperation).Mint = true
						transferOut := secp256k1fx.TransferOutput{
							Amt: op.mintAmt,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{op.mintTo},
							},
						}
						unsignedTx.Ops[0].Op.(*secp256k1fx.UpdateManagedAssetOperation).TransferOutput = transferOut
					}

					updateStatusTx := &Tx{
						UnsignedTx: unsignedTx,
					}

					// One signature to spend the tx fee, one signature to transfer the managed asset
					err = updateStatusTx.SignSECP256K1Fx(vm.codec, currentCodecVersion, [][]*crypto.PrivateKeySECP256K1R{feeSigner, op.keys})
					require.NoError(t, err)

					// Verify and accept the transaction
					uniqueUpdateStatusTx, err := vm.parseTx(updateStatusTx.Bytes())
					require.NoError(t, err)
					err = uniqueUpdateStatusTx.Verify()
					if !step.shouldFailVerify {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
						continue
					}
					err = uniqueUpdateStatusTx.Accept()
					require.NoError(t, err)

					avaxFundedAmt -= testTxFee
					avaxFundedUtxoID = &avax.UTXOID{TxID: uniqueUpdateStatusTx.ID(), OutputIndex: 0}

					updateStatusUtxoID = avax.UTXOID{TxID: uniqueUpdateStatusTx.ID(), OutputIndex: 1}
					if op.mint {
						managedAssetFundedAmt[op.mintTo.Key()] += op.mintAmt
						managedAssetFundedUtxoIDs[op.mintTo.Key()] = avax.UTXOID{TxID: updateStatusTx.ID(), OutputIndex: 2}
					}
				}
			}
		})
	}
}

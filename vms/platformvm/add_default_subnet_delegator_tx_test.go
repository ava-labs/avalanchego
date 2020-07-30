// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

// func TestAddDefaultSubnetDelegatorTxSyntacticVerify(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()

// 	nodeID := keys[0].PublicKey().Address()
// 	destination := nodeID

// 	// Case : tx is nil
// 	var tx *addDefaultSubnetDelegatorTx
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because tx is nil")
// 	}

// 	// Case: Tx ID is nil
// 	tx, err := vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.id = ids.ID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because ID is nil")
// 	}

// 	// Case: Wrong network ID
// 	tx, err = vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.NetworkID = tx.NetworkID + 1
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because the wrong network ID was used")
// 	}

// 	// Case: Missing Node ID
// 	tx, err = vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	tx.NodeID = ids.ShortID{}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because NodeID is nil")
// 	}

// 	// Case: Not enough weight
// 	tx, err = vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount-1,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because of not enough weight")
// 	}

// 	// Case: Validation length is too short
// 	tx, err = vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix())-1,
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	)
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	err = tx.SyntacticVerify()
// 	if err == nil {
// 		t.Fatal("should have errored because validation length too short")
// 	}

// 	// Case: Validation length is too long
// 	if tx, err = vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix())+1,
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err == nil {
// 		t.Fatal("should have errored because validation length too long")
// 	}

// 	// Case: Valid
// 	if tx, err = vm.newAddDefaultSubnetDelegatorTx(
// 		MinimumStakeAmount,
// 		uint64(defaultValidateStartTime.Unix()),
// 		uint64(defaultValidateEndTime.Unix()),
// 		nodeID,
// 		destination,
// 		[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 	); err != nil {
// 		t.Fatal(err)
// 	} else if err := tx.SyntacticVerify(); err != nil {
// 		t.Fatal(err)
// 	}
// }

// func TestAddDefaultSubnetDelegatorTxSemanticVerify(t *testing.T) {
// 	vm := defaultVM()
// 	vm.Ctx.Lock.Lock()
// 	defer func() {
// 		vm.Shutdown()
// 		vm.Ctx.Lock.Unlock()
// 	}()
// 	nodeID := keys[0].PublicKey().Address()
// 	destination := nodeID
// 	vdb := versiondb.New(vm.DB) // so tests don't interfere with one another
// 	currentTimestamp, err := vm.getTimestamp(vm.DB)
// 	if err != nil {
// 		t.Fatal(err)
// 	}

// 	keyIntf, err := vm.factory.NewPrivateKey()
// 	if err != nil {
// 		t.Fatal(err)
// 	}
// 	newValidatorKey := keyIntf.(*crypto.PrivateKeySECP256K1R)
// 	newValidatorID := newValidatorKey.PublicKey().Address()
// 	newValidatorStartTime := uint64(defaultValidateStartTime.Add(5 * time.Second).Unix())
// 	newValidatorEndTime := uint64(defaultValidateEndTime.Add(-5 * time.Second).Unix())
// 	// [addValidator] adds a new validator to the default subnet's pending validator set
// 	addValidator := func(db database.Database) {
// 		if tx, err := vm.newAddDefaultSubnetValidatorTx(
// 			MinimumStakeAmount,                      // stake amount
// 			newValidatorStartTime,                   // start time
// 			newValidatorEndTime,                     // end time
// 			newValidatorID,                          // node ID
// 			destination,                             // destination
// 			NumberOfShares,                          // subnet
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
// 		); err != nil {
// 			t.Fatal(err)
// 		} else if err := vm.putPendingValidators(
// 			db,
// 			&EventHeap{
// 				SortByStartTime: true,
// 				Txs:             []TimedTx{tx},
// 			},
// 			constants.DefaultSubnetID,
// 		); err != nil {
// 			t.Fatal(err)
// 		}
// 	}

// 	type test struct {
// 		stakeAmount uint64
// 		startTime   uint64
// 		endTime     uint64
// 		nodeID      ids.ShortID
// 		destination ids.ShortID
// 		feeKeys     []*crypto.PrivateKeySECP256K1R
// 		setup       func(db database.Database)
// 		shouldErr   bool
// 		errMsg      string
// 	}

// 	tests := []test{
// 		{
// 			MinimumStakeAmount,
// 			uint64(defaultValidateStartTime.Unix()),
// 			uint64(defaultValidateEndTime.Unix()) + 1,
// 			nodeID,
// 			destination,
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 			nil,
// 			true,
// 			"validator stops validating default subnet earlier than non-default subnet",
// 		},
// 		{
// 			MinimumStakeAmount,
// 			uint64(defaultValidateStartTime.Unix()),
// 			uint64(defaultValidateEndTime.Unix()) + 1,
// 			nodeID,
// 			destination,
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 			nil,
// 			true,
// 			"end time is after the default subnets end time",
// 		},
// 		{
// 			MinimumStakeAmount,
// 			uint64(defaultValidateStartTime.Add(5 * time.Second).Unix()),
// 			uint64(defaultValidateStartTime.Add(-5 * time.Second).Unix()),
// 			newValidatorID,
// 			destination,
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 			nil,
// 			true,
// 			"validator not in the current or pending validator sets of the default subnet",
// 		},
// 		{
// 			MinimumStakeAmount,
// 			newValidatorStartTime - 1, // start validating non-default subnet before default subnet
// 			newValidatorEndTime,
// 			newValidatorID,
// 			destination,
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 			addValidator,
// 			true,
// 			"validator starts validating non-default subnet before default subnet",
// 		},
// 		{
// 			MinimumStakeAmount,
// 			newValidatorStartTime,
// 			newValidatorEndTime + 1, // stop validating non-default subnet after stopping validating default subnet
// 			newValidatorID,
// 			destination,
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 			addValidator,
// 			true,
// 			"validator stops validating default subnet before non-default subnet",
// 		},
// 		{
// 			MinimumStakeAmount,
// 			newValidatorStartTime, // same start time as for default subnet
// 			newValidatorEndTime,   // same end time as for default subnet
// 			newValidatorID,
// 			destination,
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]},
// 			addValidator,
// 			false,
// 			"",
// 		},
// 		{
// 			MinimumStakeAmount, // weight
// 			uint64(currentTimestamp.Unix()),
// 			uint64(defaultValidateEndTime.Unix()),
// 			nodeID,                                  // node ID
// 			destination,                             // destination
// 			[]*crypto.PrivateKeySECP256K1R{keys[0]}, // tx fee payer
// 			nil,
// 			true,
// 			"starts validating at current timestamp",
// 		},
// 		{
// 			MinimumStakeAmount,                      // weight
// 			uint64(defaultValidateStartTime.Unix()), // start time
// 			uint64(defaultValidateEndTime.Unix()),   // end time
// 			nodeID,                                  // node ID
// 			destination,                             // destination
// 			[]*crypto.PrivateKeySECP256K1R{keys[1]}, // tx fee payer
// 			func(db database.Database) { // Remove all UTXOs owned by keys[1]
// 				utxoIDs, err := vm.getReferencingUTXOs(db, keys[1].PublicKey().Address().Bytes())
// 				if err != nil {
// 					t.Fatal(err)
// 				}
// 				for _, utxoID := range utxoIDs.List() {
// 					if err := vm.removeUTXO(db, utxoID); err != nil {
// 						t.Fatal(err)
// 					}
// 				}
// 			},
// 			true,
// 			"tx fee paying key has no funds",
// 		},
// 	}

// 	for _, tt := range tests {
// 		vdb.Abort()
// 		tx, err := vm.newAddDefaultSubnetDelegatorTx(
// 			tt.stakeAmount,
// 			tt.startTime,
// 			tt.endTime,
// 			tt.nodeID,
// 			tt.destination,
// 			tt.feeKeys,
// 		)
// 		if err != nil {
// 			t.Fatal(err)
// 		}
// 		if tt.setup != nil {
// 			tt.setup(vdb)
// 		}
// 		if _, _, _, _, err := tx.SemanticVerify(vdb); err != nil && !tt.shouldErr {
// 			t.Fatalf("got unexpected error %s", err)
// 		} else if err == nil && tt.shouldErr {
// 			t.Fatalf("expected error '%s' but got none", tt.errMsg)
// 		}
// 	}
// }

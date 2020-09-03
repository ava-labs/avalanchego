// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"math"
	"testing"
	"time"

	"github.com/ava-labs/gecko/database/versiondb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
	"github.com/ava-labs/gecko/vms/components/avax"
	"github.com/ava-labs/gecko/vms/secp256k1fx"
)

func TestAddValidatorTxSyntacticVerify(t *testing.T) {
	vm, _ := defaultVM(t)
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()

	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	// Case: tx is nil
	var unsignedTx *UnsignedAddValidatorTx
	if err := unsignedTx.Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 3: Wrong Network ID
	tx, err := vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).NetworkID++
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case: Node ID is nil
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Validator.NodeID = ids.ShortID{ID: nil}
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because node ID is nil")
	}

	// Case: Stake owner has no addresses
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Stake = []*avax.TransferableOutput{{
		Asset: avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: vm.minStake,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Threshold: 1,
				Addrs:     nil,
			},
		},
	}}
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because stake owner has no addresses")
	}

	// Case: Rewards owner has no addresses
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).RewardsOwner = &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     nil,
	}
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because rewards owner has no addresses")
	}

	// Case: Stake amount too small
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Validator.Wght-- // 1 less than minimum amount
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because stake amount too small")
	}

	// Case: Too many shares
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Shares++ // 1 more than max amount
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because of too many shares")
	}

	// Case: Validation length is too short
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Validator.End-- // 1 less than min duration
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case: Validation length is negative
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MinimumStakingDuration).Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Validator.End = tx.UnsignedTx.(*UnsignedAddValidatorTx).Validator.Start - 1
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case: Validation length is too long
	tx, err = vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateStartTime.Add(MaximumStakingDuration).Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	)
	if err != nil {
		t.Fatal(err)
	}
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Validator.End++ // 1 more than maximum duration
	// This tx was syntactically verified when it was created...pretend it wasn't so we don't use cache
	tx.UnsignedTx.(*UnsignedAddValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err == nil {
		t.Fatal("should have errored because validation length too long")
	}

	// Case: Valid
	if tx, err := vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if err := tx.UnsignedTx.(*UnsignedAddValidatorTx).Verify(vm.Ctx, vm.codec, vm.txFee, vm.Ctx.AVAXAssetID, vm.minStake); err != nil {
		t.Fatal(err)
	}
}

// Test AddValidatorTx.SemanticVerify
func TestAddValidatorTxSemanticVerify(t *testing.T) {
	vm, _ := defaultVM(t)
	vm.Ctx.Lock.Lock()
	defer func() {
		vm.Shutdown()
		vm.Ctx.Lock.Unlock()
	}()
	vDB := versiondb.New(vm.DB)

	key, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	nodeID := key.PublicKey().Address()

	// Case: Validator's start time too early
	if tx, err := vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix())-1,
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vDB, tx); err == nil {
		t.Fatal("should've errored because start time too early")
	}
	vDB.Abort()

	// Case: Validator already validating primary network
	if tx, err := vm.newAddValidatorTx(
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID, // node ID
		nodeID, // reward address
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vDB, tx); err == nil {
		t.Fatal("should've errored because validator already validating")
	}
	vDB.Abort()

	// Case: Validator in pending validator set of primary network
	key2, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	startTime := defaultGenesisTime.Add(1 * time.Second)
	tx, err := vm.newAddValidatorTx(
		vm.minStake,                                          // stake amount
		uint64(startTime.Unix()),                             // start time
		uint64(startTime.Add(MinimumStakingDuration).Unix()), // end time
		nodeID, // node ID
		key2.PublicKey().Address(),              // reward address
		NumberOfShares,                          // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]}, // key
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.addStaker(vDB, constants.PrimaryNetworkID, tx); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vDB, tx); err == nil {
		t.Fatal("should have failed because validator in pending validator set")
	}
	vDB.Abort()

	// Case: Validator doesn't have enough tokens to cover stake amount
	if _, err := vm.newAddValidatorTx( // create the tx
		vm.minStake,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	}
	// Remove all UTXOs owned by keys[0]
	utxoIDs, err := vm.getReferencingUTXOs(vDB, keys[0].PublicKey().Address().Bytes(), ids.Empty, math.MaxInt32)
	if err != nil {
		t.Fatal(err)
	}
	for _, utxoID := range utxoIDs.List() {
		if err := vm.removeUTXO(vDB, utxoID); err != nil {
			t.Fatal(err)
		}
	}
	// Now keys[0] has no funds
	if _, _, _, _, err := tx.UnsignedTx.(UnsignedProposalTx).SemanticVerify(vm, vDB, tx); err == nil {
		t.Fatal("should have failed because tx fee paying key has no funds")
	}
	vDB.Abort()
}

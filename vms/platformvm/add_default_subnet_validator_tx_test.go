// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"testing"
	"time"

	"github.com/ava-labs/gecko/vms/secp256k1fx"

	"github.com/ava-labs/gecko/database/versiondb"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/constants"
	"github.com/ava-labs/gecko/utils/crypto"
)

func TestAddDefaultSubnetValidatorTxSyntacticVerify(t *testing.T) {
	vm := defaultVM()
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
	var unsignedTx *UnsignedAddDefaultSubnetValidatorTx
	if err := unsignedTx.Verify(); err == nil {
		t.Fatal("should have errored because tx is nil")
	}

	// Case 2: ID is nil
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).id = ids.ID{ID: nil}
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because ID is nil")
	}

	// Case 3: Wrong Network ID
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).NetworkID++
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because the wrong network ID was used")
	}

	// Case: Node ID is nil
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).NodeID = ids.ShortID{ID: nil}
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because node ID is nil")
	}

	// Case: Stake owner has no addresses
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).StakeOwner = &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     nil,
	}
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because stake owner has no addresses")
	}

	// Case: Rewards owner has no addresses
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).RewardsOwner = &secp256k1fx.OutputOwners{
		Locktime:  0,
		Threshold: 1,
		Addrs:     nil,
	}
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because rewards owner has no addresses")
	}

	// Case: Stake amount too small
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Validator.Wght-- // 1 less than minimum amount
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because stake amount too small")
	}

	// Case: Too many shares
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Shares++ // 1 more than max amount
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because of too many shares")
	}

	// Case: Validation length is too short
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).End-- // 1 less than min duration
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case: Validation length is negative
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).End = tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Start - 1
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because validation length too short")
	}

	// Case: Validation length is too long
	tx, err = vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
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
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).End++ // 1 more than maximum duration
	// This tx was syntactically verified when it was created...pretend it wan't so we don't use cache
	tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).syntacticallyVerified = false
	if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err == nil {
		t.Fatal("should have errored because validation length too long")
	}

	// Case: Valid
	if tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if err := tx.UnsignedProposalTx.(*UnsignedAddDefaultSubnetValidatorTx).Verify(); err != nil {
		t.Fatal(err)
	}
}

// Test AddDefaultSubnetValidatorTx.SemanticVerify
func TestAddDefaultSubnetValidatorTxSemanticVerify(t *testing.T) {
	vm := defaultVM()
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
	if tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix())-1,
		uint64(defaultValidateEndTime.Unix()),
		nodeID,
		nodeID,
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vDB, tx); err == nil {
		t.Fatal("should've errored because start time too early")
	}
	vDB.Abort()

	// Case: Validator already validating default subnet
	if tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,
		uint64(defaultValidateStartTime.Unix()),
		uint64(defaultValidateEndTime.Unix()),
		nodeID, // node ID
		nodeID, // destination
		NumberOfShares,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vDB, tx); err == nil {
		t.Fatal("should've errored because validator already validating")
	}
	vDB.Abort()

	// Case: Validator in pending validator set of default subnet
	key2, err := vm.factory.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	startTime := defaultGenesisTime.Add(1 * time.Second)
	tx, err := vm.newAddDefaultSubnetValidatorTx(
		MinimumStakeAmount,       // stake amount
		uint64(startTime.Unix()), // start time
		uint64(startTime.Add(MinimumStakingDuration).Unix()), // end time
		key2.PublicKey().Address(),                           // node ID
		nodeID,                                               // destination
		NumberOfShares,                                       // shares
		[]*crypto.PrivateKeySECP256K1R{keys[0]},              // key
	)
	if err != nil {
		t.Fatal(err)
	} else if err := vm.putPendingValidators(vDB, // Put validator in pending validator set
		&EventHeap{
			SortByStartTime: true,
			Txs:             []*ProposalTx{tx},
		},
		constants.DefaultSubnetID,
	); err != nil {
		t.Fatal(err)
	} else if _, _, _, _, err := tx.SemanticVerify(vDB, tx); err == nil {
		t.Fatal("should have failed because validator in pending validator set")
	}
	vDB.Abort()

	// Case: Validator doesn't have enough tokens to cover stake amount
	if _, err := vm.newAddDefaultSubnetValidatorTx( // create the tx
		MinimumStakeAmount,
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
	utxoIDs, err := vm.getReferencingUTXOs(vDB, keys[0].PublicKey().Address().Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, utxoID := range utxoIDs.List() {
		if err := vm.removeUTXO(vDB, utxoID); err != nil {
			t.Fatal(err)
		}
	}
	// Now keys[0] has no funds
	if _, _, _, _, err := tx.SemanticVerify(vDB, tx); err == nil {
		t.Fatal("should have failed because tx fee paying key has no funds")
	}
	vDB.Abort()
}

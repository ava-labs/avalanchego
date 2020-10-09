// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"encoding/json"
	"fmt"

	"strings"
	"testing"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	cjson "github.com/ava-labs/avalanchego/utils/json"
)

var (
	// Test user username
	testUsername string = "ScoobyUser"

	// Test user password, must meet minimum complexity/length requirements
	testPassword string = "ShaggyPassword1Zoinks!"

	// Bytes docoded from CB58 "ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
	testPrivateKey []byte = []byte{
		0x56, 0x28, 0x9e, 0x99, 0xc9, 0x4b, 0x69, 0x12,
		0xbf, 0xc1, 0x2a, 0xdc, 0x09, 0x3c, 0x9b, 0x51,
		0x12, 0x4f, 0x0d, 0xc5, 0x4a, 0xc7, 0xa7, 0x66,
		0xb2, 0xbc, 0x5c, 0xcf, 0x55, 0x8d, 0x80, 0x27,
	}

	// 3cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c
	// Platform address resulting from the above private key
	testAddress string = "P-testing18jma8ppw3nhx5r4ap8clazz0dps7rv5umpc36y"
)

func defaultService(t *testing.T) *Service {
	vm, _ := defaultVM()
	vm.Ctx.Lock.Lock()
	defer vm.Ctx.Lock.Unlock()
	ks := keystore.CreateTestKeystore()
	if err := ks.AddUser(testUsername, testPassword); err != nil {
		t.Fatal(err)
	}
	vm.SnowmanVM.Ctx.Keystore = ks.NewBlockchainKeyStore(vm.SnowmanVM.Ctx.ChainID)
	return &Service{vm: vm}
}

// Give user [testUsername] control of [testPrivateKey] and keys[0] (which is funded)
func defaultAddress(t *testing.T, service *Service) {
	service.vm.Ctx.Lock.Lock()
	defer service.vm.Ctx.Lock.Unlock()
	userDB, err := service.vm.SnowmanVM.Ctx.Keystore.GetDatabase(testUsername, testPassword)
	if err != nil {
		t.Fatal(err)
	}
	user := user{db: userDB}
	pk, err := service.vm.factory.ToPrivateKey(testPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	privKey := pk.(*crypto.PrivateKeySECP256K1R)
	if err := user.putAddress(privKey); err != nil {
		t.Fatal(err)
	} else if err := user.putAddress(keys[0]); err != nil {
		t.Fatal(err)
	}
}

func TestAddValidator(t *testing.T) {
	expectedJSONString := `{"username":"","password":"","from":null,"changeAddr":"","startTime":"0","endTime":"0","nodeID":"","rewardAddress":"","delegationFeeRate":"0.0000"}`
	args := AddValidatorArgs{}
	bytes, err := json.Marshal(&args)
	if err != nil {
		t.Fatal(err)
	}
	jsonString := string(bytes)
	if jsonString != expectedJSONString {
		t.Fatalf("Expected: %s\nResult: %s", expectedJSONString, jsonString)
	}
}

func TestCreateBlockchainArgsParsing(t *testing.T) {
	jsonString := `{"vmID":"lol","fxIDs":["secp256k1"], "name":"awesome", "username":"bob loblaw", "password":"yeet", "genesisData":"SkB92YpWm4Q2iPnLGCuDPZPgUQMxajqQQuz91oi3xD984f8r"}`
	args := CreateBlockchainArgs{}
	err := json.Unmarshal([]byte(jsonString), &args)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = json.Marshal(args.GenesisData); err != nil {
		t.Fatal(err)
	}
}

func TestExportKey(t *testing.T) {
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1Zoinks!","address":"` + testAddress + `"}`
	args := ExportKeyArgs{}
	err := json.Unmarshal([]byte(jsonString), &args)
	if err != nil {
		t.Fatal(err)
	}

	service := defaultService(t)
	defaultAddress(t, service)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	reply := ExportKeyReply{}
	if err := service.ExportKey(nil, &args, &reply); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(reply.PrivateKey, constants.SecretKeyPrefix) {
		t.Fatalf("ExportKeyReply is missing secret key prefix: %s", constants.SecretKeyPrefix)
	}
	privateKeyString := strings.TrimPrefix(reply.PrivateKey, constants.SecretKeyPrefix)
	privateKey := formatting.CB58{}
	if err := privateKey.FromString(privateKeyString); err != nil {
		t.Fatalf("Failed to parse key: %s", err)
	}

	if !bytes.Equal(testPrivateKey, privateKey.Bytes) {
		t.Fatalf("Expected %v, got %v", testPrivateKey, privateKey.Bytes)
	}
}

func TestImportKey(t *testing.T) {
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1Zoinks!","privateKey":"PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"}`
	args := ImportKeyArgs{}
	err := json.Unmarshal([]byte(jsonString), &args)
	if err != nil {
		t.Fatal(err)
	}

	service := defaultService(t)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	reply := api.JSONAddress{}
	if err := service.ImportKey(nil, &args, &reply); err != nil {
		t.Fatal(err)
	}
	if testAddress != reply.Address {
		t.Fatalf("Expected %q, got %q", testAddress, reply.Address)
	}
}

// Test issuing a tx, having it be dropped, and then re-issued and accepted
func TestGetTxStatus(t *testing.T) {
	service := defaultService(t)
	defaultAddress(t, service)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	// create a tx
	tx, err := service.vm.newCreateChainTx(
		testSubnet1.ID(),
		nil,
		avm.ID,
		nil,
		"chain name",
		[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
		ids.ShortEmpty, // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	arg := &GetTxStatusArgs{TxID: tx.ID()}
	var status Status
	if err := service.GetTxStatus(nil, arg, &status); err != nil {
		t.Fatal(err)
	} else if status != Unknown {
		t.Fatalf("status should be unknown but is %s", status)
		// put the chain in existing chain list
	} else if err := service.vm.mempool.IssueTx(tx); err != nil {
		t.Fatal(err)
	} else if err := service.vm.putChains(service.vm.DB, []*Tx{tx}); err != nil {
		t.Fatal(err)
	} else if _, err := service.vm.BuildBlock(); err == nil {
		t.Fatal("should have errored because chain already exists")
	} else if err := service.GetTxStatus(nil, arg, &status); err != nil {
		t.Fatal(err)
	} else if status != Dropped {
		t.Fatalf("status should be Dropped but is %s", status)
		// remove the chain from existing chain list
	} else if err := service.vm.putChains(service.vm.DB, []*Tx{}); err != nil {
		t.Fatal(err)
	} else if err := service.vm.mempool.IssueTx(tx); err != nil {
		t.Fatal(err)
	} else if block, err := service.vm.BuildBlock(); err != nil {
		t.Fatal(err)
	} else if blk, ok := block.(*StandardBlock); !ok {
		t.Fatalf("should be *StandardBlock but it %T", blk)
	} else if err := blk.Verify(); err != nil {
		t.Fatal(err)
	} else if err := blk.Accept(); err != nil {
		t.Fatal(err)
	} else if err := service.GetTxStatus(nil, arg, &status); err != nil {
		t.Fatal(err)
	} else if status != Committed {
		t.Fatalf("status should be Committed but is %s", status)
	}
}

// Test issuing and then retrieving a transaction
func TestGetTx(t *testing.T) {
	service := defaultService(t)
	defaultAddress(t, service)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	type test struct {
		description string
		createTx    func() (*Tx, error)
	}

	tests := []test{
		{
			"standard block",
			func() (*Tx, error) {
				return service.vm.newCreateChainTx( // Test GetTx works for standard blocks
					testSubnet1.ID(),
					nil,
					avm.ID,
					nil,
					"chain name",
					[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
					keys[0].PublicKey().Address(), // change addr
				)
			},
		},
		{
			"proposal block",
			func() (*Tx, error) {
				return service.vm.newAddValidatorTx( // Test GetTx works for proposal blocks
					service.vm.minValidatorStake,
					uint64(service.vm.clock.Time().Add(syncBound).Unix()),
					uint64(service.vm.clock.Time().Add(syncBound).Add(defaultMinStakingDuration).Unix()),
					ids.GenerateTestShortID(),
					ids.GenerateTestShortID(),
					0,
					[]*crypto.PrivateKeySECP256K1R{keys[0]},
					keys[0].PublicKey().Address(), // change addr
				)
			},
		},
		{
			"atomic block",
			func() (*Tx, error) {
				return service.vm.newExportTx( // Test GetTx works for proposal blocks
					100,
					service.vm.Ctx.XChainID,
					ids.GenerateTestShortID(),
					[]*crypto.PrivateKeySECP256K1R{keys[0]},
					keys[0].PublicKey().Address(), // change addr
				)
			},
		},
	}

	for _, test := range tests {
		tx, err := test.createTx()
		if err != nil {
			t.Fatalf("failed test '%s': %s", test.description, err)
		}
		arg := &GetTxArgs{TxID: tx.ID()}
		var response GetTxResponse
		if err := service.GetTx(nil, arg, &response); err == nil {
			t.Fatalf("failed test '%s': haven't issued tx yet so shouldn't be able to get it", test.description)
		} else if err := service.vm.mempool.IssueTx(tx); err != nil {
			t.Fatalf("failed test '%s': %s", test.description, err)
		} else if block, err := service.vm.BuildBlock(); err != nil {
			t.Fatalf("failed test '%s': %s", test.description, err)
		} else if err := block.Verify(); err != nil {
			t.Fatalf("failed test '%s': %s", test.description, err)
		} else if err := block.Accept(); err != nil {
			t.Fatalf("failed test '%s': %s", test.description, err)
		} else if blk, ok := block.(*ProposalBlock); ok { // For proposal blocks, commit them
			if options, err := blk.Options(); err != nil {
				t.Fatalf("failed test '%s': %s", test.description, err)
			} else if commit, ok := options[0].(*Commit); !ok {
				t.Fatalf("failed test '%s': should prefer to commit", test.description)
			} else if err := commit.Verify(); err != nil {
				t.Fatalf("failed test '%s': %s", test.description, err)
			} else if err := commit.Accept(); err != nil {
				t.Fatalf("failed test '%s': %s", test.description, err)
			}
		} else if err := service.GetTx(nil, arg, &response); err != nil {
			t.Fatalf("failed test '%s': %s", test.description, err)
		} else if !bytes.Equal(response.Tx.Bytes, tx.Bytes()) {
			t.Fatalf("failed test '%s': byte representation of tx in response is incorrect", test.description)
		}
	}
}

// Test method GetBalance
func TestGetBalance(t *testing.T) {
	service := defaultService(t)
	defaultAddress(t, service)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	// Ensure GetStake is correct for each of the genesis validators
	genesis, _ := defaultGenesis()
	for _, utxo := range genesis.UTXOs {
		request := api.JSONAddress{
			Address: fmt.Sprintf("P-%s", utxo.Address),
		}
		reply := GetBalanceResponse{}
		if err := service.GetBalance(nil, &request, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Balance != cjson.Uint64(defaultBalance) {
			t.Fatalf("Wrong balance. Expected %d ; Returned %d", reply.Balance, defaultBalance)
		}
		if reply.Unlocked != cjson.Uint64(defaultBalance) {
			t.Fatalf("Wrong unlocked balance. Expected %d ; Returned %d", reply.Unlocked, defaultBalance)
		}
		if reply.LockedStakeable != 0 {
			t.Fatalf("Wrong locked stakeable balance. Expected %d ; Returned %d", reply.LockedStakeable, 0)
		}
		if reply.LockedNotStakeable != 0 {
			t.Fatalf("Wrong locked not stakeable balance. Expected %d ; Returned %d", reply.LockedNotStakeable, 0)
		}
	}
}

// Test method GetStake
func TestGetStake(t *testing.T) {
	service := defaultService(t)
	defaultAddress(t, service)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	// Ensure GetStake is correct for each of the genesis validators
	genesis, _ := defaultGenesis()
	addrs := []string{}
	for _, validator := range genesis.Validators {
		addr := fmt.Sprintf("P-%s", validator.RewardOwner.Addresses[0])
		addrs = append(addrs, addr)
		args := api.JSONAddresses{
			Addresses: []string{addr},
		}
		response := GetStakeReply{}
		if err := service.GetStake(nil, &args, &response); err != nil {
			t.Fatal(err)
		}
		if uint64(response.Staked) != defaultWeight {
			t.Fatalf("expected stake to be %d but is %d", defaultWeight, response.Staked)
		}
	}

	// Make sure this works for multiple addresses
	args := api.JSONAddresses{
		Addresses: addrs,
	}
	response := GetStakeReply{}
	if err := service.GetStake(nil, &args, &response); err != nil {
		t.Fatal(err)
	}
	if int(response.Staked) != len(genesis.Validators)*defaultWeight {
		t.Fatalf("expected stake to be %d but is %d", len(genesis.Validators)*defaultWeight, response.Staked)
	}

	// Make sure this works for delegators
	// Get the old amount of stake for keys[0]
	addr, _ := service.vm.FormatLocalAddress(keys[0].PublicKey().Address())
	args.Addresses = []string{addr}
	if err := service.GetStake(nil, &args, &response); err != nil {
		t.Fatal(err)
	}
	oldStake := response.Staked

	// Add a delegator
	stakeAmt := service.vm.minDelegatorStake + 12345
	tx, err := service.vm.newAddDelegatorTx(
		stakeAmt,
		uint64(defaultGenesisTime.Unix()),
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()),
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.vm.addStaker(service.vm.DB, constants.PrimaryNetworkID, &rewardTx{
		Reward: 0,
		Tx:     *tx,
	}); err != nil {
		t.Fatal(err)
	}

	// Make sure the delegator has the right stake (old stake + stakeAmt)
	if err := service.GetStake(nil, &args, &response); err != nil {
		t.Fatal(err)
	}
	if uint64(response.Staked) != uint64(oldStake)+stakeAmt {
		t.Fatalf("expected stake to be %d but is %d", uint64(oldStake)+stakeAmt, response.Staked)
	}
	oldStake = response.Staked

	// Make sure this works for pending stakers
	// Add a pending staker
	stakeAmt = service.vm.minValidatorStake + 54321
	tx, err = service.vm.newAddValidatorTx(
		stakeAmt,
		uint64(defaultGenesisTime.Unix()),
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()),
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
		0,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.vm.enqueueStaker(service.vm.DB, constants.PrimaryNetworkID, tx); err != nil {
		t.Fatal(err)
	}
	// Make sure the delegator has the right stake (old stake + stakeAmt)
	if err := service.GetStake(nil, &args, &response); err != nil {
		t.Fatal(err)
	}
	if uint64(response.Staked) != uint64(oldStake)+stakeAmt {
		t.Fatalf("expected stake to be %d but is %d", uint64(oldStake)+stakeAmt, response.Staked)
	}
	oldStake += cjson.Uint64(stakeAmt)

	// Make sure this works for pending stakers
	// Add a pending staker
	stakeAmt = service.vm.minValidatorStake + 54321
	tx, err = service.vm.newAddValidatorTx(
		stakeAmt,
		uint64(defaultGenesisTime.Unix()),
		uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix()),
		ids.GenerateTestShortID(),
		ids.GenerateTestShortID(),
		0,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	if err != nil {
		t.Fatal(err)
	}

	stake := tx.UnsignedTx.(*UnsignedAddValidatorTx).Stake
	stake = append(stake, &avax.TransferableOutput{
		Asset: avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1,
			OutputOwners: secp256k1fx.OutputOwners{
				Threshold: 1,
				Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
			},
		},
	})
	stake[0], stake[1] = stake[1], stake[0]
	tx.UnsignedTx.(*UnsignedAddValidatorTx).Stake = stake
	if err := tx.Sign(service.vm.codec, nil); err != nil {
		t.Fatal(err)
	}

	if err := service.vm.enqueueStaker(service.vm.DB, constants.PrimaryNetworkID, tx); err != nil {
		t.Fatal(err)
	}
	// Make sure the delegator has the right stake (old stake + stakeAmt)
	if err := service.GetStake(nil, &args, &response); err != nil {
		t.Fatal(err)
	}
	if uint64(response.Staked) != uint64(oldStake)+stakeAmt {
		t.Fatalf("expected stake to be %d but is %d", uint64(oldStake)+stakeAmt, response.Staked)
	}
}

// Test method GetCurrentValidators
func TestGetCurrentValidators(t *testing.T) {
	service := defaultService(t)
	defaultAddress(t, service)
	service.vm.Ctx.Lock.Lock()
	defer func() {
		if err := service.vm.Shutdown(); err != nil {
			t.Fatal(err)
		}
		service.vm.Ctx.Lock.Unlock()
	}()

	genesis, _ := defaultGenesis()

	// Call getValidators
	args := GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	response := GetCurrentValidatorsReply{}

	err := service.GetCurrentValidators(nil, &args, &response)
	switch {
	case err != nil:
		t.Fatal(err)
	case len(response.Validators) != len(genesis.Validators):
		t.Fatalf("should be %d validators but are %d", len(genesis.Validators), len(response.Validators))
	case len(response.Delegators) != 0:
		t.Fatalf("should be 0 delegators but are %d", len(response.Delegators))
	}

	for _, vdr := range genesis.Validators {
		found := false
		for i := 0; i < len(response.Validators) && !found; i++ {
			gotVdr, ok := response.Validators[i].(APIPrimaryValidator)
			switch {
			case !ok:
				t.Fatal("expected APIPrimaryValidator")
			case gotVdr.NodeID != vdr.NodeID:
			case gotVdr.EndTime != vdr.EndTime:
				t.Fatalf("expected end time of %s to be %v but got %v",
					vdr.NodeID,
					vdr.EndTime,
					gotVdr.EndTime,
				)
			case gotVdr.StartTime != vdr.StartTime:
				t.Fatalf("expected start time of %s to be %v but got %v",
					vdr.NodeID,
					vdr.StartTime,
					gotVdr.StartTime,
				)
			case gotVdr.Weight != vdr.Weight:
				t.Fatalf("expected weight of %s to be %v but got %v",
					vdr.NodeID,
					vdr.Weight,
					gotVdr.Weight,
				)
			default:
				found = true
			}
		}
		if !found {
			t.Fatalf("expected validators to contain %s but didn't", vdr.NodeID)
		}
	}

	// Add a delegator
	stakeAmt := service.vm.minDelegatorStake + 12345
	validatorNodeID := keys[1].PublicKey().Address()
	delegatorStartTime := uint64(defaultValidateStartTime.Unix())
	delegatorEndTime := uint64(defaultValidateStartTime.Add(defaultMinStakingDuration).Unix())

	tx, err := service.vm.newAddDelegatorTx(
		stakeAmt,
		delegatorStartTime,
		delegatorEndTime,
		validatorNodeID,
		ids.GenerateTestShortID(),
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.vm.addStaker(service.vm.DB, constants.PrimaryNetworkID, &rewardTx{
		Reward: 0,
		Tx:     *tx,
	}); err != nil {
		t.Fatal(err)
	}

	// Call getCurrentValidators
	args = GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	err = service.GetCurrentValidators(nil, &args, &response)
	switch {
	case err != nil:
		t.Fatal(err)
	case len(response.Validators) != len(genesis.Validators):
		t.Fatalf("should be %d validators but are %d", len(genesis.Validators), len(response.Validators))
	case len(response.Delegators) != 1:
		t.Fatalf("should be 1 delegators but are %d", len(response.Delegators))
	}

	// Make sure the delegator is there
	found := false
	for i := 0; i < len(response.Validators) && !found; i++ {
		vdr := response.Validators[i].(APIPrimaryValidator)
		if vdr.NodeID != validatorNodeID.PrefixedString(constants.NodeIDPrefix) {
			continue
		}
		found = true
		if len(vdr.Delegators) != 1 {
			t.Fatalf("%s should have 1 delegator", vdr.NodeID)
		}
		delegator := vdr.Delegators[0]
		switch {
		case delegator.NodeID != vdr.NodeID:
			t.Fatal("wrong node ID")
		case uint64(delegator.StartTime) != delegatorStartTime:
			t.Fatal("wrong start time")
		case uint64(delegator.EndTime) != delegatorEndTime:
			t.Fatal("wrong end time")
		case delegator.weight() != stakeAmt:
			t.Fatalf("wrong weight")
		}
	}
	if !found {
		t.Fatalf("didnt find delegator")
	}
}

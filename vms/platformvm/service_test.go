// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	stdjson "encoding/json"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/blocks"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	vmkeystore "github.com/ava-labs/avalanchego/vms/components/keystore"
	pchainapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/blocks/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var (
	// Test user username
	testUsername = "ScoobyUser"

	// Test user password, must meet minimum complexity/length requirements
	testPassword = "ShaggyPassword1Zoinks!"

	// Bytes decoded from CB58 "ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"
	testPrivateKey = []byte{
		0x56, 0x28, 0x9e, 0x99, 0xc9, 0x4b, 0x69, 0x12,
		0xbf, 0xc1, 0x2a, 0xdc, 0x09, 0x3c, 0x9b, 0x51,
		0x12, 0x4f, 0x0d, 0xc5, 0x4a, 0xc7, 0xa7, 0x66,
		0xb2, 0xbc, 0x5c, 0xcf, 0x55, 0x8d, 0x80, 0x27,
	}

	// 3cb7d3842e8cee6a0ebd09f1fe884f6861e1b29c
	// Platform address resulting from the above private key
	testAddress = "P-testing18jma8ppw3nhx5r4ap8clazz0dps7rv5umpc36y"

	encodings = []formatting.Encoding{
		formatting.JSON, formatting.Hex,
	}
)

func defaultService(t *testing.T) (*Service, *mutableSharedMemory) {
	vm, _, mutableSharedMemory := defaultVM()
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	ks := keystore.New(logging.NoLog{}, manager.NewMemDB(version.Semantic1_0_0))
	err := ks.CreateUser(testUsername, testPassword)
	require.NoError(t, err)

	vm.ctx.Keystore = ks.NewBlockchainKeyStore(vm.ctx.ChainID)
	return &Service{
		vm:          vm,
		addrManager: avax.NewAddressManager(vm.ctx),
		stakerAttributesCache: &cache.LRU[ids.ID, *stakerAttributes]{
			Size: stakerAttributesCacheSize,
		},
	}, mutableSharedMemory
}

// Give user [testUsername] control of [testPrivateKey] and keys[0] (which is funded)
func defaultAddress(t *testing.T, service *Service) {
	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()
	user, err := vmkeystore.NewUserFromKeystore(service.vm.ctx.Keystore, testUsername, testPassword)
	require.NoError(t, err)

	pk, err := testKeyFactory.ToPrivateKey(testPrivateKey)
	require.NoError(t, err)

	privKey := pk.(*crypto.PrivateKeySECP256K1R)
	err = user.PutKeys(privKey, keys[0])
	require.NoError(t, err)
}

func TestAddValidator(t *testing.T) {
	expectedJSONString := `{"username":"","password":"","from":null,"changeAddr":"","txID":"11111111111111111111111111111111LpoYY","startTime":"0","endTime":"0","weight":"0","nodeID":"NodeID-111111111111111111116DBWJs","rewardAddress":"","delegationFeeRate":"0.0000"}`
	args := AddValidatorArgs{}
	bytes, err := stdjson.Marshal(&args)
	require.NoError(t, err)
	require.Equal(t, expectedJSONString, string(bytes))
}

func TestCreateBlockchainArgsParsing(t *testing.T) {
	jsonString := `{"vmID":"lol","fxIDs":["secp256k1"], "name":"awesome", "username":"bob loblaw", "password":"yeet", "genesisData":"SkB92YpWm4Q2iPnLGCuDPZPgUQMxajqQQuz91oi3xD984f8r"}`
	args := CreateBlockchainArgs{}
	err := stdjson.Unmarshal([]byte(jsonString), &args)
	require.NoError(t, err)

	_, err = stdjson.Marshal(args.GenesisData)
	require.NoError(t, err)
}

func TestExportKey(t *testing.T) {
	require := require.New(t)
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1Zoinks!","address":"` + testAddress + `"}`
	args := ExportKeyArgs{}
	err := stdjson.Unmarshal([]byte(jsonString), &args)
	require.NoError(err)

	service, _ := defaultService(t)
	defaultAddress(t, service)
	service.vm.ctx.Lock.Lock()
	defer func() {
		err := service.vm.Shutdown(context.Background())
		require.NoError(err)
		service.vm.ctx.Lock.Unlock()
	}()

	reply := ExportKeyReply{}
	err = service.ExportKey(nil, &args, &reply)
	require.NoError(err)

	require.Equal(testPrivateKey, reply.PrivateKey.Bytes())
}

func TestImportKey(t *testing.T) {
	require := require.New(t)
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1Zoinks!","privateKey":"PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"}`
	args := ImportKeyArgs{}
	err := stdjson.Unmarshal([]byte(jsonString), &args)
	require.NoError(err)

	service, _ := defaultService(t)
	service.vm.ctx.Lock.Lock()
	defer func() {
		err := service.vm.Shutdown(context.Background())
		require.NoError(err)
		service.vm.ctx.Lock.Unlock()
	}()

	reply := api.JSONAddress{}
	err = service.ImportKey(nil, &args, &reply)
	require.NoError(err)
	require.Equal(testAddress, reply.Address)
}

// Test issuing a tx and accepted
func TestGetTxStatus(t *testing.T) {
	require := require.New(t)
	service, mutableSharedMemory := defaultService(t)
	defaultAddress(t, service)
	service.vm.ctx.Lock.Lock()
	defer func() {
		err := service.vm.Shutdown(context.Background())
		require.NoError(err)
		service.vm.ctx.Lock.Unlock()
	}()

	factory := crypto.FactorySECP256K1R{}
	recipientKeyIntf, err := factory.NewPrivateKey()
	require.NoError(err)

	recipientKey := recipientKeyIntf.(*crypto.PrivateKeySECP256K1R)

	m := atomic.NewMemory(prefixdb.New([]byte{}, service.vm.dbManager.Current().Database))

	sm := m.NewSharedMemory(service.vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(xChainID)

	// #nosec G404
	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.GenerateTestID(),
			OutputIndex: rand.Uint32(),
		},
		Asset: avax.Asset{ID: avaxAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1234567,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Addrs:     []ids.ShortID{recipientKey.PublicKey().Address()},
				Threshold: 1,
			},
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.Version, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	err = peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		service.vm.ctx.ChainID: {
			PutRequests: []*atomic.Element{
				{
					Key:   inputID[:],
					Value: utxoBytes,
					Traits: [][]byte{
						recipientKey.PublicKey().Address().Bytes(),
					},
				},
			},
		},
	})
	require.NoError(err)

	oldSharedMemory := mutableSharedMemory.SharedMemory
	mutableSharedMemory.SharedMemory = sm

	tx, err := service.vm.txBuilder.NewImportTx(xChainID, ids.ShortEmpty, []*crypto.PrivateKeySECP256K1R{recipientKey}, ids.ShortEmpty)
	require.NoError(err)

	mutableSharedMemory.SharedMemory = oldSharedMemory

	var (
		arg  = &GetTxStatusArgs{TxID: tx.ID()}
		resp GetTxStatusResponse
	)
	err = service.GetTxStatus(nil, arg, &resp)
	require.NoError(err)
	require.Equal(status.Unknown, resp.Status)
	require.Zero(resp.Reason)

	// put the chain in existing chain list
	err = service.vm.Builder.AddUnverifiedTx(tx)
	require.Error(err)

	mutableSharedMemory.SharedMemory = sm

	err = service.vm.Builder.AddUnverifiedTx(tx)
	require.NoError(err)

	block, err := service.vm.BuildBlock(context.Background())
	require.NoError(err)

	blk := block.(*blockexecutor.Block)
	err = blk.Verify(context.Background())
	require.NoError(err)

	err = blk.Accept(context.Background())
	require.NoError(err)

	resp = GetTxStatusResponse{} // reset
	err = service.GetTxStatus(nil, arg, &resp)
	require.NoError(err)
	require.Equal(status.Committed, resp.Status)
	require.Zero(resp.Reason)
}

// Test issuing and then retrieving a transaction
func TestGetTx(t *testing.T) {
	type test struct {
		description string
		createTx    func(service *Service) (*txs.Tx, error)
	}

	tests := []test{
		{
			"standard block",
			func(service *Service) (*txs.Tx, error) {
				return service.vm.txBuilder.NewCreateChainTx( // Test GetTx works for standard blocks
					testSubnet1.ID(),
					nil,
					constants.AVMID,
					nil,
					"chain name",
					[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
					keys[0].PublicKey().Address(), // change addr
				)
			},
		},
		{
			"proposal block",
			func(service *Service) (*txs.Tx, error) {
				return service.vm.txBuilder.NewAddValidatorTx( // Test GetTx works for proposal blocks
					service.vm.MinValidatorStake,
					uint64(service.vm.clock.Time().Add(txexecutor.SyncBound).Unix()),
					uint64(service.vm.clock.Time().Add(txexecutor.SyncBound).Add(defaultMinStakingDuration).Unix()),
					ids.GenerateTestNodeID(),
					ids.GenerateTestShortID(),
					0,
					[]*crypto.PrivateKeySECP256K1R{keys[0]},
					keys[0].PublicKey().Address(), // change addr
				)
			},
		},
		{
			"atomic block",
			func(service *Service) (*txs.Tx, error) {
				return service.vm.txBuilder.NewExportTx( // Test GetTx works for proposal blocks
					100,
					service.vm.ctx.XChainID,
					ids.GenerateTestShortID(),
					[]*crypto.PrivateKeySECP256K1R{keys[0]},
					keys[0].PublicKey().Address(), // change addr
				)
			},
		},
	}

	for _, test := range tests {
		for _, encoding := range encodings {
			testName := fmt.Sprintf("test '%s - %s'",
				test.description,
				encoding.String(),
			)
			t.Run(testName, func(t *testing.T) {
				require := require.New(t)
				service, _ := defaultService(t)
				defaultAddress(t, service)
				service.vm.ctx.Lock.Lock()

				tx, err := test.createTx(service)
				require.NoError(err)

				arg := &api.GetTxArgs{
					TxID:     tx.ID(),
					Encoding: encoding,
				}
				var response api.GetTxReply
				err = service.GetTx(nil, arg, &response)
				require.Error(err)

				err = service.vm.Builder.AddUnverifiedTx(tx)
				require.NoError(err)

				block, err := service.vm.BuildBlock(context.Background())
				require.NoError(err)

				err = block.Verify(context.Background())
				require.NoError(err)

				err = block.Accept(context.Background())
				require.NoError(err)

				if blk, ok := block.(snowman.OracleBlock); ok { // For proposal blocks, commit them
					options, err := blk.Options(context.Background())
					if !errors.Is(err, snowman.ErrNotOracle) {
						require.NoError(err)

						commit := options[0].(*blockexecutor.Block)
						_, ok := commit.Block.(*blocks.BanffCommitBlock)
						require.True(ok)

						err := commit.Verify(context.Background())
						require.NoError(err)

						err = commit.Accept(context.Background())
						require.NoError(err)
					}
				}

				err = service.GetTx(nil, arg, &response)
				require.NoError(err)

				switch encoding {
				case formatting.Hex:
					// we're always guaranteed a string for hex encodings.
					responseTxBytes, err := formatting.Decode(response.Encoding, response.Tx.(string))
					require.NoError(err)
					require.Equal(tx.Bytes(), responseTxBytes)

				case formatting.JSON:
					require.Equal(tx, response.Tx)
				}

				err = service.vm.Shutdown(context.Background())
				require.NoError(err)
				service.vm.ctx.Lock.Unlock()
			})
		}
	}
}

// Test method GetBalance
func TestGetBalance(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	defaultAddress(t, service)
	service.vm.ctx.Lock.Lock()
	defer func() {
		err := service.vm.Shutdown(context.Background())
		require.NoError(err)
		service.vm.ctx.Lock.Unlock()
	}()

	// Ensure GetStake is correct for each of the genesis validators
	genesis, _ := defaultGenesis()
	for _, utxo := range genesis.UTXOs {
		request := GetBalanceRequest{
			Addresses: []string{
				fmt.Sprintf("P-%s", utxo.Address),
			},
		}
		reply := GetBalanceResponse{}

		require.NoError(service.GetBalance(nil, &request, &reply))

		require.Equal(json.Uint64(defaultBalance), reply.Balance)
		require.Equal(json.Uint64(defaultBalance), reply.Unlocked)
		require.Equal(json.Uint64(0), reply.LockedStakeable)
		require.Equal(json.Uint64(0), reply.LockedNotStakeable)
	}
}

func TestGetStake(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	defaultAddress(t, service)
	service.vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	// Ensure GetStake is correct for each of the genesis validators
	genesis, _ := defaultGenesis()
	addrsStrs := []string{}
	for i, validator := range genesis.Validators {
		addr := fmt.Sprintf("P-%s", validator.RewardOwner.Addresses[0])
		addrsStrs = append(addrsStrs, addr)

		args := GetStakeArgs{
			api.JSONAddresses{
				Addresses: []string{addr},
			},
			formatting.Hex,
		}
		response := GetStakeReply{}
		require.NoError(service.GetStake(nil, &args, &response))
		require.EqualValues(uint64(defaultWeight), uint64(response.Staked))
		require.Len(response.Outputs, 1)

		// Unmarshal into an output
		outputBytes, err := formatting.Decode(args.Encoding, response.Outputs[0])
		require.NoError(err)

		var output avax.TransferableOutput
		_, err = txs.Codec.Unmarshal(outputBytes, &output)
		require.NoError(err)

		out := output.Out.(*secp256k1fx.TransferOutput)
		require.EqualValues(defaultWeight, out.Amount())
		require.EqualValues(1, out.Threshold)
		require.Len(out.Addrs, 1)
		require.Equal(keys[i].PublicKey().Address(), out.Addrs[0])
		require.Zero(out.Locktime)
	}

	// Make sure this works for multiple addresses
	args := GetStakeArgs{
		api.JSONAddresses{
			Addresses: addrsStrs,
		},
		formatting.Hex,
	}
	response := GetStakeReply{}
	require.NoError(service.GetStake(nil, &args, &response))
	require.EqualValues(len(genesis.Validators)*defaultWeight, response.Staked)
	require.Len(response.Outputs, len(genesis.Validators))

	for _, outputStr := range response.Outputs {
		outputBytes, err := formatting.Decode(args.Encoding, outputStr)
		require.NoError(err)

		var output avax.TransferableOutput
		_, err = txs.Codec.Unmarshal(outputBytes, &output)
		require.NoError(err)

		out := output.Out.(*secp256k1fx.TransferOutput)
		require.EqualValues(defaultWeight, out.Amount())
		require.EqualValues(1, out.Threshold)
		require.Zero(out.Locktime)
		require.Len(out.Addrs, 1)
	}

	oldStake := uint64(defaultWeight)

	// Add a delegator
	stakeAmount := service.vm.MinDelegatorStake + 12345
	delegatorNodeID := ids.NodeID(keys[0].PublicKey().Address())
	delegatorEndTime := uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix())
	tx, err := service.vm.txBuilder.NewAddDelegatorTx(
		stakeAmount,
		uint64(defaultGenesisTime.Unix()),
		delegatorEndTime,
		delegatorNodeID,
		ids.GenerateTestShortID(),
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddDelegatorTx),
		0,
	)
	require.NoError(err)

	service.vm.state.PutCurrentDelegator(staker)
	service.vm.state.AddTx(tx, status.Committed)
	require.NoError(service.vm.state.Commit())

	// Make sure the delegator addr has the right stake (old stake + stakeAmount)
	addr, _ := service.addrManager.FormatLocalAddress(keys[0].PublicKey().Address())
	args.Addresses = []string{addr}
	require.NoError(service.GetStake(nil, &args, &response))
	require.EqualValues(oldStake+stakeAmount, uint64(response.Staked))
	require.Len(response.Outputs, 2)

	// Unmarshal into transferable outputs
	outputs := make([]avax.TransferableOutput, 2)
	for i := range outputs {
		outputBytes, err := formatting.Decode(args.Encoding, response.Outputs[i])
		require.NoError(err)
		_, err = txs.Codec.Unmarshal(outputBytes, &outputs[i])
		require.NoError(err)
	}

	// Make sure the stake amount is as expected
	require.EqualValues(stakeAmount+oldStake, outputs[0].Out.Amount()+outputs[1].Out.Amount())

	oldStake = uint64(response.Staked)

	// Make sure this works for pending stakers
	// Add a pending staker
	stakeAmount = service.vm.MinValidatorStake + 54321
	pendingStakerNodeID := ids.GenerateTestNodeID()
	pendingStakerEndTime := uint64(defaultGenesisTime.Add(defaultMinStakingDuration).Unix())
	tx, err = service.vm.txBuilder.NewAddValidatorTx(
		stakeAmount,
		uint64(defaultGenesisTime.Unix()),
		pendingStakerEndTime,
		pendingStakerNodeID,
		ids.GenerateTestShortID(),
		0,
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddValidatorTx),
	)
	require.NoError(err)

	service.vm.state.PutPendingValidator(staker)
	service.vm.state.AddTx(tx, status.Committed)
	require.NoError(service.vm.state.Commit())

	// Make sure the delegator has the right stake (old stake + stakeAmount)
	require.NoError(service.GetStake(nil, &args, &response))
	require.EqualValues(oldStake+stakeAmount, response.Staked)
	require.Len(response.Outputs, 3)

	// Unmarshal
	outputs = make([]avax.TransferableOutput, 3)
	for i := range outputs {
		outputBytes, err := formatting.Decode(args.Encoding, response.Outputs[i])
		require.NoError(err)
		_, err = txs.Codec.Unmarshal(outputBytes, &outputs[i])
		require.NoError(err)
	}

	// Make sure the stake amount is as expected
	require.EqualValues(stakeAmount+oldStake, outputs[0].Out.Amount()+outputs[1].Out.Amount()+outputs[2].Out.Amount())
}

// Test method GetCurrentValidators
func TestGetCurrentValidators(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	defaultAddress(t, service)
	service.vm.ctx.Lock.Lock()
	defer func() {
		err := service.vm.Shutdown(context.Background())
		require.NoError(err)
		service.vm.ctx.Lock.Unlock()
	}()

	genesis, _ := defaultGenesis()

	// Call getValidators
	args := GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	response := GetCurrentValidatorsReply{}

	err := service.GetCurrentValidators(nil, &args, &response)
	require.NoError(err)
	require.Equal(len(genesis.Validators), len(response.Validators))

	for _, vdr := range genesis.Validators {
		found := false
		for i := 0; i < len(response.Validators) && !found; i++ {
			gotVdr := response.Validators[i].(pchainapi.PermissionlessValidator)
			if gotVdr.NodeID != vdr.NodeID {
				continue
			}

			require.Equal(vdr.EndTime, gotVdr.EndTime)
			require.Equal(vdr.StartTime, gotVdr.StartTime)
			found = true
		}
		require.True(found, "expected validators to contain %s but didn't", vdr.NodeID)
	}

	// Add a delegator
	stakeAmount := service.vm.MinDelegatorStake + 12345
	validatorNodeID := ids.NodeID(keys[1].PublicKey().Address())
	delegatorStartTime := uint64(defaultValidateStartTime.Unix())
	delegatorEndTime := uint64(defaultValidateStartTime.Add(defaultMinStakingDuration).Unix())

	tx, err := service.vm.txBuilder.NewAddDelegatorTx(
		stakeAmount,
		delegatorStartTime,
		delegatorEndTime,
		validatorNodeID,
		ids.GenerateTestShortID(),
		[]*crypto.PrivateKeySECP256K1R{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddDelegatorTx),
		0,
	)
	require.NoError(err)

	service.vm.state.PutCurrentDelegator(staker)
	service.vm.state.AddTx(tx, status.Committed)
	err = service.vm.state.Commit()
	require.NoError(err)

	// Call getCurrentValidators
	args = GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	err = service.GetCurrentValidators(nil, &args, &response)
	require.NoError(err)
	require.Equal(len(genesis.Validators), len(response.Validators))

	// Make sure the delegator is there
	found := false
	for i := 0; i < len(response.Validators) && !found; i++ {
		vdr := response.Validators[i].(pchainapi.PermissionlessValidator)
		if vdr.NodeID != validatorNodeID {
			continue
		}
		found = true

		require.Equal(1, len(vdr.Delegators))
		delegator := vdr.Delegators[0]
		require.Equal(delegator.NodeID, vdr.NodeID)
		require.Equal(uint64(delegator.StartTime), delegatorStartTime)
		require.Equal(uint64(delegator.EndTime), delegatorEndTime)
		require.Equal(uint64(delegator.Weight), stakeAmount)
	}
	require.True(found)
}

func TestGetTimestamp(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	service.vm.ctx.Lock.Lock()
	defer func() {
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	reply := GetTimestampReply{}
	require.NoError(service.GetTimestamp(nil, nil, &reply))
	require.Equal(service.vm.state.GetTimestamp(), reply.Timestamp)

	newTimestamp := reply.Timestamp.Add(time.Second)
	service.vm.state.SetTimestamp(newTimestamp)

	require.NoError(service.GetTimestamp(nil, nil, &reply))
	require.Equal(newTimestamp, reply.Timestamp)
}

func TestGetBlock(t *testing.T) {
	tests := []struct {
		name     string
		encoding formatting.Encoding
	}{
		{
			name:     "json",
			encoding: formatting.JSON,
		},
		{
			name:     "hex",
			encoding: formatting.Hex,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)
			service, _ := defaultService(t)
			service.vm.ctx.Lock.Lock()
			defer service.vm.ctx.Lock.Unlock()

			service.vm.Config.CreateAssetTxFee = 100 * defaultTxFee

			// Make a block an accept it, then check we can get it.
			tx, err := service.vm.txBuilder.NewCreateChainTx( // Test GetTx works for standard blocks
				testSubnet1.ID(),
				nil,
				constants.AVMID,
				nil,
				"chain name",
				[]*crypto.PrivateKeySECP256K1R{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
				keys[0].PublicKey().Address(), // change addr
			)
			require.NoError(err)

			preferred, err := service.vm.Builder.Preferred()
			require.NoError(err)

			statelessBlock, err := blocks.NewBanffStandardBlock(
				preferred.Timestamp(),
				preferred.ID(),
				preferred.Height()+1,
				[]*txs.Tx{tx},
			)
			require.NoError(err)

			block := service.vm.manager.NewBlock(statelessBlock)

			require.NoError(block.Verify(context.Background()))
			require.NoError(block.Accept(context.Background()))

			args := api.GetBlockArgs{
				BlockID:  block.ID(),
				Encoding: test.encoding,
			}
			response := api.GetBlockResponse{}
			err = service.GetBlock(nil, &args, &response)
			require.NoError(err)

			switch {
			case test.encoding == formatting.JSON:
				require.Equal(statelessBlock, response.Block)

				_, err = stdjson.Marshal(response)
				require.NoError(err)
			default:
				decoded, _ := formatting.Decode(response.Encoding, response.Block.(string))
				require.Equal(block.Bytes(), decoded)
			}

			require.Equal(test.encoding, response.Encoding)
		})
	}
}

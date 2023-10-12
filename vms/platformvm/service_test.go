// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	stdjson "encoding/json"

	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/keystore"
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	vmkeystore "github.com/ava-labs/avalanchego/vms/components/keystore"
	pchainapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
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
	vm, _, mutableSharedMemory := defaultVM(t)
	vm.ctx.Lock.Lock()
	defer vm.ctx.Lock.Unlock()
	ks := keystore.New(logging.NoLog{}, manager.NewMemDB(version.Semantic1_0_0))
	require.NoError(t, ks.CreateUser(testUsername, testPassword))

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
	require := require.New(t)

	service.vm.ctx.Lock.Lock()
	defer service.vm.ctx.Lock.Unlock()
	user, err := vmkeystore.NewUserFromKeystore(service.vm.ctx.Keystore, testUsername, testPassword)
	require.NoError(err)

	pk, err := testKeyFactory.ToPrivateKey(testPrivateKey)
	require.NoError(err)

	require.NoError(user.PutKeys(pk, keys[0]))
}

func TestAddValidator(t *testing.T) {
	require := require.New(t)

	expectedJSONString := `{"username":"","password":"","from":null,"changeAddr":"","txID":"11111111111111111111111111111111LpoYY","startTime":"0","endTime":"0","weight":"0","nodeID":"NodeID-111111111111111111116DBWJs","rewardAddress":"","delegationFeeRate":"0.0000"}`
	args := AddValidatorArgs{}
	bytes, err := stdjson.Marshal(&args)
	require.NoError(err)
	require.Equal(expectedJSONString, string(bytes))
}

func TestCreateBlockchainArgsParsing(t *testing.T) {
	require := require.New(t)

	jsonString := `{"vmID":"lol","fxIDs":["secp256k1"], "name":"awesome", "username":"bob loblaw", "password":"yeet", "genesisData":"SkB92YpWm4Q2iPnLGCuDPZPgUQMxajqQQuz91oi3xD984f8r"}`
	args := CreateBlockchainArgs{}
	require.NoError(stdjson.Unmarshal([]byte(jsonString), &args))

	_, err := stdjson.Marshal(args.GenesisData)
	require.NoError(err)
}

func TestExportKey(t *testing.T) {
	require := require.New(t)
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1Zoinks!","address":"` + testAddress + `"}`
	args := ExportKeyArgs{}
	require.NoError(stdjson.Unmarshal([]byte(jsonString), &args))

	service, _ := defaultService(t)
	defaultAddress(t, service)
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	reply := ExportKeyReply{}
	require.NoError(service.ExportKey(nil, &args, &reply))

	require.Equal(testPrivateKey, reply.PrivateKey.Bytes())
}

func TestImportKey(t *testing.T) {
	require := require.New(t)
	jsonString := `{"username":"ScoobyUser","password":"ShaggyPassword1Zoinks!","privateKey":"PrivateKey-ewoqjP7PxY4yr3iLTpLisriqt94hdyDFNgchSxGGztUrTXtNN"}`
	args := ImportKeyArgs{}
	require.NoError(stdjson.Unmarshal([]byte(jsonString), &args))

	service, _ := defaultService(t)
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	reply := api.JSONAddress{}
	require.NoError(service.ImportKey(nil, &args, &reply))
	require.Equal(testAddress, reply.Address)
}

// Test issuing a tx and accepted
func TestGetTxStatus(t *testing.T) {
	require := require.New(t)
	service, mutableSharedMemory := defaultService(t)
	defaultAddress(t, service)
	service.vm.ctx.Lock.Lock()
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	factory := secp256k1.Factory{}
	recipientKey, err := factory.NewPrivateKey()
	require.NoError(err)

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
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
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
	}))

	oldSharedMemory := mutableSharedMemory.SharedMemory
	mutableSharedMemory.SharedMemory = sm

	tx, err := service.vm.txBuilder.NewImportTx(xChainID, ids.ShortEmpty, []*secp256k1.PrivateKey{recipientKey}, ids.ShortEmpty)
	require.NoError(err)

	mutableSharedMemory.SharedMemory = oldSharedMemory

	service.vm.ctx.Lock.Unlock()

	var (
		arg  = &GetTxStatusArgs{TxID: tx.ID()}
		resp GetTxStatusResponse
	)
	require.NoError(service.GetTxStatus(nil, arg, &resp))
	require.Equal(status.Unknown, resp.Status)
	require.Zero(resp.Reason)

	service.vm.ctx.Lock.Lock()

	// put the chain in existing chain list
	err = service.vm.Builder.AddUnverifiedTx(tx)
	require.ErrorIs(err, database.ErrNotFound) // Missing shared memory UTXO

	mutableSharedMemory.SharedMemory = sm

	require.NoError(service.vm.Builder.AddUnverifiedTx(tx))

	block, err := service.vm.BuildBlock(context.Background())
	require.NoError(err)

	blk := block.(*blockexecutor.Block)
	require.NoError(blk.Verify(context.Background()))

	require.NoError(blk.Accept(context.Background()))

	service.vm.ctx.Lock.Unlock()

	resp = GetTxStatusResponse{} // reset
	require.NoError(service.GetTxStatus(nil, arg, &resp))
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
					[]byte{},
					constants.AVMID,
					[]ids.ID{},
					"chain name",
					[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
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
					[]*secp256k1.PrivateKey{keys[0]},
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
					[]*secp256k1.PrivateKey{keys[0]},
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

				service.vm.ctx.Lock.Unlock()

				arg := &api.GetTxArgs{
					TxID:     tx.ID(),
					Encoding: encoding,
				}
				var response api.GetTxReply
				err = service.GetTx(nil, arg, &response)
				require.ErrorIs(err, database.ErrNotFound) // We haven't issued the tx yet

				service.vm.ctx.Lock.Lock()

				require.NoError(service.vm.Builder.AddUnverifiedTx(tx))

				blk, err := service.vm.BuildBlock(context.Background())
				require.NoError(err)

				require.NoError(blk.Verify(context.Background()))

				require.NoError(blk.Accept(context.Background()))

				if blk, ok := blk.(snowman.OracleBlock); ok { // For proposal blocks, commit them
					options, err := blk.Options(context.Background())
					if !errors.Is(err, snowman.ErrNotOracle) {
						require.NoError(err)

						commit := options[0].(*blockexecutor.Block)
						require.IsType(&block.BanffCommitBlock{}, commit.Block)
						require.NoError(commit.Verify(context.Background()))
						require.NoError(commit.Accept(context.Background()))
					}
				}

				service.vm.ctx.Lock.Unlock()

				require.NoError(service.GetTx(nil, arg, &response))

				switch encoding {
				case formatting.Hex:
					// we're always guaranteed a string for hex encodings.
					var txStr string
					require.NoError(stdjson.Unmarshal(response.Tx, &txStr))
					responseTxBytes, err := formatting.Decode(response.Encoding, txStr)
					require.NoError(err)
					require.Equal(tx.Bytes(), responseTxBytes)

				case formatting.JSON:
					tx.Unsigned.InitCtx(service.vm.ctx)
					expectedTxJSON, err := stdjson.Marshal(tx)
					require.NoError(err)
					require.Equal(expectedTxJSON, []byte(response.Tx))
				}

				service.vm.ctx.Lock.Lock()
				require.NoError(service.vm.Shutdown(context.Background()))
				service.vm.ctx.Lock.Unlock()
			})
		}
	}
}

func TestGetBalance(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	defaultAddress(t, service)
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	// Ensure GetStake is correct for each of the genesis validators
	genesis, _ := defaultGenesis(t)
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
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	// Ensure GetStake is correct for each of the genesis validators
	genesis, _ := defaultGenesis(t)
	addrsStrs := []string{}
	for i, validator := range genesis.Validators {
		addr := fmt.Sprintf("P-%s", validator.RewardOwner.Addresses[0])
		addrsStrs = append(addrsStrs, addr)

		args := GetStakeArgs{
			JSONAddresses: api.JSONAddresses{
				Addresses: []string{addr},
			},
			Encoding: formatting.Hex,
		}
		response := GetStakeReply{}
		require.NoError(service.GetStake(nil, &args, &response))
		require.Equal(defaultWeight, uint64(response.Staked))
		require.Len(response.Outputs, 1)

		// Unmarshal into an output
		outputBytes, err := formatting.Decode(args.Encoding, response.Outputs[0])
		require.NoError(err)

		var output avax.TransferableOutput
		_, err = txs.Codec.Unmarshal(outputBytes, &output)
		require.NoError(err)

		out := output.Out.(*secp256k1fx.TransferOutput)
		require.Equal(defaultWeight, out.Amount())
		require.Equal(uint32(1), out.Threshold)
		require.Len(out.Addrs, 1)
		require.Equal(keys[i].PublicKey().Address(), out.Addrs[0])
		require.Zero(out.Locktime)
	}

	// Make sure this works for multiple addresses
	args := GetStakeArgs{
		JSONAddresses: api.JSONAddresses{
			Addresses: addrsStrs,
		},
		Encoding: formatting.Hex,
	}
	response := GetStakeReply{}
	require.NoError(service.GetStake(nil, &args, &response))
	require.Equal(len(genesis.Validators)*int(defaultWeight), int(response.Staked))
	require.Len(response.Outputs, len(genesis.Validators))

	for _, outputStr := range response.Outputs {
		outputBytes, err := formatting.Decode(args.Encoding, outputStr)
		require.NoError(err)

		var output avax.TransferableOutput
		_, err = txs.Codec.Unmarshal(outputBytes, &output)
		require.NoError(err)

		out := output.Out.(*secp256k1fx.TransferOutput)
		require.Equal(defaultWeight, out.Amount())
		require.Equal(uint32(1), out.Threshold)
		require.Zero(out.Locktime)
		require.Len(out.Addrs, 1)
	}

	oldStake := defaultWeight

	service.vm.ctx.Lock.Lock()

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
		[]*secp256k1.PrivateKey{keys[0]},
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

	service.vm.ctx.Lock.Unlock()

	// Make sure the delegator addr has the right stake (old stake + stakeAmount)
	addr, _ := service.addrManager.FormatLocalAddress(keys[0].PublicKey().Address())
	args.Addresses = []string{addr}
	require.NoError(service.GetStake(nil, &args, &response))
	require.Equal(oldStake+stakeAmount, uint64(response.Staked))
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
	require.Equal(stakeAmount+oldStake, outputs[0].Out.Amount()+outputs[1].Out.Amount())

	oldStake = uint64(response.Staked)

	service.vm.ctx.Lock.Lock()

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
		[]*secp256k1.PrivateKey{keys[0]},
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

	service.vm.ctx.Lock.Unlock()

	// Make sure the delegator has the right stake (old stake + stakeAmount)
	require.NoError(service.GetStake(nil, &args, &response))
	require.Equal(oldStake+stakeAmount, uint64(response.Staked))
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
	require.Equal(stakeAmount+oldStake, outputs[0].Out.Amount()+outputs[1].Out.Amount()+outputs[2].Out.Amount())
}

func TestGetCurrentValidators(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	defaultAddress(t, service)
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	genesis, _ := defaultGenesis(t)

	// Call getValidators
	args := GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	response := GetCurrentValidatorsReply{}

	require.NoError(service.GetCurrentValidators(nil, &args, &response))
	require.Len(response.Validators, len(genesis.Validators))

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

	service.vm.ctx.Lock.Lock()

	delTx, err := service.vm.txBuilder.NewAddDelegatorTx(
		stakeAmount,
		delegatorStartTime,
		delegatorEndTime,
		validatorNodeID,
		ids.GenerateTestShortID(),
		[]*secp256k1.PrivateKey{keys[0]},
		keys[0].PublicKey().Address(), // change addr
	)
	require.NoError(err)

	staker, err := state.NewCurrentStaker(
		delTx.ID(),
		delTx.Unsigned.(*txs.AddDelegatorTx),
		0,
	)
	require.NoError(err)

	service.vm.state.PutCurrentDelegator(staker)
	service.vm.state.AddTx(delTx, status.Committed)
	require.NoError(service.vm.state.Commit())

	service.vm.ctx.Lock.Unlock()

	// Call getCurrentValidators
	args = GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	require.NoError(service.GetCurrentValidators(nil, &args, &response))
	require.Len(response.Validators, len(genesis.Validators))

	// Make sure the delegator is there
	found := false
	for i := 0; i < len(response.Validators) && !found; i++ {
		vdr := response.Validators[i].(pchainapi.PermissionlessValidator)
		if vdr.NodeID != validatorNodeID {
			continue
		}
		found = true

		require.Nil(vdr.Delegators)

		innerArgs := GetCurrentValidatorsArgs{
			SubnetID: constants.PrimaryNetworkID,
			NodeIDs:  []ids.NodeID{vdr.NodeID},
		}
		innerResponse := GetCurrentValidatorsReply{}
		require.NoError(service.GetCurrentValidators(nil, &innerArgs, &innerResponse))
		require.Len(innerResponse.Validators, 1)

		innerVdr := innerResponse.Validators[0].(pchainapi.PermissionlessValidator)
		require.Equal(vdr.NodeID, innerVdr.NodeID)

		require.NotNil(innerVdr.Delegators)
		require.Len(*innerVdr.Delegators, 1)
		delegator := (*innerVdr.Delegators)[0]
		require.Equal(delegator.NodeID, innerVdr.NodeID)
		require.Equal(uint64(delegator.StartTime), delegatorStartTime)
		require.Equal(uint64(delegator.EndTime), delegatorEndTime)
		require.Equal(uint64(delegator.Weight), stakeAmount)
	}
	require.True(found)

	service.vm.ctx.Lock.Lock()

	// Reward the delegator
	tx, err := service.vm.txBuilder.NewRewardValidatorTx(delTx.ID())
	require.NoError(err)
	service.vm.state.AddTx(tx, status.Committed)
	service.vm.state.DeleteCurrentDelegator(staker)
	require.NoError(service.vm.state.SetDelegateeReward(staker.SubnetID, staker.NodeID, 100000))
	require.NoError(service.vm.state.Commit())

	service.vm.ctx.Lock.Unlock()

	// Call getValidators
	response = GetCurrentValidatorsReply{}
	require.NoError(service.GetCurrentValidators(nil, &args, &response))
	require.Len(response.Validators, len(genesis.Validators))

	for _, vdr := range response.Validators {
		castVdr := vdr.(pchainapi.PermissionlessValidator)
		if castVdr.NodeID != validatorNodeID {
			continue
		}
		require.Equal(uint64(100000), uint64(*castVdr.AccruedDelegateeReward))
	}
}

func TestGetTimestamp(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)
	defer func() {
		service.vm.ctx.Lock.Lock()
		require.NoError(service.vm.Shutdown(context.Background()))
		service.vm.ctx.Lock.Unlock()
	}()

	reply := GetTimestampReply{}
	require.NoError(service.GetTimestamp(nil, nil, &reply))

	service.vm.ctx.Lock.Lock()

	require.Equal(service.vm.state.GetTimestamp(), reply.Timestamp)

	newTimestamp := reply.Timestamp.Add(time.Second)
	service.vm.state.SetTimestamp(newTimestamp)

	service.vm.ctx.Lock.Unlock()

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
			defer func() {
				service.vm.ctx.Lock.Lock()
				require.NoError(service.vm.Shutdown(context.Background()))
				service.vm.ctx.Lock.Unlock()
			}()

			service.vm.Config.CreateAssetTxFee = 100 * defaultTxFee

			// Make a block an accept it, then check we can get it.
			tx, err := service.vm.txBuilder.NewCreateChainTx( // Test GetTx works for standard blocks
				testSubnet1.ID(),
				[]byte{},
				constants.AVMID,
				[]ids.ID{},
				"chain name",
				[]*secp256k1.PrivateKey{testSubnet1ControlKeys[0], testSubnet1ControlKeys[1]},
				keys[0].PublicKey().Address(), // change addr
			)
			require.NoError(err)

			preferred, err := service.vm.Builder.Preferred()
			require.NoError(err)

			statelessBlock, err := block.NewBanffStandardBlock(
				preferred.Timestamp(),
				preferred.ID(),
				preferred.Height()+1,
				[]*txs.Tx{tx},
			)
			require.NoError(err)

			blk := service.vm.manager.NewBlock(statelessBlock)

			require.NoError(blk.Verify(context.Background()))
			require.NoError(blk.Accept(context.Background()))

			service.vm.ctx.Lock.Unlock()

			args := api.GetBlockArgs{
				BlockID:  blk.ID(),
				Encoding: test.encoding,
			}
			response := api.GetBlockResponse{}
			require.NoError(service.GetBlock(nil, &args, &response))

			switch {
			case test.encoding == formatting.JSON:
				statelessBlock.InitCtx(service.vm.ctx)
				expectedBlockJSON, err := stdjson.Marshal(statelessBlock)
				require.NoError(err)
				require.Equal(expectedBlockJSON, []byte(response.Block))
			default:
				var blockStr string
				require.NoError(stdjson.Unmarshal(response.Block, &blockStr))
				responseBlockBytes, err := formatting.Decode(response.Encoding, blockStr)
				require.NoError(err)
				require.Equal(blk.Bytes(), responseBlockBytes)
			}

			require.Equal(test.encoding, response.Encoding)
		})
	}
}

func TestGetValidatorsAtReplyMarshalling(t *testing.T) {
	require := require.New(t)

	reply := &GetValidatorsAtReply{
		Validators: make(map[ids.NodeID]*validators.GetValidatorOutput),
	}

	{
		reply.Validators[ids.EmptyNodeID] = &validators.GetValidatorOutput{
			NodeID:    ids.EmptyNodeID,
			PublicKey: nil,
			Weight:    0,
		}
	}
	{
		nodeID := ids.GenerateTestNodeID()
		sk, err := bls.NewSecretKey()
		require.NoError(err)
		reply.Validators[nodeID] = &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: bls.PublicFromSecretKey(sk),
			Weight:    math.MaxUint64,
		}
	}

	replyJSON, err := reply.MarshalJSON()
	require.NoError(err)

	var parsedReply GetValidatorsAtReply
	require.NoError(parsedReply.UnmarshalJSON(replyJSON))
	require.Equal(reply, &parsedReply)
}

func TestServiceGetBlockByHeight(t *testing.T) {
	ctrl := gomock.NewController(t)

	blockID := ids.GenerateTestID()
	blockHeight := uint64(1337)

	type test struct {
		name                        string
		serviceAndExpectedBlockFunc func(t *testing.T, ctrl *gomock.Controller) (*Service, interface{})
		encoding                    formatting.Encoding
		expectedErr                 error
	}

	tests := []test{
		{
			name: "block height not found",
			serviceAndExpectedBlockFunc: func(_ *testing.T, ctrl *gomock.Controller) (*Service, interface{}) {
				state := state.NewMockState(ctrl)
				state.EXPECT().GetBlockIDAtHeight(blockHeight).Return(ids.Empty, database.ErrNotFound)

				manager := blockexecutor.NewMockManager(ctrl)
				return &Service{
					vm: &VM{
						state:   state,
						manager: manager,
						ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}, nil
			},
			encoding:    formatting.Hex,
			expectedErr: database.ErrNotFound,
		},
		{
			name: "block not found",
			serviceAndExpectedBlockFunc: func(_ *testing.T, ctrl *gomock.Controller) (*Service, interface{}) {
				state := state.NewMockState(ctrl)
				state.EXPECT().GetBlockIDAtHeight(blockHeight).Return(blockID, nil)

				manager := blockexecutor.NewMockManager(ctrl)
				manager.EXPECT().GetStatelessBlock(blockID).Return(nil, database.ErrNotFound)
				return &Service{
					vm: &VM{
						state:   state,
						manager: manager,
						ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}, nil
			},
			encoding:    formatting.Hex,
			expectedErr: database.ErrNotFound,
		},
		{
			name: "JSON format",
			serviceAndExpectedBlockFunc: func(_ *testing.T, ctrl *gomock.Controller) (*Service, interface{}) {
				block := block.NewMockBlock(ctrl)
				block.EXPECT().InitCtx(gomock.Any())

				state := state.NewMockState(ctrl)
				state.EXPECT().GetBlockIDAtHeight(blockHeight).Return(blockID, nil)

				manager := blockexecutor.NewMockManager(ctrl)
				manager.EXPECT().GetStatelessBlock(blockID).Return(block, nil)
				return &Service{
					vm: &VM{
						state:   state,
						manager: manager,
						ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}, block
			},
			encoding:    formatting.JSON,
			expectedErr: nil,
		},
		{
			name: "hex format",
			serviceAndExpectedBlockFunc: func(t *testing.T, ctrl *gomock.Controller) (*Service, interface{}) {
				block := block.NewMockBlock(ctrl)
				blockBytes := []byte("hi mom")
				block.EXPECT().Bytes().Return(blockBytes)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetBlockIDAtHeight(blockHeight).Return(blockID, nil)

				expected, err := formatting.Encode(formatting.Hex, blockBytes)
				require.NoError(t, err)

				manager := blockexecutor.NewMockManager(ctrl)
				manager.EXPECT().GetStatelessBlock(blockID).Return(block, nil)
				return &Service{
					vm: &VM{
						state:   state,
						manager: manager,
						ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}, expected
			},
			encoding:    formatting.Hex,
			expectedErr: nil,
		},
		{
			name: "hexc format",
			serviceAndExpectedBlockFunc: func(t *testing.T, ctrl *gomock.Controller) (*Service, interface{}) {
				block := block.NewMockBlock(ctrl)
				blockBytes := []byte("hi mom")
				block.EXPECT().Bytes().Return(blockBytes)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetBlockIDAtHeight(blockHeight).Return(blockID, nil)

				expected, err := formatting.Encode(formatting.HexC, blockBytes)
				require.NoError(t, err)

				manager := blockexecutor.NewMockManager(ctrl)
				manager.EXPECT().GetStatelessBlock(blockID).Return(block, nil)
				return &Service{
					vm: &VM{
						state:   state,
						manager: manager,
						ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}, expected
			},
			encoding:    formatting.HexC,
			expectedErr: nil,
		},
		{
			name: "hexnc format",
			serviceAndExpectedBlockFunc: func(t *testing.T, ctrl *gomock.Controller) (*Service, interface{}) {
				block := block.NewMockBlock(ctrl)
				blockBytes := []byte("hi mom")
				block.EXPECT().Bytes().Return(blockBytes)

				state := state.NewMockState(ctrl)
				state.EXPECT().GetBlockIDAtHeight(blockHeight).Return(blockID, nil)

				expected, err := formatting.Encode(formatting.HexNC, blockBytes)
				require.NoError(t, err)

				manager := blockexecutor.NewMockManager(ctrl)
				manager.EXPECT().GetStatelessBlock(blockID).Return(block, nil)
				return &Service{
					vm: &VM{
						state:   state,
						manager: manager,
						ctx: &snow.Context{
							Log: logging.NoLog{},
						},
					},
				}, expected
			},
			encoding:    formatting.HexNC,
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			service, expected := tt.serviceAndExpectedBlockFunc(t, ctrl)

			args := &api.GetBlockByHeightArgs{
				Height:   json.Uint64(blockHeight),
				Encoding: tt.encoding,
			}
			reply := &api.GetBlockResponse{}
			err := service.GetBlockByHeight(nil, args, reply)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.encoding, reply.Encoding)

			expectedJSON, err := stdjson.Marshal(expected)
			require.NoError(err)

			require.Equal(stdjson.RawMessage(expectedJSON), reply.Block)
		})
	}
}

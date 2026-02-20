// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/cache/lru"
	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/formatting/address"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/platformvm/block"
	"github.com/ava-labs/avalanchego/vms/platformvm/block/executor/executormock"
	"github.com/ava-labs/avalanchego/vms/platformvm/genesis/genesistest"
	"github.com/ava-labs/avalanchego/vms/platformvm/signer"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/validators/fee"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/message"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/ava-labs/avalanchego/vms/types"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary/common"

	avajson "github.com/ava-labs/avalanchego/utils/json"
	pchainapi "github.com/ava-labs/avalanchego/vms/platformvm/api"
	blockbuilder "github.com/ava-labs/avalanchego/vms/platformvm/block/builder"
	blockexecutor "github.com/ava-labs/avalanchego/vms/platformvm/block/executor"
	txexecutor "github.com/ava-labs/avalanchego/vms/platformvm/txs/executor"
)

var encodings = []formatting.Encoding{
	formatting.JSON, formatting.Hex,
}

func defaultService(t *testing.T) (*Service, *mutableSharedMemory) {
	vm, _, mutableSharedMemory := defaultVM(t, upgradetest.Latest)
	return &Service{
		vm:                    vm,
		addrManager:           avax.NewAddressManager(vm.ctx),
		stakerAttributesCache: lru.NewCache[ids.ID, *stakerAttributes](stakerAttributesCacheSize),
	}, mutableSharedMemory
}

func TestGetProposedHeight(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

	reply := api.GetHeightResponse{}
	require.NoError(service.GetProposedHeight(&http.Request{}, nil, &reply))

	minHeight, err := service.vm.GetMinimumHeight(t.Context())
	require.NoError(err)
	require.Equal(minHeight, uint64(reply.Height))

	service.vm.ctx.Lock.Lock()

	// issue any transaction to put into the new block
	subnetID := testSubnet1.ID()
	wallet := newWallet(t, service.vm, walletConfig{
		subnetIDs: []ids.ID{subnetID},
	})

	tx, err := wallet.IssueCreateChainTx(
		subnetID,
		[]byte{},
		constants.AVMID,
		[]ids.ID{},
		"chain name",
		common.WithMemo([]byte{}),
	)
	require.NoError(err)

	service.vm.ctx.Lock.Unlock()

	// Get the last accepted block which should be genesis
	genesisBlockID := service.vm.manager.LastAccepted()

	require.NoError(service.vm.Network.IssueTxFromRPC(tx))
	service.vm.ctx.Lock.Lock()

	block, err := service.vm.BuildBlock(t.Context())
	require.NoError(err)

	blk := block.(*blockexecutor.Block)
	require.NoError(blk.Verify(t.Context()))

	require.NoError(blk.Accept(t.Context()))

	service.vm.ctx.Lock.Unlock()

	latestBlockID := service.vm.manager.LastAccepted()
	latestBlock, err := service.vm.manager.GetBlock(latestBlockID)
	require.NoError(err)
	require.NotEqual(genesisBlockID, latestBlockID)

	// Confirm that the proposed height hasn't changed with the new block being accepted.
	require.NoError(service.GetProposedHeight(&http.Request{}, nil, &reply))
	require.Equal(minHeight, uint64(reply.Height))

	// Set the clock to beyond the proposer VM height of the most recent accepted block
	service.vm.clock.Set(latestBlock.Timestamp().Add(31 * time.Second))

	// Confirm that the proposed height has updated to the latest block height
	require.NoError(service.GetProposedHeight(&http.Request{}, nil, &reply))
	require.Equal(latestBlock.Height(), uint64(reply.Height))
}

// Test issuing a tx and accepted
func TestGetTxStatus(t *testing.T) {
	require := require.New(t)
	service, mutableSharedMemory := defaultService(t)
	service.vm.ctx.Lock.Lock()

	recipientKey, err := secp256k1.NewPrivateKey()
	require.NoError(err)

	m := atomic.NewMemory(prefixdb.New([]byte{}, service.vm.db))

	sm := m.NewSharedMemory(service.vm.ctx.ChainID)
	peerSharedMemory := m.NewSharedMemory(service.vm.ctx.XChainID)

	randSrc := rand.NewSource(0)

	utxo := &avax.UTXO{
		UTXOID: avax.UTXOID{
			TxID:        ids.GenerateTestID(),
			OutputIndex: uint32(randSrc.Int63()),
		},
		Asset: avax.Asset{ID: service.vm.ctx.AVAXAssetID},
		Out: &secp256k1fx.TransferOutput{
			Amt: 1234567,
			OutputOwners: secp256k1fx.OutputOwners{
				Locktime:  0,
				Addrs:     []ids.ShortID{recipientKey.Address()},
				Threshold: 1,
			},
		},
	}
	utxoBytes, err := txs.Codec.Marshal(txs.CodecVersion, utxo)
	require.NoError(err)

	inputID := utxo.InputID()
	require.NoError(peerSharedMemory.Apply(map[ids.ID]*atomic.Requests{
		service.vm.ctx.ChainID: {
			PutRequests: []*atomic.Element{
				{
					Key:   inputID[:],
					Value: utxoBytes,
					Traits: [][]byte{
						recipientKey.Address().Bytes(),
					},
				},
			},
		},
	}))

	mutableSharedMemory.SharedMemory = sm

	wallet := newWallet(t, service.vm, walletConfig{
		keys: []*secp256k1.PrivateKey{recipientKey},
	})
	tx, err := wallet.IssueImportTx(
		service.vm.ctx.XChainID,
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.ShortEmpty},
		},
	)
	require.NoError(err)

	service.vm.ctx.Lock.Unlock()

	var (
		arg  = &GetTxStatusArgs{TxID: tx.ID()}
		resp GetTxStatusResponse
	)
	require.NoError(service.GetTxStatus(nil, arg, &resp))
	require.Equal(status.Unknown, resp.Status)
	require.Empty(resp.Reason)

	// put the chain in existing chain list
	require.NoError(service.vm.Network.IssueTxFromRPC(tx))
	service.vm.ctx.Lock.Lock()

	block, err := service.vm.BuildBlock(t.Context())
	require.NoError(err)

	blk := block.(*blockexecutor.Block)
	require.NoError(blk.Verify(t.Context()))

	require.NoError(blk.Accept(t.Context()))

	service.vm.ctx.Lock.Unlock()

	resp = GetTxStatusResponse{} // reset
	require.NoError(service.GetTxStatus(nil, arg, &resp))
	require.Equal(status.Committed, resp.Status)
	require.Empty(resp.Reason)
}

// Test issuing and then retrieving a transaction
func TestGetTx(t *testing.T) {
	type test struct {
		description string
		createTx    func(t testing.TB, s *Service) *txs.Tx
	}

	tests := []test{
		{
			"standard block",
			func(t testing.TB, s *Service) *txs.Tx {
				subnetID := testSubnet1.ID()
				wallet := newWallet(t, s.vm, walletConfig{
					subnetIDs: []ids.ID{subnetID},
				})

				tx, err := wallet.IssueCreateChainTx(
					subnetID,
					[]byte{},
					constants.AVMID,
					[]ids.ID{},
					"chain name",
					common.WithMemo([]byte{}),
				)
				require.NoError(t, err)
				return tx
			},
		},
		{
			"proposal block",
			func(t testing.TB, s *Service) *txs.Tx {
				wallet := newWallet(t, s.vm, walletConfig{})

				sk, err := localsigner.New()
				require.NoError(t, err)
				pop, err := signer.NewProofOfPossession(sk)
				require.NoError(t, err)

				rewardsOwner := &secp256k1fx.OutputOwners{
					Threshold: 1,
					Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
				}
				tx, err := wallet.IssueAddPermissionlessValidatorTx(
					&txs.SubnetValidator{
						Validator: txs.Validator{
							NodeID: ids.GenerateTestNodeID(),
							Start:  uint64(s.vm.clock.Time().Add(txexecutor.SyncBound).Unix()),
							End:    uint64(s.vm.clock.Time().Add(txexecutor.SyncBound).Add(defaultMinStakingDuration).Unix()),
							Wght:   s.vm.MinValidatorStake,
						},
						Subnet: constants.PrimaryNetworkID,
					},
					pop,
					s.vm.ctx.AVAXAssetID,
					rewardsOwner,
					rewardsOwner,
					0,
					common.WithMemo([]byte{}),
				)
				require.NoError(t, err)
				return tx
			},
		},
		{
			"atomic block",
			func(t testing.TB, s *Service) *txs.Tx {
				wallet := newWallet(t, s.vm, walletConfig{})

				tx, err := wallet.IssueExportTx(
					s.vm.ctx.XChainID,
					[]*avax.TransferableOutput{{
						Asset: avax.Asset{ID: s.vm.ctx.AVAXAssetID},
						Out: &secp256k1fx.TransferOutput{
							Amt: 100,
							OutputOwners: secp256k1fx.OutputOwners{
								Locktime:  0,
								Threshold: 1,
								Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
							},
						},
					}},
					common.WithMemo([]byte{}),
				)
				require.NoError(t, err)
				return tx
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

				service.vm.ctx.Lock.Lock()
				tx := test.createTx(t, service)
				service.vm.ctx.Lock.Unlock()

				arg := &api.GetTxArgs{
					TxID:     tx.ID(),
					Encoding: encoding,
				}
				var response api.GetTxReply
				err := service.GetTx(nil, arg, &response)
				require.ErrorIs(err, database.ErrNotFound) // We haven't issued the tx yet

				require.NoError(service.vm.Network.IssueTxFromRPC(tx))
				service.vm.ctx.Lock.Lock()

				blk, err := service.vm.BuildBlock(t.Context())
				require.NoError(err)

				require.NoError(blk.Verify(t.Context()))

				require.NoError(blk.Accept(t.Context()))

				if blk, ok := blk.(snowman.OracleBlock); ok { // For proposal blocks, commit them
					options, err := blk.Options(t.Context())
					if !errors.Is(err, snowman.ErrNotOracle) {
						require.NoError(err)

						commit := options[0].(*blockexecutor.Block)
						require.IsType(&block.BanffCommitBlock{}, commit.Block)
						require.NoError(commit.Verify(t.Context()))
						require.NoError(commit.Accept(t.Context()))
					}
				}

				service.vm.ctx.Lock.Unlock()

				require.NoError(service.GetTx(nil, arg, &response))

				switch encoding {
				case formatting.Hex:
					// we're always guaranteed a string for hex encodings.
					var txStr string
					require.NoError(json.Unmarshal(response.Tx, &txStr))
					responseTxBytes, err := formatting.Decode(response.Encoding, txStr)
					require.NoError(err)
					require.Equal(tx.Bytes(), responseTxBytes)

				case formatting.JSON:
					tx.Unsigned.InitCtx(service.vm.ctx)
					expectedTxJSON, err := json.Marshal(tx)
					require.NoError(err)
					require.JSONEq(string(expectedTxJSON), string(response.Tx))
				}
			})
		}
	}
}

func TestGetBalance(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

	feeCalculator := state.PickFeeCalculator(&service.vm.Internal, service.vm.state)
	createSubnetFee, err := feeCalculator.CalculateFee(testSubnet1.Unsigned)
	require.NoError(err)

	// Ensure GetStake is correct for each of the genesis validators
	genesis := genesistest.New(t, genesistest.Config{})
	for idx, utxo := range genesis.UTXOs {
		out := utxo.Out.(*secp256k1fx.TransferOutput)
		require.Len(out.Addrs, 1)

		addr := out.Addrs[0]
		addrStr, err := address.Format("P", constants.UnitTestHRP, addr.Bytes())
		require.NoError(err)

		request := GetBalanceRequest{
			Addresses: []string{
				addrStr,
			},
		}
		reply := GetBalanceResponse{}

		require.NoError(service.GetBalance(nil, &request, &reply))
		balance := genesistest.DefaultInitialBalance
		if idx == 0 {
			// we use the first key to fund a subnet creation in [defaultGenesis].
			// As such we need to account for the subnet creation fee
			balance = genesistest.DefaultInitialBalance - createSubnetFee
		}
		require.Equal(avajson.Uint64(balance), reply.Balance)
		require.Equal(avajson.Uint64(balance), reply.Unlocked)
		require.Equal(avajson.Uint64(0), reply.LockedStakeable)
		require.Equal(avajson.Uint64(0), reply.LockedNotStakeable)
	}
}

func TestGetStake(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

	// Ensure GetStake is correct for each of the genesis validators
	genesis := genesistest.New(t, genesistest.Config{})
	addrsStrs := []string{}
	for _, validatorTx := range genesis.Validators {
		validator := validatorTx.Unsigned.(*txs.AddValidatorTx)
		require.Len(validator.StakeOuts, 1)
		stakeOut := validator.StakeOuts[0].Out.(*secp256k1fx.TransferOutput)
		require.Len(stakeOut.Addrs, 1)
		addr := stakeOut.Addrs[0]

		addrStr, err := address.Format("P", constants.UnitTestHRP, addr.Bytes())
		require.NoError(err)

		addrsStrs = append(addrsStrs, addrStr)

		args := GetStakeArgs{
			JSONAddresses: api.JSONAddresses{
				Addresses: []string{addrStr},
			},
			Encoding: formatting.Hex,
		}
		response := GetStakeReply{}
		require.NoError(service.GetStake(nil, &args, &response))
		require.Equal(genesistest.DefaultValidatorWeight, uint64(response.Staked))
		require.Len(response.Outputs, 1)

		// Unmarshal into an output
		outputBytes, err := formatting.Decode(args.Encoding, response.Outputs[0])
		require.NoError(err)

		var output avax.TransferableOutput
		_, err = txs.Codec.Unmarshal(outputBytes, &output)
		require.NoError(err)

		require.Equal(
			avax.TransferableOutput{
				Asset: avax.Asset{
					ID: service.vm.ctx.AVAXAssetID,
				},
				Out: &secp256k1fx.TransferOutput{
					Amt: genesistest.DefaultValidatorWeight,
					OutputOwners: secp256k1fx.OutputOwners{
						Threshold: 1,
						Addrs: []ids.ShortID{
							addr,
						},
					},
				},
			},
			output,
		)
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
	require.Equal(len(genesis.Validators)*int(genesistest.DefaultValidatorWeight), int(response.Staked))
	require.Len(response.Outputs, len(genesis.Validators))

	for _, outputStr := range response.Outputs {
		outputBytes, err := formatting.Decode(args.Encoding, outputStr)
		require.NoError(err)

		var output avax.TransferableOutput
		_, err = txs.Codec.Unmarshal(outputBytes, &output)
		require.NoError(err)

		out := output.Out.(*secp256k1fx.TransferOutput)
		require.Equal(genesistest.DefaultValidatorWeight, out.Amt)
		require.Equal(uint32(1), out.Threshold)
		require.Zero(out.Locktime)
		require.Len(out.Addrs, 1)
	}

	oldStake := genesistest.DefaultValidatorWeight

	service.vm.ctx.Lock.Lock()

	wallet := newWallet(t, service.vm, walletConfig{})

	// Add a delegator
	stakeAmount := service.vm.MinDelegatorStake + 12345
	delegatorNodeID := genesistest.DefaultNodeIDs[0]
	delegatorEndTime := genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}
	withChangeOwner := common.WithChangeOwner(&secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
	})
	tx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: delegatorNodeID,
			Start:  genesistest.DefaultValidatorStartTimeUnix,
			End:    uint64(delegatorEndTime.Unix()),
			Wght:   stakeAmount,
		},
		rewardsOwner,
		withChangeOwner,
	)
	require.NoError(err)

	addDelTx := tx.Unsigned.(*txs.AddDelegatorTx)
	staker, err := state.NewCurrentStaker(
		tx.ID(),
		addDelTx,
		genesistest.DefaultValidatorStartTime,
		0,
	)
	require.NoError(err)

	service.vm.state.PutCurrentDelegator(staker)
	service.vm.state.AddTx(tx, status.Committed)
	require.NoError(service.vm.state.Commit())

	service.vm.ctx.Lock.Unlock()

	// Make sure the delegator addr has the right stake (old stake + stakeAmount)
	addr, _ := service.addrManager.FormatLocalAddress(genesistest.DefaultFundedKeys[0].Address())
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
	pendingStakerEndTime := uint64(genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration).Unix())
	tx, err = wallet.IssueAddValidatorTx(
		&txs.Validator{
			NodeID: pendingStakerNodeID,
			Start:  uint64(genesistest.DefaultValidatorStartTime.Unix()),
			End:    pendingStakerEndTime,
			Wght:   stakeAmount,
		},
		rewardsOwner,
		0,
		withChangeOwner,
	)
	require.NoError(err)

	staker, err = state.NewPendingStaker(
		tx.ID(),
		tx.Unsigned.(*txs.AddValidatorTx),
	)
	require.NoError(err)

	require.NoError(service.vm.state.PutPendingValidator(staker))
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

	genesis := genesistest.New(t, genesistest.Config{})

	// Call getValidators
	args := GetCurrentValidatorsArgs{SubnetID: constants.PrimaryNetworkID}
	response := GetCurrentValidatorsReply{}

	// Connect to nodes other than the last node in genesis.Validators, which is the node being tested.
	connectedIDs := set.NewSet[ids.NodeID](len(genesis.Validators) - 1)
	for _, validatorTx := range genesis.Validators[:len(genesis.Validators)-1] {
		validator := validatorTx.Unsigned.(*txs.AddValidatorTx)
		connectedIDs.Add(validator.NodeID())
		require.NoError(service.vm.Connected(t.Context(), validator.NodeID(), version.Current))
	}

	require.NoError(service.GetCurrentValidators(nil, &args, &response))
	require.Len(response.Validators, len(genesis.Validators))

	for _, validatorTx := range genesis.Validators {
		validator := validatorTx.Unsigned.(*txs.AddValidatorTx)
		nodeID := validator.NodeID()

		found := false
		for i := 0; i < len(response.Validators); i++ {
			gotVdr := response.Validators[i].(pchainapi.PermissionlessValidator)
			if gotVdr.NodeID != nodeID {
				continue
			}

			require.Equal(validator.EndTime().Unix(), int64(gotVdr.EndTime))
			require.Equal(validator.StartTime().Unix(), int64(gotVdr.StartTime))
			require.Equal(connectedIDs.Contains(validator.NodeID()), *gotVdr.Connected)
			require.Equal(float32(avajson.Float32(100)), float32(*gotVdr.Uptime))
			found = true
			break
		}
		require.True(found, "expected validators to contain %s but didn't", nodeID)
	}

	// Add a delegator
	stakeAmount := service.vm.MinDelegatorStake + 12345
	validatorNodeID := genesistest.DefaultNodeIDs[1]
	delegatorEndTime := genesistest.DefaultValidatorStartTime.Add(defaultMinStakingDuration)

	service.vm.ctx.Lock.Lock()

	wallet := newWallet(t, service.vm, walletConfig{})
	delTx, err := wallet.IssueAddDelegatorTx(
		&txs.Validator{
			NodeID: validatorNodeID,
			Start:  genesistest.DefaultValidatorStartTimeUnix,
			End:    uint64(delegatorEndTime.Unix()),
			Wght:   stakeAmount,
		},
		&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
		},
		common.WithChangeOwner(&secp256k1fx.OutputOwners{
			Threshold: 1,
			Addrs:     []ids.ShortID{genesistest.DefaultFundedKeys[0].Address()},
		}),
	)
	require.NoError(err)

	addDelTx := delTx.Unsigned.(*txs.AddDelegatorTx)
	staker, err := state.NewCurrentStaker(
		delTx.ID(),
		addDelTx,
		genesistest.DefaultValidatorStartTime,
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
		require.Equal(uint64(delegator.StartTime), genesistest.DefaultValidatorStartTimeUnix)
		require.Equal(int64(delegator.EndTime), delegatorEndTime.Unix())
		require.Equal(uint64(delegator.Weight), stakeAmount)
	}
	require.True(found)

	service.vm.ctx.Lock.Lock()

	// Reward the delegator
	tx, err := blockbuilder.NewRewardValidatorTx(service.vm.ctx, delTx.ID())
	require.NoError(err)
	service.vm.state.AddTx(tx, status.Committed)
	service.vm.state.DeleteCurrentDelegator(staker)
	require.NoError(service.vm.state.SetValidatorMutables(staker.NodeID, staker.SubnetID, state.ValidatorMutables{DelegateeReward: 100000}))
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

func TestGetValidatorsAt(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

	genesis := genesistest.New(t, genesistest.Config{})

	args := GetValidatorsAtArgs{}
	response := GetValidatorsAtReply{}

	service.vm.ctx.Lock.Lock()
	lastAccepted := service.vm.manager.LastAccepted()
	lastAcceptedBlk, err := service.vm.manager.GetBlock(lastAccepted)
	require.NoError(err)

	service.vm.ctx.Lock.Unlock()

	// Confirm that it returns the genesis validators given the latest height
	args.Height = pchainapi.Height(lastAcceptedBlk.Height())
	require.NoError(service.GetValidatorsAt(&http.Request{}, &args, &response))
	require.Len(response.Validators, len(genesis.Validators))

	service.vm.ctx.Lock.Lock()

	wallet := newWallet(t, service.vm, walletConfig{})
	rewardsOwner := &secp256k1fx.OutputOwners{
		Threshold: 1,
		Addrs:     []ids.ShortID{ids.GenerateTestShortID()},
	}

	sk, err := localsigner.New()
	require.NoError(err)
	pop, err := signer.NewProofOfPossession(sk)
	require.NoError(err)

	tx, err := wallet.IssueAddPermissionlessValidatorTx(
		&txs.SubnetValidator{
			Validator: txs.Validator{
				NodeID: ids.GenerateTestNodeID(),
				Start:  uint64(service.vm.clock.Time().Add(txexecutor.SyncBound).Unix()),
				End:    uint64(service.vm.clock.Time().Add(txexecutor.SyncBound).Add(defaultMinStakingDuration).Unix()),
				Wght:   service.vm.MinValidatorStake,
			},
			Subnet: constants.PrimaryNetworkID,
		},
		pop,
		service.vm.ctx.AVAXAssetID,
		rewardsOwner,
		rewardsOwner,
		0,
		common.WithMemo([]byte{}),
	)

	require.NoError(err)

	service.vm.ctx.Lock.Unlock()
	require.NoError(service.vm.Network.IssueTxFromRPC(tx))
	service.vm.ctx.Lock.Lock()

	block, err := service.vm.BuildBlock(t.Context())
	require.NoError(err)

	blk := block.(*blockexecutor.Block)
	require.NoError(blk.Verify(t.Context()))

	require.NoError(blk.Accept(t.Context()))
	service.vm.ctx.Lock.Unlock()

	newLastAccepted := service.vm.manager.LastAccepted()
	newLastAcceptedBlk, err := service.vm.manager.GetBlock(newLastAccepted)
	require.NoError(err)
	require.NotEqual(newLastAccepted, lastAccepted)

	// Confirm that it returns the genesis validators + the new validator given the latest height
	args.Height = pchainapi.Height(newLastAcceptedBlk.Height())
	require.NoError(service.GetValidatorsAt(&http.Request{}, &args, &response))
	require.Len(response.Validators, len(genesis.Validators)+1)

	// Confirm that [IsProposed] works. The proposed height should be the genesis height
	args.Height = pchainapi.Height(pchainapi.ProposedHeight)
	require.NoError(service.GetValidatorsAt(&http.Request{}, &args, &response))
	require.Len(response.Validators, len(genesis.Validators))

	service.vm.ctx.Lock.Lock()

	// set clock beyond the [validators.recentlyAcceptedWindowTTL] to bump the
	// proposerVM height
	service.vm.clock.Set(newLastAcceptedBlk.Timestamp().Add(40 * time.Second))
	service.vm.ctx.Lock.Unlock()

	// Resending the same request with [Height] set to [platformapi.ProposedHeight] should now
	// include the new validator
	require.NoError(service.GetValidatorsAt(&http.Request{}, &args, &response))
	require.Len(response.Validators, len(genesis.Validators)+1)
}

func TestGetValidatorsAtArgsMarshalling(t *testing.T) {
	subnetID, err := ids.FromString("u3Jjpzzj95827jdENvR1uc76f4zvvVQjGshbVWaSr2Ce5WV1H")
	require.NoError(t, err)

	tests := []struct {
		name string
		args GetValidatorsAtArgs
		json string
	}{
		{
			name: "specific height",
			args: GetValidatorsAtArgs{
				Height:   pchainapi.Height(12345),
				SubnetID: subnetID,
			},
			json: `{"height":"12345","subnetID":"u3Jjpzzj95827jdENvR1uc76f4zvvVQjGshbVWaSr2Ce5WV1H"}`,
		},
		{
			name: "proposed height",
			args: GetValidatorsAtArgs{
				Height:   pchainapi.ProposedHeight,
				SubnetID: subnetID,
			},
			json: `{"height":"proposed","subnetID":"u3Jjpzzj95827jdENvR1uc76f4zvvVQjGshbVWaSr2Ce5WV1H"}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			// Test that marshalling produces the expected JSON
			argsJSON, err := json.Marshal(test.args)
			require.NoError(err)
			require.JSONEq(test.json, string(argsJSON))

			// Test that unmarshalling produces the expected args
			var parsedArgs GetValidatorsAtArgs
			require.NoError(json.Unmarshal(argsJSON, &parsedArgs))
			require.Equal(test.args, parsedArgs)
		})
	}
}

func TestGetTimestamp(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

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

			subnetID := testSubnet1.ID()
			wallet := newWallet(t, service.vm, walletConfig{
				subnetIDs: []ids.ID{subnetID},
			})
			tx, err := wallet.IssueCreateChainTx(
				subnetID,
				[]byte{},
				constants.AVMID,
				[]ids.ID{},
				"chain name",
				common.WithMemo([]byte{}),
			)
			require.NoError(err)

			preferredID := service.vm.manager.Preferred()
			preferred, err := service.vm.manager.GetBlock(preferredID)
			require.NoError(err)

			statelessBlock, err := block.NewBanffStandardBlock(
				preferred.Timestamp(),
				preferred.ID(),
				preferred.Height()+1,
				[]*txs.Tx{tx},
			)
			require.NoError(err)

			blk := service.vm.manager.NewBlock(statelessBlock)

			require.NoError(blk.Verify(t.Context()))
			require.NoError(blk.Accept(t.Context()))

			service.vm.ctx.Lock.Unlock()

			args := api.GetBlockArgs{
				BlockID:  blk.ID(),
				Encoding: test.encoding,
			}
			response := api.GetBlockResponse{}
			require.NoError(service.GetBlock(nil, &args, &response))

			switch test.encoding {
			case formatting.JSON:
				statelessBlock.InitCtx(service.vm.ctx)
				expectedBlockJSON, err := json.Marshal(statelessBlock)
				require.NoError(err)
				require.JSONEq(string(expectedBlockJSON), string(response.Block))
			default:
				var blockStr string
				require.NoError(json.Unmarshal(response.Block, &blockStr))
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
		sk, err := localsigner.New()
		require.NoError(err)
		reply.Validators[nodeID] = &validators.GetValidatorOutput{
			NodeID:    nodeID,
			PublicKey: sk.PublicKey(),
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

				manager := executormock.NewManager(ctrl)
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

				manager := executormock.NewManager(ctrl)
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

				manager := executormock.NewManager(ctrl)
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

				manager := executormock.NewManager(ctrl)
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

				manager := executormock.NewManager(ctrl)
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

				manager := executormock.NewManager(ctrl)
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
				Height:   avajson.Uint64(blockHeight),
				Encoding: tt.encoding,
			}
			reply := &api.GetBlockResponse{}
			err := service.GetBlockByHeight(nil, args, reply)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.encoding, reply.Encoding)

			expectedJSON, err := json.Marshal(expected)
			require.NoError(err)

			require.JSONEq(string(expectedJSON), string(reply.Block))
		})
	}
}

func TestServiceGetSubnets(t *testing.T) {
	require := require.New(t)
	service, _ := defaultService(t)

	testSubnet1ID := testSubnet1.ID()

	var response GetSubnetsResponse
	require.NoError(service.GetSubnets(nil, &GetSubnetsArgs{}, &response))
	require.Equal([]APISubnet{
		{
			ID: testSubnet1ID,
			ControlKeys: []string{
				"P-testing1d6kkj0qh4wcmus3tk59npwt3rluc6en72ngurd",
				"P-testing17fpqs358de5lgu7a5ftpw2t8axf0pm33983krk",
				"P-testing1lnk637g0edwnqc2tn8tel39652fswa3xk4r65e",
			},
			Threshold: 2,
		},
		{
			ID:          constants.PrimaryNetworkID,
			ControlKeys: []string{},
			Threshold:   0,
		},
	}, response.Subnets)

	newOwnerIDStr := "P-testing1t73fa4p4dypa4s3kgufuvr6hmprjclw66mgqgm"
	newOwnerID, err := service.addrManager.ParseLocalAddress(newOwnerIDStr)
	require.NoError(err)

	service.vm.ctx.Lock.Lock()
	service.vm.state.SetSubnetOwner(testSubnet1ID, &secp256k1fx.OutputOwners{
		Addrs:     []ids.ShortID{newOwnerID},
		Threshold: 1,
	})
	service.vm.ctx.Lock.Unlock()

	require.NoError(service.GetSubnets(nil, &GetSubnetsArgs{}, &response))
	require.Equal([]APISubnet{
		{
			ID: testSubnet1ID,
			ControlKeys: []string{
				newOwnerIDStr,
			},
			Threshold: 1,
		},
		{
			ID:          constants.PrimaryNetworkID,
			ControlKeys: []string{},
			Threshold:   0,
		},
	}, response.Subnets)
}

func TestGetFeeConfig(t *testing.T) {
	require := require.New(t)

	service, _ := defaultService(t)

	var reply gas.Config
	require.NoError(service.GetFeeConfig(nil, nil, &reply))
	require.Equal(defaultDynamicFeeConfig, reply)
}

func FuzzGetFeeState(f *testing.F) {
	f.Fuzz(func(t *testing.T, capacity, excess uint64) {
		require := require.New(t)

		service, _ := defaultService(t)

		var (
			expectedState = gas.State{
				Capacity: gas.Gas(capacity),
				Excess:   gas.Gas(excess),
			}
			expectedTime  = time.Now()
			expectedReply = GetFeeStateReply{
				State: expectedState,
				Price: gas.CalculatePrice(
					defaultDynamicFeeConfig.MinPrice,
					expectedState.Excess,
					defaultDynamicFeeConfig.ExcessConversionConstant,
				),
				Time: expectedTime,
			}
		)

		service.vm.ctx.Lock.Lock()
		service.vm.state.SetFeeState(expectedState)
		service.vm.state.SetTimestamp(expectedTime)
		service.vm.ctx.Lock.Unlock()

		var reply GetFeeStateReply
		require.NoError(service.GetFeeState(nil, nil, &reply))
		require.Equal(expectedReply, reply)
	})
}

func TestGetCurrentValidatorsForL1(t *testing.T) {
	subnetID := ids.GenerateTestID()

	sk, err := localsigner.New()
	require.NoError(t, err)
	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	otherSK, err := localsigner.New()
	require.NoError(t, err)
	otherPK := otherSK.PublicKey()
	otherPKBytes := bls.PublicKeyToUncompressedBytes(otherPK)

	tests := []struct {
		name         string
		initial      []*state.Staker
		l1Validators []state.L1Validator
	}{
		{
			name: "empty_noop",
		},
		{
			name: "initial_stakers",
			initial: []*state.Staker{
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: pk,
					Weight:    1,
					StartTime: time.Unix(0, 0),
				},
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: otherPK,
					Weight:    1,
					StartTime: time.Unix(1, 0),
				},
			},
		},
		{
			name: "l1_validators",
			l1Validators: []state.L1Validator{
				{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID,
					NodeID:       ids.GenerateTestNodeID(),
					StartTime:    0,
					PublicKey:    pkBytes,
					Weight:       1,
				},
				{
					ValidationID: ids.GenerateTestID(),
					SubnetID:     subnetID,
					NodeID:       ids.GenerateTestNodeID(),
					PublicKey:    otherPKBytes,
					StartTime:    1,
					Weight:       1,
				},
			},
		},
		{
			name: "initial_stakers_l1_validators_mixed",
			initial: []*state.Staker{
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: pk,
					Weight:    123123,
					StartTime: time.Unix(0, 0),
				},
				{
					TxID:      ids.GenerateTestID(),
					SubnetID:  subnetID,
					NodeID:    ids.GenerateTestNodeID(),
					PublicKey: otherPK,
					Weight:    0,
					StartTime: time.Unix(2, 0),
				},
			},
			l1Validators: []state.L1Validator{
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID,
					NodeID:            ids.GenerateTestNodeID(),
					StartTime:         0,
					PublicKey:         pkBytes,
					Weight:            1,
					EndAccumulatedFee: 1,
					MinNonce:          2,
				},
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID,
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         pkBytes,
					StartTime:         2,
					Weight:            1,
					EndAccumulatedFee: 0,
				},
				{
					ValidationID:      ids.GenerateTestID(),
					SubnetID:          subnetID,
					NodeID:            ids.GenerateTestNodeID(),
					PublicKey:         otherPKBytes,
					StartTime:         3,
					Weight:            0,
					EndAccumulatedFee: 1,
				},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			service, _ := defaultService(t)
			service.vm.ctx.Lock.Lock()
			stakersByTxID := make(map[ids.ID]*state.Staker)
			for _, staker := range test.initial {
				primaryStaker := &state.Staker{
					TxID:      ids.GenerateTestID(),
					SubnetID:  constants.PrimaryNetworkID,
					NodeID:    staker.NodeID,
					PublicKey: staker.PublicKey,
					Weight:    5,
					// start primary network staker 1 second before the subnet staker
					StartTime: staker.StartTime.Add(-time.Second),
					Priority:  txs.PrimaryNetworkValidatorCurrentPriority,
				}
				require.NoError(service.vm.state.PutCurrentValidator(primaryStaker))
				staker.Priority = txs.SubnetPermissionedValidatorCurrentPriority
				require.NoError(service.vm.state.PutCurrentValidator(staker))

				stakersByTxID[staker.TxID] = staker
			}

			l1ValidatorsByVID := make(map[ids.ID]state.L1Validator)
			if len(test.l1Validators) != 0 {
				service.vm.state.SetSubnetToL1Conversion(subnetID,
					state.SubnetToL1Conversion{
						ConversionID: ids.GenerateTestID(),
						ChainID:      ids.GenerateTestID(),
						Addr:         []byte{'a', 'd', 'd', 'r'},
					})
			}

			for _, l1Validator := range test.l1Validators {
				deactivationOwner := message.PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				}

				remainingBalanceOwner := message.PChainOwner{
					Threshold: 1,
					Addresses: []ids.ShortID{
						ids.GenerateTestShortID(),
					},
				}

				remainingBalanceOwnerBytes, err := txs.Codec.Marshal(txs.CodecVersion, remainingBalanceOwner)
				require.NoError(err)
				deactivationOwnerBytes, err := txs.Codec.Marshal(txs.CodecVersion, deactivationOwner)
				require.NoError(err)
				l1Validator.RemainingBalanceOwner = remainingBalanceOwnerBytes
				l1Validator.DeactivationOwner = deactivationOwnerBytes

				require.NoError(service.vm.state.PutL1Validator(l1Validator))

				if l1Validator.Weight == 0 {
					continue
				}
				l1ValidatorsByVID[l1Validator.ValidationID] = l1Validator
			}

			service.vm.state.SetHeight(0)
			require.NoError(service.vm.state.Commit())
			service.vm.ctx.Lock.Unlock()

			testValidator := func(vdr any) ids.NodeID {
				switch v := vdr.(type) {
				case pchainapi.Staker:
					staker, exists := stakersByTxID[v.TxID]
					require.True(exists, "unexpected validator: %s", vdr)
					require.Equal(staker.NodeID, v.NodeID)
					require.Equal(avajson.Uint64(staker.Weight), v.Weight)
					require.Equal(staker.StartTime.Unix(), int64(v.StartTime))
					return v.NodeID
				case pchainapi.APIL1Validator:
					validator, exists := l1ValidatorsByVID[*v.ValidationID]
					require.True(exists, "unexpected validator: %s", vdr)
					require.Equal(validator.NodeID, v.NodeID)
					require.Equal(avajson.Uint64(validator.Weight), v.Weight)
					require.Equal(validator.StartTime, uint64(v.StartTime))
					accruedFees := service.vm.state.GetAccruedFees()
					require.Equal(avajson.Uint64(validator.EndAccumulatedFee-accruedFees), *v.Balance)
					require.Equal(avajson.Uint64(validator.MinNonce), *v.MinNonce)
					require.Equal(
						types.JSONByteSlice(bls.PublicKeyToCompressedBytes(bls.PublicKeyFromValidUncompressedBytes(validator.PublicKey))),
						*v.PublicKey)
					var expectedRemainingBalanceOwner message.PChainOwner
					_, err := txs.Codec.Unmarshal(validator.RemainingBalanceOwner, &expectedRemainingBalanceOwner)
					require.NoError(err)
					formattedRemainingBalanceOwner, err := service.addrManager.FormatLocalAddress(expectedRemainingBalanceOwner.Addresses[0])
					require.NoError(err)
					require.Equal(formattedRemainingBalanceOwner, v.RemainingBalanceOwner.Addresses[0])
					require.Equal(avajson.Uint32(expectedRemainingBalanceOwner.Threshold), v.RemainingBalanceOwner.Threshold)
					var expectedDeactivationOwner message.PChainOwner
					_, err = txs.Codec.Unmarshal(validator.DeactivationOwner, &expectedDeactivationOwner)
					require.NoError(err)
					formattedDeactivationOwner, err := service.addrManager.FormatLocalAddress(expectedDeactivationOwner.Addresses[0])
					require.NoError(err)
					require.Equal(formattedDeactivationOwner, v.DeactivationOwner.Addresses[0])
					require.Equal(avajson.Uint32(expectedDeactivationOwner.Threshold), v.DeactivationOwner.Threshold)
					return v.NodeID
				default:
					require.Failf("unexpected validator type", "got: %T", vdr)
					return ids.NodeID{}
				}
			}

			args := GetCurrentValidatorsArgs{
				SubnetID: subnetID,
			}
			reply := GetCurrentValidatorsReply{}
			require.NoError(service.GetCurrentValidators(&http.Request{}, &args, &reply))
			require.Len(reply.Validators, len(stakersByTxID)+len(l1ValidatorsByVID))
			for _, vdr := range reply.Validators {
				testValidator(vdr)
			}

			// Test with a specific node ID
			var nodeIDs []ids.NodeID
			if len(stakersByTxID) > 0 {
				// pick the first staker
				nodeIDs = append(nodeIDs, maps.Values(stakersByTxID)[0].NodeID)
			}
			if len(l1ValidatorsByVID) > 0 {
				nodeIDs = append(nodeIDs, maps.Values(l1ValidatorsByVID)[0].NodeID)
			}

			args.NodeIDs = nodeIDs
			reply = GetCurrentValidatorsReply{}
			require.NoError(service.GetCurrentValidators(&http.Request{}, &args, &reply))
			require.Len(reply.Validators, len(nodeIDs))
			for i, vdr := range reply.Validators {
				nodeID := testValidator(vdr)
				require.Equal(args.NodeIDs[i], nodeID)
			}
		})
	}
}

func TestGetValidatorFeeConfig(t *testing.T) {
	require := require.New(t)

	service, _ := defaultService(t)

	var reply fee.Config
	require.NoError(service.GetValidatorFeeConfig(nil, nil, &reply))
	require.Equal(defaultValidatorFeeConfig, reply)
}

func FuzzGetValidatorFeeState(f *testing.F) {
	f.Fuzz(func(t *testing.T, l1ValidatorExcess uint64) {
		require := require.New(t)

		service, _ := defaultService(t)

		var (
			expectedL1ValidatorExcess = gas.Gas(l1ValidatorExcess)
			expectedTime              = time.Now()
			expectedReply             = GetValidatorFeeStateReply{
				Excess: expectedL1ValidatorExcess,
				Price: gas.CalculatePrice(
					defaultValidatorFeeConfig.MinPrice,
					expectedL1ValidatorExcess,
					defaultValidatorFeeConfig.ExcessConversionConstant,
				),
				Time: expectedTime,
			}
		)

		service.vm.ctx.Lock.Lock()
		service.vm.state.SetL1ValidatorExcess(expectedL1ValidatorExcess)
		service.vm.state.SetTimestamp(expectedTime)
		service.vm.ctx.Lock.Unlock()

		var reply GetValidatorFeeStateReply
		require.NoError(service.GetValidatorFeeState(nil, nil, &reply))
		require.Equal(expectedReply, reply)
	})
}

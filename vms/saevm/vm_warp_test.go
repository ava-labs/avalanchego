// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"context"
	"math/big"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/rpc"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/sae"
	"github.com/ava-labs/strevm/saedb"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/warp"

	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	libevmcommon "github.com/ava-labs/libevm/common"
)

var (
	warpTxGasFeeCap = big.NewInt(225 * params.GWei)
	warpTxGasTipCap = big.NewInt(params.GWei)
)

type sut struct {
	ctx     context.Context
	snowCtx *snow.Context
	vm      *SinceGenesis
	client  *ethclient.Client
	chainID *big.Int
	signer  types.Signer
}

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

func TestSendWarpMessage(t *testing.T) {
	require := require.New(t)
	env := newSut(t)

	payloadData := utils.RandomBytes(100)
	warpSendMessageInput, err := warpcontract.PackSendWarpMessage(payloadData)
	require.NoError(err)

	addressedPayload, err := payload.NewAddressedCall(vmtest.TestEthAddrs[0].Bytes(), payloadData)
	require.NoError(err)
	expectedUnsignedMessage, err := avalancheWarp.NewUnsignedMessage(
		env.snowCtx.NetworkID,
		env.snowCtx.ChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(err)

	tx0 := types.NewTransaction(
		uint64(0),
		warpcontract.ContractAddress,
		big.NewInt(1),
		100_000,
		big.NewInt(25*params.GWei),
		warpSendMessageInput,
	)
	signedTx0, err := types.SignTx(tx0, env.signer, vmtest.TestKeys[0].ToECDSA())
	require.NoError(err)

	require.NoError(env.client.SendTransaction(env.ctx, signedTx0))

	built := env.buildBlock(t, nil)

	expectedBlockHashPayload, err := payload.NewHash(built.ID())
	require.NoError(err)
	expectedBlockUnsignedMessage, err := avalancheWarp.NewUnsignedMessage(env.snowCtx.NetworkID, env.snowCtx.ChainID, expectedBlockHashPayload.Bytes())
	require.NoError(err)

	addressedErr := env.vm.warpVerifier.Verify(env.ctx, expectedUnsignedMessage, nil)
	require.NotNil(addressedErr)
	require.Equal(int32(warp.TypeErrCode), addressedErr.Code)

	blockErr := env.vm.warpVerifier.Verify(env.ctx, expectedBlockUnsignedMessage, nil)
	require.NotNil(blockErr)
	require.Equal(int32(warp.VerifyErrCode), blockErr.Code)

	require.Len(built.EthBlock().Transactions(), 1)

	env.acceptBlock(t, built)

	receipts := built.Receipts()
	require.Len(receipts, 1)
	require.Len(receipts[0].Logs, 1)
	expectedTopics := []libevmcommon.Hash{
		warpcontract.WarpABI.Events["SendWarpMessage"].ID,
		libevmcommon.BytesToHash(vmtest.TestEthAddrs[0].Bytes()),
		libevmcommon.Hash(expectedUnsignedMessage.ID()),
	}
	require.Equal(expectedTopics, receipts[0].Logs[0].Topics)
	logData := receipts[0].Logs[0].Data
	unsignedMessage, err := warpcontract.UnpackSendWarpEventDataToMessage(logData)
	require.NoError(err)

	require.Nil(env.vm.warpVerifier.Verify(env.ctx, unsignedMessage, nil))
	require.Nil(env.vm.warpVerifier.Verify(env.ctx, expectedBlockUnsignedMessage, nil))
}

func TestPredicateVerification(t *testing.T) {
	sourceChainID := ids.GenerateTestID()
	networkID := snowtest.Context(t, snowtest.CChainID).NetworkID

	sourceAddress := secp256k1.TestKeys()[0].EthAddress()
	addressedPayload, err := payload.NewAddressedCall(sourceAddress.Bytes(), []byte{1, 2, 3})
	require.NoError(t, err)
	addressedCallMessage, err := avalancheWarp.NewUnsignedMessage(
		networkID,
		sourceChainID,
		addressedPayload.Bytes(),
	)
	require.NoError(t, err)
	addressedCallTxPayload, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err)

	blockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	blockHashMessage, err := avalancheWarp.NewUnsignedMessage(
		networkID,
		sourceChainID,
		blockHashPayload.Bytes(),
	)
	require.NoError(t, err)
	blockHashTxPayload, err := warpcontract.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err)

	tests := []struct {
		name           string
		validPredicate bool
		unsignedMsg    *avalancheWarp.UnsignedMessage
		txPayload      []byte
	}{
		{
			name:           "valid warp message",
			validPredicate: true,
			unsignedMsg:    addressedCallMessage,
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "invalid warp message",
			validPredicate: false,
			unsignedMsg:    addressedCallMessage,
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "valid warp block hash",
			validPredicate: true,
			unsignedMsg:    blockHashMessage,
			txPayload:      blockHashTxPayload,
		},
		{
			name:           "invalid warp block hash",
			validPredicate: false,
			unsignedMsg:    blockHashMessage,
			txPayload:      blockHashTxPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			sut := newSut(t)

			signedMsg := signWarpMessage(t, sut.snowCtx, tt.unsignedMsg, tt.validPredicate)
			validateTx := sut.sendWarpTx(t, tt.txPayload, signedMsg)

			built := sut.buildBlock(t, &block.Context{PChainHeight: 0})
			require.Len(built.EthBlock().Transactions(), 1)

			sut.acceptBlock(t, built)
			assertPredicateResult(t, built, validateTx, tt.validPredicate)

			receipts := built.Receipts()
			require.Len(receipts, 1)
			require.Equal(types.ReceiptStatusSuccessful, receipts[0].Status)
		})
	}
}

func newSut(t *testing.T) *sut {
	t.Helper()

	require := require.New(t)
	ctx := t.Context()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.NetworkUpgrades = upgradetest.GetConfig(upgradetest.Durango)

	genesisBytes := []byte(vmtest.GenesisJSON(paramstest.ForkToChainConfig[upgradetest.Durango]))

	mempoolConf := legacypool.DefaultConfig
	mempoolConf.Journal = "/dev/null"

	vm := NewSinceGenesis(sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			TrieDBConfig: triedb.HashDefaults,
		},
	})

	appSender := &enginetest.Sender{
		SendAppGossipF: func(context.Context, engcommon.SendConfig, []byte) error { return nil },
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		nil,
		appSender,
	))
	t.Cleanup(func() {
		require.NoError(vm.Shutdown(context.WithoutCancel(ctx)))
	})

	require.NoError(vm.SetState(ctx, snow.Bootstrapping))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	handlers, err := vm.CreateHandlers(ctx)
	require.NoError(err)
	server := httptest.NewServer(handlers["/ws"])
	t.Cleanup(server.Close)

	rpcClient, err := rpc.Dial("ws://" + server.Listener.Addr().String())
	require.NoError(err)
	t.Cleanup(rpcClient.Close)

	client := ethclient.NewClient(rpcClient)
	chainID, err := client.ChainID(ctx)
	require.NoError(err)

	lastID, err := vm.LastAccepted(ctx)
	require.NoError(err)
	require.NoError(vm.SetPreference(ctx, lastID, nil))

	return &sut{
		ctx:     ctx,
		snowCtx: snowCtx,
		vm:      vm,
		client:  client,
		chainID: chainID,
		signer:  types.LatestSignerForChainID(chainID),
	}
}

func (s *sut) buildBlock(t *testing.T, blockCtx *block.Context) *blocks.Block {
	t.Helper()

	require := require.New(t)

	msg, err := s.vm.WaitForEvent(s.ctx)
	require.NoError(err)
	require.Equal(engcommon.PendingTxs, msg)

	built, err := s.vm.BuildBlock(s.ctx, blockCtx)
	require.NoError(err)
	require.NoError(s.vm.VerifyBlock(s.ctx, blockCtx, built))
	return built
}

func (s *sut) acceptBlock(t *testing.T, built *blocks.Block) {
	t.Helper()

	require := require.New(t)
	require.NoError(s.vm.SetPreference(s.ctx, built.ID(), nil))
	require.NoError(s.vm.AcceptBlock(s.ctx, built))
	require.NoError(built.WaitUntilExecuted(s.ctx))
}

func (s *sut) sendWarpTx(
	t *testing.T,
	txPayload []byte,
	signedMessage *avalancheWarp.Message,
) *types.Transaction {
	t.Helper()

	require := require.New(t)

	warpAddr := warpcontract.ContractAddress
	tx, err := types.SignTx(types.NewTx(&types.DynamicFeeTx{
		ChainID:   s.chainID,
		Nonce:     0,
		To:        &warpAddr,
		Gas:       200_000,
		GasFeeCap: new(big.Int).Set(warpTxGasFeeCap),
		GasTipCap: new(big.Int).Set(warpTxGasTipCap),
		Value:     big.NewInt(0),
		Data:      txPayload,
		AccessList: types.AccessList{{
			Address:     warpcontract.ContractAddress,
			StorageKeys: predicate.New(signedMessage.Bytes()),
		}},
	}), s.signer, vmtest.TestKeys[0].ToECDSA())
	require.NoError(err)

	require.NoError(s.client.SendTransaction(s.ctx, tx))
	return tx
}

func assertPredicateResult(
	t *testing.T,
	built *blocks.Block,
	validateTx *types.Transaction,
	expectValid bool,
) {
	t.Helper()

	require := require.New(t)

	rules := paramstest.ForkToChainConfig[upgradetest.Durango].Rules(
		built.EthBlock().Number(),
		params.IsMergeTODO,
		built.EthBlock().Time(),
	)
	rulesExtra := params.GetRulesExtra(rules)
	headerPredicateResultsBytes := customheader.PredicateBytesFromExtra(
		rulesExtra.AvalancheRules,
		built.EthBlock().Extra(),
	)
	blockResults, err := predicate.ParseBlockResults(headerPredicateResultsBytes)
	require.NoError(err)

	txBits := blockResults.Get(validateTx.Hash(), warpcontract.ContractAddress)
	if expectValid {
		require.Zero(txBits.Len())
		return
	}

	require.Equal(1, txBits.Len())
	require.True(txBits.Contains(0))
}

func signWarpMessage(
	t *testing.T,
	snowCtx *snow.Context,
	unsignedMessage *avalancheWarp.UnsignedMessage,
	validSignature bool,
) *avalancheWarp.Message {
	t.Helper()

	require := require.New(t)

	nodeID0 := ids.GenerateTestNodeID()
	secretKey0, err := localsigner.New()
	require.NoError(err)

	nodeID1 := ids.GenerateTestNodeID()
	secretKey1, err := localsigner.New()
	require.NoError(err)

	sourceChainID := unsignedMessage.SourceChainID
	subnetID := ids.GenerateTestID()
	snowCtx.ValidatorState = &validatorstest.State{
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			require.Equal(sourceChainID, chainID)
			return subnetID, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			validatorSet := validators.WarpSet{
				Validators: []*validators.Warp{
					{
						PublicKey:      secretKey0.PublicKey(),
						PublicKeyBytes: bls.PublicKeyToUncompressedBytes(secretKey0.PublicKey()),
						Weight:         50,
						NodeIDs:        []ids.NodeID{nodeID0},
					},
					{
						PublicKey:      secretKey1.PublicKey(),
						PublicKeyBytes: bls.PublicKeyToUncompressedBytes(secretKey1.PublicKey()),
						Weight:         50,
						NodeIDs:        []ids.NodeID{nodeID1},
					},
				},
				TotalWeight: 100,
			}
			utils.Sort(validatorSet.Validators)

			return map[ids.ID]validators.WarpSet{
				subnetID: validatorSet,
			}, nil
		},
	}

	if !validSignature {
		warpSignature := &avalancheWarp.BitSetSignature{
			Signers: set.NewBits().Bytes(),
		}
		signedMessage, err := avalancheWarp.NewMessage(unsignedMessage, warpSignature)
		require.NoError(err)
		return signedMessage
	}

	signature0, err := secretKey0.Sign(unsignedMessage.Bytes())
	require.NoError(err)
	signature1, err := secretKey1.Sign(unsignedMessage.Bytes())
	require.NoError(err)

	aggregatedSignature, err := bls.AggregateSignatures([]*bls.Signature{signature0, signature1})
	require.NoError(err)

	signersBitSet := set.NewBits()
	signersBitSet.Add(0)
	signersBitSet.Add(1)

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: signersBitSet.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregatedSignature))

	signedMessage, err := avalancheWarp.NewMessage(unsignedMessage, warpSignature)
	require.NoError(err)
	return signedMessage
}

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saevm

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rpc"
	"github.com/ava-labs/libevm/triedb"
	"github.com/ava-labs/strevm/blocks"
	saeparams "github.com/ava-labs/strevm/params"
	"github.com/ava-labs/strevm/sae"
	"github.com/ava-labs/strevm/saedb"
	"github.com/ava-labs/strevm/saetest"
	"github.com/stretchr/testify/require"
)

type SUT struct {
	ctx           context.Context
	snowCtx       *snow.Context
	vm            *SinceGenesis
	client        *ethclient.Client
	chainID       *big.Int
	wallet        *saetest.Wallet
	validatorKeys []*localsigner.LocalSigner

	appResponse chan []byte
	appErr      chan *engcommon.AppError
}

func newSUT(t *testing.T) *SUT {
	t.Helper()

	// Test will fail if any error log from libevm, or warn log from SAE, is emitted.
	// Some warn logs from libevm are expected.
	log.SetDefault(log.NewLogger(ethtest.NewTBLogHandler(t, log.LevelError)))
	logger := saetest.NewTBLogger(t, logging.Info)
	ctx := logger.CancelOnError(t.Context())

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.NetworkUpgrades = upgradetest.GetConfig(upgradetest.Durango)
	snowCtx.Log = logger
	validatorState, validatorKeys := createValidatorState()
	snowCtx.ValidatorState = validatorState

	mempoolConf := legacypool.DefaultConfig
	mempoolConf.Journal = "/dev/null"

	const numKeys = 1
	keychain := saetest.NewUNSAFEKeyChain(t, numKeys)
	g := &core.Genesis{
		Config:     paramstest.ForkToChainConfig[upgradetest.Durango],
		Alloc:      saetest.MaxAllocFor(keychain.Addresses()...),
		Timestamp:  saeparams.TauSeconds,
		Difficulty: big.NewInt(0),
	}
	genesisBytes, err := json.Marshal(g)
	require.NoError(t, err)

	vm := NewSinceGenesis(sae.Config{
		MempoolConfig: mempoolConf,
		DBConfig: saedb.Config{
			TrieDBConfig: triedb.HashDefaults,
		},
	})

	// allow receiving responses via [SUT.verifyWarpMessage]
	appResponseCh := make(chan []byte, 1)
	appErrCh := make(chan *engcommon.AppError, 1)
	appSender := &enginetest.SenderStub{
		SentAppResponse: appResponseCh,
		SentAppError:    appErrCh,
	}

	require.NoError(t, vm.Initialize(
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
		require.NoError(t, vm.Shutdown(context.WithoutCancel(ctx)))
	})

	require.NoError(t, vm.SetState(ctx, snow.NormalOp))

	handlers, err := vm.CreateHandlers(ctx)
	require.NoError(t, err)
	server := httptest.NewServer(handlers["/ws"])
	t.Cleanup(server.Close)

	rpcClient, err := rpc.Dial("ws://" + server.Listener.Addr().String())
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)

	client := ethclient.NewClient(rpcClient)
	chainID, err := client.ChainID(ctx)
	require.NoError(t, err)

	// TODO(alarso16): delete this - it should be on the VM
	lastID, err := vm.LastAccepted(ctx)
	require.NoError(t, err)
	require.NoError(t, vm.SetPreference(ctx, lastID, nil))

	return &SUT{
		ctx:     ctx,
		snowCtx: snowCtx,
		vm:      vm,
		client:  client,
		chainID: chainID,
		wallet: saetest.NewWalletWithKeyChain(
			keychain,
			types.LatestSigner(g.Config),
		),
		validatorKeys: validatorKeys,
		appResponse:   appResponseCh,
		appErr:        appErrCh,
	}
}

func (s *SUT) buildAndVerifyBlock(t *testing.T, blockCtx *block.Context) *blocks.Block {
	t.Helper()

	msg, err := s.vm.WaitForEvent(s.ctx)
	require.NoError(t, err)
	require.Equal(t, engcommon.PendingTxs, msg)

	built, err := s.vm.BuildBlock(s.ctx, blockCtx)
	require.NoError(t, err)
	require.NoError(t, s.vm.VerifyBlock(s.ctx, blockCtx, built))
	return built
}

func (s *SUT) acceptAndExecuteBlock(t *testing.T, built *blocks.Block) {
	t.Helper()

	require.NoError(t, s.vm.SetPreference(s.ctx, built.ID(), nil))
	require.NoError(t, s.vm.AcceptBlock(s.ctx, built))
	require.NoError(t, built.WaitUntilExecuted(s.ctx))
}

func createValidatorState() (*validatorstest.State, []*localsigner.LocalSigner) {
	const (
		numValidators      = 2
		weightPerValidator = 50
	)

	secretKeys := make([]*localsigner.LocalSigner, numValidators)
	nodeIDs := make([]ids.NodeID, numValidators)
	for i := range numValidators {
		key, _ := localsigner.New() // Uses rand, never returns error
		secretKeys[i] = key
		nodeIDs[i] = ids.GenerateTestNodeID()
	}

	subnetID := ids.GenerateTestID()
	return &validatorstest.State{
		GetValidatorSetF: func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
			return map[ids.NodeID]*validators.GetValidatorOutput{}, nil
		},
		GetMinimumHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetCurrentHeightF: func(context.Context) (uint64, error) {
			return 0, nil
		},
		GetSubnetIDF: func(_ context.Context, chainID ids.ID) (ids.ID, error) {
			return subnetID, nil
		},
		GetWarpValidatorSetsF: func(context.Context, uint64) (map[ids.ID]validators.WarpSet, error) {
			warpValidators := make([]*validators.Warp, numValidators)
			for i := range numValidators {
				warpValidators[i] = &validators.Warp{
					PublicKey:      secretKeys[i].PublicKey(),
					PublicKeyBytes: bls.PublicKeyToUncompressedBytes(secretKeys[i].PublicKey()),
					Weight:         50,
					NodeIDs:        []ids.NodeID{nodeIDs[i]},
				}
			}
			validatorSet := validators.WarpSet{
				Validators:  warpValidators,
				TotalWeight: weightPerValidator * numValidators,
			}
			utils.Sort(validatorSet.Validators)

			return map[ids.ID]validators.WarpSet{
				subnetID: validatorSet,
			}, nil
		},
	}, secretKeys
}

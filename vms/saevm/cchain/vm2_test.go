// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// This file holds the original sae-devnet-5 test scaffolding (SUT2) and the
// atomic/warp coverage that was built on top of it. It is preserved verbatim
// from before the minimal-hook merge so that the existing tests keep passing
// while we incrementally port them onto the minimal-hook SUT in vm_test.go.

package cchain

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/libevm/ethtest"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/database/prefixdb"
	"github.com/ava-labs/avalanchego/graft/coreth/core/extstate"
	"github.com/ava-labs/avalanchego/graft/coreth/params"
	"github.com/ava-labs/avalanchego/graft/coreth/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/atomic"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/validatorstest"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/tx"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/warp"
	"github.com/ava-labs/avalanchego/vms/saevm/saetest"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	warpcontract "github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

type SUT2 struct {
	ctx     context.Context
	snowCtx *snow.Context
	vm      *VM
	client  *ethclient.Client

	// Wallet for issuing transactions
	ethWallet     *saetest.Wallet
	validatorKeys []*localsigner.LocalSigner

	// For issuing atomic transactions
	atomicKey    *secp256k1.PrivateKey
	atomicMemory *avalancheatomic.Memory
	avaxClient   *rpc.Client

	// See [SUT2.verifyWarpMessage]
	appResponse chan []byte
	appErr      chan *engcommon.AppError
}

func newSUT2(t *testing.T) *SUT2 {
	t.Helper()

	// TODO(alarso16): this will need to be parameterizable
	const fork = upgradetest.Durango
	upgrades := upgradetest.GetConfig(fork)

	// Test will fail if any error log from libevm, or warn log from SAE, is
	// emitted. Some warn logs from libevm are expected.
	log.SetDefault(log.NewLogger(ethtest.NewTBLogHandler(t, log.LevelError)))
	logger := saetest.NewTBLogger(t, logging.Info)
	ctx := logger.CancelOnError(t.Context())

	baseDB := memdb.New()
	atomicMemory := avalancheatomic.NewMemory(prefixdb.New([]byte{0}, baseDB))
	snowCtx, validatorKeys := newSnowCtx(t, upgrades, atomicMemory)

	const numKeys = 1
	keychain := saetest.NewUNSAFEKeyChain(t, numKeys)
	atomicKey, err := secp256k1.NewPrivateKey()
	require.NoError(t, err)
	g := &core.Genesis{
		Config:     paramstest.ForkToChainConfig[fork],
		Alloc:      saetest.MaxAllocFor(append(keychain.Addresses(), atomicKey.EthAddress())...),
		Timestamp:  saeparams.TauSeconds,
		Difficulty: big.NewInt(0),
	}
	genesisBytes, err := json.Marshal(g)
	require.NoError(t, err)

	vm := &VM{}

	// allow receiving responses via [SUT2.verifyWarpMessage]
	appResponseCh := make(chan []byte, 1)
	appErrCh := make(chan *engcommon.AppError, 1)
	appSender := &enginetest.SenderStub{
		SentAppResponse: appResponseCh,
		SentAppError:    appErrCh,
	}

	require.NoError(t, vm.Initialize(
		ctx,
		snowCtx,
		baseDB,
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

	uri := server.Listener.Addr().String()
	rpcClient, err := rpc.Dial("ws://" + uri)
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)

	client := ethclient.NewClient(rpcClient)

	avaxServer := httptest.NewServer(handlers[avaxHTTPExtensionPath])
	t.Cleanup(avaxServer.Close)
	avaxClient, err := rpc.Dial("http://" + avaxServer.Listener.Addr().String())
	require.NoError(t, err)

	// TODO(alarso16): delete this - it should be on the VM
	lastID, err := vm.LastAccepted(ctx)
	require.NoError(t, err)
	require.NoError(t, vm.SetPreference(ctx, lastID, nil))

	return &SUT2{
		ctx:     ctx,
		snowCtx: snowCtx,
		vm:      vm,
		client:  client,
		ethWallet: saetest.NewWalletWithKeyChain(
			keychain,
			types.LatestSigner(g.Config),
		),
		validatorKeys: validatorKeys,
		appResponse:   appResponseCh,
		appErr:        appErrCh,
		atomicKey:     atomicKey,
		atomicMemory:  atomicMemory,
		avaxClient:    avaxClient,
	}
}

func (s *SUT2) buildAndVerifyBlock(t *testing.T, blockCtx *block.Context) *blocks.Block {
	t.Helper()

	msg, err := s.vm.WaitForEvent(s.ctx)
	require.NoError(t, err)
	require.Equal(t, engcommon.PendingTxs, msg)

	built, err := s.vm.BuildBlock(s.ctx, blockCtx)
	require.NoError(t, err)
	require.NoError(t, s.vm.VerifyBlock(s.ctx, blockCtx, built))
	return built
}

func (s *SUT2) acceptAndExecuteBlock(t *testing.T, built *blocks.Block) {
	t.Helper()

	require.NoError(t, s.vm.SetPreference(s.ctx, built.ID(), nil))
	require.NoError(t, s.vm.AcceptBlock(s.ctx, built))
	require.NoError(t, built.WaitUntilExecuted(s.ctx))
}

func newSnowCtx(t *testing.T, upgrades upgrade.Config, atomicMemory *avalancheatomic.Memory) (*snow.Context, []*localsigner.LocalSigner) {
	t.Helper()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.NetworkUpgrades = upgrades
	validatorState, validatorKeys := newValidatorState(snowCtx.SubnetID)
	snowCtx.ValidatorState = validatorState
	snowCtx.SharedMemory = atomicMemory.NewSharedMemory(snowCtx.ChainID)
	return snowCtx, validatorKeys
}

func newValidatorState(subnetID ids.ID) (*validatorstest.State, []*localsigner.LocalSigner) {
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
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
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

// TestExportTx adds an atomic export and verifies that the exported UTXO is in shared memory.
func TestExportTx(t *testing.T) {
	t.Skip("failing") // TODO: FIXME

	sut := newSUT2(t)

	tests := []struct {
		name      string
		amount    uint64
		destChain ids.ID
	}{
		{
			name:      "P Chain",
			amount:    1,
			destChain: sut.snowCtx.SubnetID,
		},
		{
			name:      "X Chain",
			amount:    1,
			destChain: sut.snowCtx.XChainID,
		},
		{
			name:      "Random Chain",
			amount:    1,
			destChain: ids.GenerateTestID(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exportTx := sut.issueExportTx(t, tt.destChain, tt.amount)

			// Tx added build time
			b := sut.buildAndVerifyBlock(t, nil)
			opts, err := sut.vm.hooks.EndOfBlockOps(b.EthBlock())
			require.NoError(t, err)
			require.Len(t, opts, 1)

			// Result should be available after execution
			sut.acceptAndExecuteBlock(t, b)
			sm := sut.atomicMemory.NewSharedMemory(tt.destChain)
			indexedValues, _, _, err := sm.Indexed(sut.snowCtx.ChainID, [][]byte{sut.atomicKey.Address().Bytes()}, nil, nil, 3)
			require.NoError(t, err)
			require.Len(t, indexedValues, 1)

			// Exact UTXO should be in shared memory
			_, req, err := exportTx.AtomicRequests() // codec isn't exported, can still get UTXO marshaled like this
			require.NoError(t, err)
			require.Len(t, req.PutRequests, 1)
			require.Equal(t, req.PutRequests[0].Value, indexedValues[0])

			// Check nonce for address, it should be incremented by 1
			prevBlock := new(big.Int).Sub(b.Number(), common.Big1)
			startNonce, err := sut.client.NonceAt(sut.ctx, sut.atomicKey.EthAddress(), prevBlock)
			require.NoError(t, err)
			endNonce, err := sut.client.NonceAt(sut.ctx, sut.atomicKey.EthAddress(), b.Number())
			require.NoError(t, err)
			require.Equal(t, startNonce+1, endNonce)
		})
	}
}

func (s *SUT2) issueExportTx(t *testing.T, chainID ids.ID, amount uint64) *tx.Tx {
	id, err := s.vm.LastAccepted(s.ctx)
	require.NoError(t, err)

	b := s.vm.GethRPCBackends()
	state, _, err := b.StateAndHeaderByNumberOrHash(
		s.ctx, rpc.BlockNumberOrHashWithHash(common.Hash(id), true),
	)
	require.NoErrorf(t, err, "%T.StateAndHeaderByNumberOrHash()", b)

	// TODO: Failing because eth_baseFee is returning null
	var hex hexutil.Big
	require.NoError(t, s.client.Client().CallContext(s.ctx, &hex, "eth_baseFee"), "eth_baseFee")
	baseFee := (*big.Int)(&hex)

	rules := params.TestDurangoChainConfig.Rules(new(big.Int), params.IsMergeTODO, 0)

	// TODO(alarso16): Use new atomic code. This is identical after marshaled.
	corethTx, err := atomic.NewExportTx(
		s.snowCtx,
		*params.GetRulesExtra(rules),
		extstate.New(state),
		s.snowCtx.AVAXAssetID,
		amount,
		chainID,
		s.atomicKey.Address(),
		baseFee,
		[]*secp256k1.PrivateKey{s.atomicKey},
	)
	require.NoError(t, err)
	txBytes := corethTx.SignedBytes()

	res := &api.JSONTxID{}
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	require.NoError(t, err)
	require.NoError(t, s.avaxClient.Call(res, "avax.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}))

	// Copy into new Tx type for better testing.
	newTx, err := tx.Parse(txBytes)
	require.NoError(t, err)

	return newTx
}

// TestSendWarpMessage checks availability of warp verification requests
// relative to block execution.
func TestSendWarpMessage(t *testing.T) {
	sut := newSUT2(t)

	payloadData := utils.RandomBytes(100)

	warpSendMessageInput, err := warpcontract.PackSendWarpMessage(payloadData)
	require.NoError(t, err)
	sut.sendWarpTx(t, warpSendMessageInput, nil /*unsigned message*/)

	built := sut.buildAndVerifyBlock(t, nil)
	require.Len(t, built.EthBlock().Transactions(), 1)

	// The validator will not sign any messages, since the transaction is not executed yet.

	addressedPayload, err := payload.NewAddressedCall(sut.ethWallet.Addresses()[0].Bytes(), payloadData)
	require.NoError(t, err)
	unsignedMessage := sut.newUnsignedWarpMessage(t, addressedPayload.Bytes())
	sut.verifyWarpMessage(t, unsignedMessage.Bytes(), int32(warp.TypeErrCode))

	blockHashPayload, err := payload.NewHash(built.ID())
	require.NoError(t, err)
	blockMessage := sut.newUnsignedWarpMessage(t, blockHashPayload.Bytes())
	sut.verifyWarpMessage(t, blockMessage.Bytes(), int32(warp.VerifyErrCode))

	sut.acceptAndExecuteBlock(t, built)

	// Receipts are generated after block execution
	receipts := built.Receipts()
	require.Len(t, receipts, 1)
	require.Len(t, receipts[0].Logs, 1)
	expectedTopics := []common.Hash{
		warpcontract.WarpABI.Events["SendWarpMessage"].ID,
		common.BytesToHash(sut.ethWallet.Addresses()[0].Bytes()),
		common.Hash(unsignedMessage.ID()),
	}
	require.Equal(t, expectedTopics, receipts[0].Logs[0].Topics)

	// Unsigned warp message should have been emitted in the log.
	logData := receipts[0].Logs[0].Data
	loggedMessage, err := warpcontract.UnpackSendWarpEventDataToMessage(logData)
	require.NoError(t, err)
	require.Equal(t, unsignedMessage, loggedMessage)

	// The messages should be verifiable after the block is executed.
	sut.verifyWarpMessage(t, unsignedMessage.Bytes(), 0 /* valid message */)
	sut.verifyWarpMessage(t, blockMessage.Bytes(), 0 /* valid message */)
}

func TestPredicateVerification(t *testing.T) {
	sut := newSUT2(t)

	sourceAddress := sut.ethWallet.Addresses()[0]
	addressedPayload, err := payload.NewAddressedCall(sourceAddress.Bytes(), []byte{1, 2, 3})
	require.NoError(t, err)
	addressedCallMessage := sut.newUnsignedWarpMessage(t, addressedPayload.Bytes())
	addressedCallTxPayload, err := warpcontract.PackGetVerifiedWarpMessage(0)
	require.NoError(t, err)

	blockHashPayload, err := payload.NewHash(ids.GenerateTestID())
	require.NoError(t, err)
	blockHashMessage := sut.newUnsignedWarpMessage(t, blockHashPayload.Bytes())
	blockHashTxPayload, err := warpcontract.PackGetVerifiedWarpBlockHash(0)
	require.NoError(t, err)

	tests := []struct {
		name           string
		validPredicate bool
		signedMsg      *avalancheWarp.Message
		txPayload      []byte
	}{
		{
			name:           "valid warp message",
			validPredicate: true,
			signedMsg:      sut.signWarpMessage(t, addressedCallMessage),
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "invalid warp message",
			validPredicate: false,
			signedMsg:      fakeSign(t, addressedCallMessage),
			txPayload:      addressedCallTxPayload,
		},
		{
			name:           "valid warp block hash",
			validPredicate: true,
			signedMsg:      sut.signWarpMessage(t, blockHashMessage),
			txPayload:      blockHashTxPayload,
		},
		{
			name:           "invalid warp block hash",
			validPredicate: false,
			signedMsg:      fakeSign(t, blockHashMessage),
			txPayload:      blockHashTxPayload,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validateTx := sut.sendWarpTx(t, tt.txPayload, tt.signedMsg)

			built := sut.buildAndVerifyBlock(t, &block.Context{PChainHeight: 0})
			require.Len(t, built.EthBlock().Transactions(), 1)

			sut.acceptAndExecuteBlock(t, built)
			assertPredicateResult(t, built, validateTx, tt.validPredicate)

			receipts := built.Receipts()
			require.Len(t, receipts, 1)
			require.Equal(t, types.ReceiptStatusSuccessful, receipts[0].Status)
		})
	}
}

func assertPredicateResult(
	t *testing.T,
	built *blocks.Block,
	validateTx *types.Transaction,
	expectValid bool,
) {
	t.Helper()

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
	require.NoError(t, err)

	txBits := blockResults.Get(validateTx.Hash(), warpcontract.ContractAddress)
	if expectValid {
		require.Zero(t, txBits.Len())
		return
	}

	require.Equal(t, 1, txBits.Len())
	require.True(t, txBits.Contains(0))
}

func (s *SUT2) newUnsignedWarpMessage(t *testing.T, payload []byte) *avalancheWarp.UnsignedMessage {
	t.Helper()

	message, err := avalancheWarp.NewUnsignedMessage(s.snowCtx.NetworkID, s.snowCtx.ChainID, payload)
	require.NoError(t, err)
	return message
}

func (s *SUT2) sendWarpTx(
	t *testing.T,
	txPayload []byte,
	signedMessage *avalancheWarp.Message,
) *types.Transaction {
	t.Helper()

	var accessList types.AccessList
	if signedMessage != nil {
		accessList = types.AccessList{{
			Address:     warpcontract.ContractAddress,
			StorageKeys: predicate.New(signedMessage.Bytes()),
		}}
	}

	warpAddr := warpcontract.ContractAddress
	tx := s.ethWallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:         &warpAddr,
		Gas:        200_000,
		GasFeeCap:  big.NewInt(225 * params.GWei),
		Value:      big.NewInt(0),
		Data:       txPayload,
		AccessList: accessList,
	})

	require.NoError(t, s.client.SendTransaction(s.ctx, tx))
	return tx
}

func (s *SUT2) signWarpMessage(
	t *testing.T,
	unsignedMessage *avalancheWarp.UnsignedMessage,
) *avalancheWarp.Message {
	t.Helper()

	signatures := make([]*bls.Signature, len(s.validatorKeys))
	for i, key := range s.validatorKeys {
		signature, err := key.Sign(unsignedMessage.Bytes())
		require.NoError(t, err)
		signatures[i] = signature
	}

	aggregatedSignature, err := bls.AggregateSignatures(signatures)
	require.NoError(t, err)

	signersBitSet := set.NewBits()
	signersBitSet.Add(0)
	signersBitSet.Add(1)

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers: signersBitSet.Bytes(),
	}
	copy(warpSignature.Signature[:], bls.SignatureToBytes(aggregatedSignature))

	signedMessage, err := avalancheWarp.NewMessage(unsignedMessage, warpSignature)
	require.NoError(t, err)
	return signedMessage
}

func fakeSign(t *testing.T, unsignedMessage *avalancheWarp.UnsignedMessage) *avalancheWarp.Message {
	t.Helper()

	warpSignature := &avalancheWarp.BitSetSignature{
		Signers:   set.NewBits().Bytes(),
		Signature: [96]byte{1, 2, 3},
	}
	signedMessage, err := avalancheWarp.NewMessage(unsignedMessage, warpSignature)
	require.NoError(t, err)
	return signedMessage
}

// verifyWarpMessage sends a message to the warp handler and verifies that the
// response is as expected based on the provided error code. If `expected` is 0,
// then a valid message is expected.
func (s *SUT2) verifyWarpMessage(t *testing.T, payloadData []byte, expected int32) {
	t.Helper()

	protoMsg := &sdk.SignatureRequest{Message: payloadData}
	requestBytes, err := proto.Marshal(protoMsg)
	require.NoError(t, err)

	msg := p2p.PrefixMessage(p2p.ProtocolPrefix(acp118.HandlerID), requestBytes)
	deadline, _ := s.ctx.Deadline() // deadline is ignored
	require.NoError(t, s.vm.AppRequest(t.Context(), ids.GenerateTestNodeID(), 1, deadline, msg))

	var (
		responseBytes []byte
		appErr        *engcommon.AppError
	)
	select {
	case responseBytes = <-s.appResponse:
	case appErr = <-s.appErr:
	case <-t.Context().Done():
		t.Fatalf("waiting for app response: %s", t.Context().Err())
	}

	switch expected {
	case 0:
		require.Nil(t, appErr)
		require.NotNil(t, responseBytes)
	default:
		require.Nil(t, responseBytes)
		require.NotNil(t, appErr)
		require.Equal(t, expected, appErr.Code)
	}
}

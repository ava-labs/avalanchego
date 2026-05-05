// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnetevm

import (
	"math/big"
	"os"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/extras"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/subnetevm/warp"

	warpcontract "github.com/ava-labs/avalanchego/graft/subnet-evm/precompile/contracts/warp"
	engcommon "github.com/ava-labs/avalanchego/snow/engine/common"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestMain(m *testing.M) {
	evm.RegisterAllLibEVMExtras()
	os.Exit(m.Run())
}

// withWarpEnabled schedules the `warp` precompile at genesis so that the SAE
// block builder populates predicate bytes in `header.Extra` and ABI calls to
// `warpcontract.ContractAddress` are routed to the warp precompile.
//
// Composes with any pre-existing `withGenesisConfig`: the prior callback runs
// first, then warp is registered into [extras.ChainConfig.GenesisPrecompiles].
// The activation timestamp matches `upgrade.InitiallyActiveTime` (the time
// returned by `upgradetest.GetConfig` for any scheduled fork), satisfying
// [warpcontract.Config.Verify]'s "warp must activate at-or-after Durango"
// invariant on every fork the SUT supports.
func withWarpEnabled() sutOption {
	return options.Func[sutConfig](func(c *sutConfig) {
		prev := c.configureGenesis
		c.configureGenesis = func(genesis *core.Genesis, addresses []common.Address) {
			if prev != nil {
				prev(genesis, addresses)
			}
			extra := params.GetExtra(genesis.Config)
			if extra.GenesisPrecompiles == nil {
				extra.GenesisPrecompiles = extras.Precompiles{}
			}
			activationTS := utils.PointerTo(uint64(upgrade.InitiallyActiveTime.Unix()))
			extra.GenesisPrecompiles[warpcontract.ConfigKey] = warpcontract.NewDefaultConfig(activationTS)
		}
	})
}

// TestSendWarpMessage checks availability of warp verification requests
// relative to block execution.
func TestSendWarpMessage(t *testing.T) {
	sut := newSUT(t, withWarpEnabled())

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
	sut.verifyWarpMessage(t, unsignedMessage.Bytes(), int32(warp.VerifyErrCode))

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
	sut := newSUT(t, withWarpEnabled())

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

	headerPredicateResultsBytes := customheader.PredicateBytesFromExtra(
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

func (s *SUT) newUnsignedWarpMessage(t *testing.T, payload []byte) *avalancheWarp.UnsignedMessage {
	t.Helper()

	message, err := avalancheWarp.NewUnsignedMessage(s.snowCtx.NetworkID, s.snowCtx.ChainID, payload)
	require.NoError(t, err)
	return message
}

func (s *SUT) sendWarpTx(
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

func (s *SUT) signWarpMessage(
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
func (s *SUT) verifyWarpMessage(t *testing.T, payloadData []byte, expected int32) {
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
		t.Fatalf("waiting for app response: %v", t.Context().Err())
	}

	switch expected {
	case 0:
		require.Nil(t, appErr)
		// The response is a signature - out of scope for this test.
		// Any response is good!
		require.NotNil(t, responseBytes)
	default:
		require.Nil(t, responseBytes)
		require.NotNil(t, appErr)
		require.Equal(t, expected, appErr.Code)
	}
}

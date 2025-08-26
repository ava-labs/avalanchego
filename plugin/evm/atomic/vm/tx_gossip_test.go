// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vm

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/coreth/plugin/evm/atomic"
	"github.com/ava-labs/coreth/plugin/evm/config"
	"github.com/ava-labs/coreth/plugin/evm/vmtest"
	"github.com/ava-labs/coreth/utils"
	"github.com/ava-labs/coreth/utils/utilstest"

	avalancheatomic "github.com/ava-labs/avalanchego/chains/atomic"
	agoUtils "github.com/ava-labs/avalanchego/utils"
)

func TestAtomicTxGossip(t *testing.T) {
	require := require.New(t)
	ctx, cancel := utilstest.NewTestContext(t)
	defer cancel()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.AVAXAssetID = ids.GenerateTestID()
	validatorState := utils.NewTestValidatorState()
	snowCtx.ValidatorState = validatorState
	memory := avalancheatomic.NewMemory(memdb.New())
	snowCtx.SharedMemory = memory.NewSharedMemory(snowCtx.ChainID)

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := pk.EthAddress()
	genesis := vmtest.NewPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	responseSender := &enginetest.SenderStub{
		SentAppResponse: make(chan []byte, 1),
	}
	vm := newAtomicTestVM()

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		nil,
		responseSender,
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	// sender for the peer requesting gossip from [vm]
	peerSender := &enginetest.SenderStub{
		SentAppRequest: make(chan []byte, 1),
	}
	validatorSet := p2p.NewValidators(
		logging.NoLog{},
		snowCtx.SubnetID,
		validatorState,
		0,
	)
	network, err := p2p.NewNetwork(
		logging.NoLog{},
		peerSender,
		prometheus.NewRegistry(),
		"",
		validatorSet,
	)
	require.NoError(err)
	client := network.NewClient(p2p.AtomicTxGossipHandlerID, validatorSet)

	// we only accept gossip requests from validators
	requestingNodeID := ids.GenerateTestNodeID()
	require.NoError(vm.Connected(ctx, requestingNodeID, nil))
	validatorState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return 0, nil
	}
	validatorState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{
			requestingNodeID: {
				NodeID: requestingNodeID,
				Weight: 1,
			},
		}, nil
	}

	// Ask the VM for any new transactions. We should get nothing at first.
	emptyBloomFilter, err := gossip.NewBloomFilter(
		prometheus.NewRegistry(),
		"",
		config.TxGossipBloomMinTargetElements,
		config.TxGossipBloomTargetFalsePositiveRate,
		config.TxGossipBloomResetFalsePositiveRate,
	)
	require.NoError(err)
	emptyBloomFilterBytes, _ := emptyBloomFilter.Marshal()
	request := &sdk.PullGossipRequest{
		Filter: emptyBloomFilterBytes,
		Salt:   agoUtils.RandomBytes(32),
	}

	requestBytes, err := proto.Marshal(request)
	require.NoError(err)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Empty(response.Gossip)
		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.Ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 1, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 1, <-responseSender.SentAppResponse))
	utilstest.WaitGroupWithContext(t, ctx, wg)

	// Issue a tx to the VM
	utxo, err := addUTXO(
		memory,
		snowCtx,
		ids.GenerateTestID(),
		0,
		snowCtx.AVAXAssetID,
		100_000_000_000,
		pk.Address(),
	)
	require.NoError(err)
	tx, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, address, vmtest.InitialBaseFee, secp256k1fx.NewKeychain(pk), []*avax.UTXO{utxo})
	require.NoError(err)
	require.NoError(vm.AtomicMempool.AddLocalTx(tx))

	// wait so we aren't throttled by the vm
	utilstest.SleepWithContext(ctx, 5*time.Second)

	// Ask the VM for new transactions. We should get the newly issued tx.
	wg.Add(1)

	marshaller := atomic.TxMarshaller{}
	onResponse = func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Len(response.Gossip, 1)

		gotTx, err := marshaller.UnmarshalGossip(response.Gossip[0])
		require.NoError(err)
		require.Equal(tx.ID(), gotTx.GossipID())

		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.Ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 3, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 3, <-responseSender.SentAppResponse))
	utilstest.WaitGroupWithContext(t, ctx, wg)
}

// Tests that a tx is gossiped when it is issued
func TestAtomicTxPushGossipOutbound(t *testing.T) {
	require := require.New(t)
	ctx, cancel := utilstest.NewTestContext(t)
	defer cancel()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.AVAXAssetID = ids.GenerateTestID()
	validatorState := utils.NewTestValidatorState()
	snowCtx.ValidatorState = validatorState
	memory := avalancheatomic.NewMemory(memdb.New())
	snowCtx.SharedMemory = memory.NewSharedMemory(snowCtx.ChainID)

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := pk.EthAddress()
	genesis := vmtest.NewPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	sender := &enginetest.SenderStub{
		SentAppGossip: make(chan []byte, 1),
	}
	vm := newAtomicTestVM()

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		nil,
		sender,
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	// Issue a tx to the VM
	utxo, err := addUTXO(
		memory,
		snowCtx,
		ids.GenerateTestID(),
		0,
		snowCtx.AVAXAssetID,
		100_000_000_000,
		pk.Address(),
	)
	require.NoError(err)
	tx, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, address, vmtest.InitialBaseFee, secp256k1fx.NewKeychain(pk), []*avax.UTXO{utxo})
	require.NoError(err)
	require.NoError(vm.AtomicMempool.AddLocalTx(tx))
	vm.AtomicTxPushGossiper.Add(tx)

	gossipedBytes := <-sender.SentAppGossip
	require.Equal(byte(p2p.AtomicTxGossipHandlerID), gossipedBytes[0])

	outboundGossipMsg := &sdk.PushGossip{}
	require.NoError(proto.Unmarshal(gossipedBytes[1:], outboundGossipMsg))
	require.Len(outboundGossipMsg.Gossip, 1)

	marshaller := atomic.TxMarshaller{}
	gossipedTx, err := marshaller.UnmarshalGossip(outboundGossipMsg.Gossip[0])
	require.NoError(err)
	require.Equal(tx.ID(), gossipedTx.ID())
}

// Tests that a tx is gossiped when it is issued
func TestAtomicTxPushGossipInbound(t *testing.T) {
	require := require.New(t)
	ctx, cancel := utilstest.NewTestContext(t)
	defer cancel()

	snowCtx := snowtest.Context(t, snowtest.CChainID)
	snowCtx.AVAXAssetID = ids.GenerateTestID()
	validatorState := utils.NewTestValidatorState()
	snowCtx.ValidatorState = validatorState
	memory := avalancheatomic.NewMemory(memdb.New())
	snowCtx.SharedMemory = memory.NewSharedMemory(snowCtx.ChainID)

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := pk.EthAddress()
	genesis := vmtest.NewPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	sender := &enginetest.Sender{}
	vm := newAtomicTestVM()

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		nil,
		sender,
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	// issue a tx to the vm
	utxo, err := addUTXO(
		memory,
		snowCtx,
		ids.GenerateTestID(),
		0,
		snowCtx.AVAXAssetID,
		100_000_000_000,
		pk.Address(),
	)
	require.NoError(err)
	tx, err := atomic.NewImportTx(vm.Ctx, vm.CurrentRules(), vm.clock.Unix(), vm.Ctx.XChainID, address, vmtest.InitialBaseFee, secp256k1fx.NewKeychain(pk), []*avax.UTXO{utxo})
	require.NoError(err)
	require.NoError(vm.AtomicMempool.AddLocalTx(tx))

	marshaller := atomic.TxMarshaller{}
	gossipBytes, err := marshaller.MarshalGossip(tx)
	require.NoError(err)

	inboundGossip := &sdk.PushGossip{
		Gossip: [][]byte{gossipBytes},
	}
	inboundGossipBytes, err := proto.Marshal(inboundGossip)
	require.NoError(err)

	inboundGossipMsg := append(binary.AppendUvarint(nil, p2p.AtomicTxGossipHandlerID), inboundGossipBytes...)

	require.NoError(vm.AppGossip(ctx, ids.EmptyNodeID, inboundGossipMsg))
	require.True(vm.AtomicMempool.Has(tx.ID()))
}

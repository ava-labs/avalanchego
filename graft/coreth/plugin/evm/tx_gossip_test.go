// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/binary"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/chains/atomic"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

func TestEthTxGossip(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	snowCtx := snow.DefaultContextTest()
	validatorState := &validators.TestState{}
	snowCtx.ValidatorState = validatorState

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := GetEthAddress(pk)
	genesis := newPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	responseSender := &common.FakeSender{
		SentAppResponse: make(chan []byte, 1),
	}
	vm := &VM{
		p2pSender:             responseSender,
		atomicTxGossipHandler: &p2p.NoOpHandler{},
		atomicTxPullGossiper:  &gossip.NoOpGossiper{},
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		make(chan common.Message),
		nil,
		&common.SenderTest{},
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	// sender for the peer requesting gossip from [vm]
	peerSender := &common.FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}

	network, err := p2p.NewNetwork(logging.NoLog{}, peerSender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(ethTxGossipProtocol)

	// we only accept gossip requests from validators
	requestingNodeID := ids.GenerateTestNodeID()
	require.NoError(vm.Network.Connected(ctx, requestingNodeID, nil))
	validatorState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return 0, nil
	}
	validatorState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{requestingNodeID: nil}, nil
	}

	// Ask the VM for any new transactions. We should get nothing at first.
	emptyBloomFilter, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	require.NoError(err)
	emptyBloomFilterBytes, err := emptyBloomFilter.Bloom.MarshalBinary()
	require.NoError(err)
	request := &sdk.PullGossipRequest{
		Filter: emptyBloomFilterBytes,
		Salt:   utils.RandomBytes(32),
	}

	requestBytes, err := proto.Marshal(request)
	require.NoError(err)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Empty(response.Gossip)
		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 1, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 1, <-responseSender.SentAppResponse))
	wg.Wait()

	// Issue a tx to the VM
	tx := types.NewTransaction(0, address, big.NewInt(10), 100_000, big.NewInt(params.LaunchMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), pk.ToECDSA())
	require.NoError(err)

	errs := vm.txPool.AddLocals([]*types.Transaction{signedTx})
	require.Len(errs, 1)
	require.Nil(errs[0])

	// wait so we aren't throttled by the vm
	time.Sleep(5 * time.Second)

	// Ask the VM for new transactions. We should get the newly issued tx.
	wg.Add(1)
	onResponse = func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Len(response.Gossip, 1)

		gotTx := &GossipEthTx{}
		require.NoError(gotTx.Unmarshal(response.Gossip[0]))
		require.Equal(signedTx.Hash(), gotTx.Tx.Hash())

		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 3, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 3, <-responseSender.SentAppResponse))
	wg.Wait()
}

func TestAtomicTxGossip(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	snowCtx := snow.DefaultContextTest()
	snowCtx.AVAXAssetID = ids.GenerateTestID()
	snowCtx.XChainID = ids.GenerateTestID()
	validatorState := &validators.TestState{
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return ids.Empty, nil
		},
	}
	snowCtx.ValidatorState = validatorState
	memory := atomic.NewMemory(memdb.New())
	snowCtx.SharedMemory = memory.NewSharedMemory(ids.Empty)

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := GetEthAddress(pk)
	genesis := newPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	responseSender := &common.FakeSender{
		SentAppResponse: make(chan []byte, 1),
	}
	vm := &VM{
		p2pSender:          responseSender,
		ethTxGossipHandler: &p2p.NoOpHandler{},
		ethTxPullGossiper:  &gossip.NoOpGossiper{},
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		make(chan common.Message),
		nil,
		&common.SenderTest{},
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	// sender for the peer requesting gossip from [vm]
	peerSender := &common.FakeSender{
		SentAppRequest: make(chan []byte, 1),
	}
	network, err := p2p.NewNetwork(logging.NoLog{}, peerSender, prometheus.NewRegistry(), "")
	require.NoError(err)
	client := network.NewClient(atomicTxGossipProtocol)

	// we only accept gossip requests from validators
	requestingNodeID := ids.GenerateTestNodeID()
	require.NoError(vm.Network.Connected(ctx, requestingNodeID, nil))
	validatorState.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return 0, nil
	}
	validatorState.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{requestingNodeID: nil}, nil
	}

	// Ask the VM for any new transactions. We should get nothing at first.
	emptyBloomFilter, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	require.NoError(err)
	emptyBloomFilterBytes, err := emptyBloomFilter.Bloom.MarshalBinary()
	require.NoError(err)
	request := &sdk.PullGossipRequest{
		Filter: emptyBloomFilterBytes,
		Salt:   utils.RandomBytes(32),
	}

	requestBytes, err := proto.Marshal(request)
	require.NoError(err)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Empty(response.Gossip)
		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 1, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 1, <-responseSender.SentAppResponse))
	wg.Wait()

	// Issue a tx to the VM
	utxo, err := addUTXO(
		memory,
		snowCtx,
		ids.GenerateTestID(),
		0,
		snowCtx.AVAXAssetID,
		100_000_000_000,
		pk.PublicKey().Address(),
	)
	require.NoError(err)
	tx, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, address, initialBaseFee, secp256k1fx.NewKeychain(pk), []*avax.UTXO{utxo})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(tx))

	// wait so we aren't throttled by the vm
	time.Sleep(5 * time.Second)

	// Ask the VM for new transactions. We should get the newly issued tx.
	wg.Add(1)
	onResponse = func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Len(response.Gossip, 1)

		gotTx := &GossipAtomicTx{}
		require.NoError(gotTx.Unmarshal(response.Gossip[0]))
		require.Equal(tx.ID(), gotTx.GetID())

		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 3, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 3, <-responseSender.SentAppResponse))
	wg.Wait()
}

// Tests that a tx is gossiped when it is issued
func TestEthTxPushGossipOutbound(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	snowCtx := snow.DefaultContextTest()
	sender := &common.FakeSender{
		SentAppGossip: make(chan []byte, 1),
	}

	vm := &VM{
		p2pSender:            sender,
		ethTxPullGossiper:    gossip.NoOpGossiper{},
		atomicTxPullGossiper: gossip.NoOpGossiper{},
	}

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := GetEthAddress(pk)
	genesis := newPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		make(chan common.Message),
		nil,
		&common.FakeSender{},
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	tx := types.NewTransaction(0, address, big.NewInt(10), 100_000, big.NewInt(params.LaunchMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), pk.ToECDSA())
	require.NoError(err)

	// issue a tx
	require.NoError(vm.txPool.AddLocal(signedTx))

	sent := <-sender.SentAppGossip
	got := &sdk.PushGossip{}

	// we should get a message that has the protocol prefix and the gossip
	// message
	require.Equal(byte(ethTxGossipProtocol), sent[0])
	require.NoError(proto.Unmarshal(sent[1:], got))

	gossipedTx := &GossipEthTx{}
	require.Len(got.Gossip, 1)
	require.NoError(gossipedTx.Unmarshal(got.Gossip[0]))
	require.Equal(ids.ID(signedTx.Hash()), gossipedTx.GetID())
}

// Tests that a gossiped tx is added to the mempool and forwarded
func TestEthTxPushGossipInbound(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	snowCtx := snow.DefaultContextTest()

	sender := &common.FakeSender{
		SentAppGossip: make(chan []byte, 1),
	}
	vm := &VM{
		p2pSender:            sender,
		ethTxPullGossiper:    gossip.NoOpGossiper{},
		atomicTxPullGossiper: gossip.NoOpGossiper{},
	}

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := GetEthAddress(pk)
	genesis := newPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		make(chan common.Message),
		nil,
		&common.FakeSender{},
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	tx := types.NewTransaction(0, address, big.NewInt(10), 100_000, big.NewInt(params.LaunchMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), pk.ToECDSA())
	require.NoError(err)

	gossipedTx := &GossipEthTx{
		Tx: signedTx,
	}
	gossipedTxBytes, err := gossipedTx.Marshal()
	require.NoError(err)

	inboundGossip := &sdk.PushGossip{
		Gossip: [][]byte{gossipedTxBytes},
	}

	inboundGossipBytes, err := proto.Marshal(inboundGossip)
	require.NoError(err)

	inboundGossipMsg := append(binary.AppendUvarint(nil, ethTxGossipProtocol), inboundGossipBytes...)
	require.NoError(vm.AppGossip(ctx, ids.EmptyNodeID, inboundGossipMsg))

	forwardedMsg := &sdk.PushGossip{}
	outboundGossipBytes := <-sender.SentAppGossip

	require.Equal(byte(ethTxGossipProtocol), outboundGossipBytes[0])
	require.NoError(proto.Unmarshal(outboundGossipBytes[1:], forwardedMsg))
	require.Len(forwardedMsg.Gossip, 1)

	forwardedTx := &GossipEthTx{}
	require.NoError(forwardedTx.Unmarshal(forwardedMsg.Gossip[0]))
	require.Equal(gossipedTx.GetID(), forwardedTx.GetID())
	require.True(vm.txPool.Has(signedTx.Hash()))
}

// Tests that a tx is gossiped when it is issued
func TestAtomicTxPushGossipOutbound(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	snowCtx := snow.DefaultContextTest()
	snowCtx.AVAXAssetID = ids.GenerateTestID()
	snowCtx.XChainID = ids.GenerateTestID()
	validatorState := &validators.TestState{
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return ids.Empty, nil
		},
	}
	snowCtx.ValidatorState = validatorState
	memory := atomic.NewMemory(memdb.New())
	snowCtx.SharedMemory = memory.NewSharedMemory(ids.Empty)

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := GetEthAddress(pk)
	genesis := newPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	sender := &common.FakeSender{
		SentAppGossip: make(chan []byte, 1),
	}
	vm := &VM{
		p2pSender:            sender,
		ethTxPullGossiper:    gossip.NoOpGossiper{},
		atomicTxPullGossiper: gossip.NoOpGossiper{},
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		make(chan common.Message),
		nil,
		&common.FakeSender{},
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	// Issue a tx to the VM
	utxo, err := addUTXO(
		memory,
		snowCtx,
		ids.GenerateTestID(),
		0,
		snowCtx.AVAXAssetID,
		100_000_000_000,
		pk.PublicKey().Address(),
	)
	require.NoError(err)
	tx, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, address, initialBaseFee, secp256k1fx.NewKeychain(pk), []*avax.UTXO{utxo})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(tx))

	gossipedBytes := <-sender.SentAppGossip
	require.Equal(byte(atomicTxGossipProtocol), gossipedBytes[0])

	outboundGossipMsg := &sdk.PushGossip{}
	require.NoError(proto.Unmarshal(gossipedBytes[1:], outboundGossipMsg))
	require.Len(outboundGossipMsg.Gossip, 1)

	gossipedTx := &GossipAtomicTx{}
	require.NoError(gossipedTx.Unmarshal(outboundGossipMsg.Gossip[0]))
	require.Equal(tx.ID(), gossipedTx.Tx.ID())
}

// Tests that a tx is gossiped when it is issued
func TestAtomicTxPushGossipInbound(t *testing.T) {
	require := require.New(t)
	ctx := context.Background()
	snowCtx := snow.DefaultContextTest()
	snowCtx.AVAXAssetID = ids.GenerateTestID()
	snowCtx.XChainID = ids.GenerateTestID()
	validatorState := &validators.TestState{
		GetSubnetIDF: func(context.Context, ids.ID) (ids.ID, error) {
			return ids.Empty, nil
		},
	}
	snowCtx.ValidatorState = validatorState
	memory := atomic.NewMemory(memdb.New())
	snowCtx.SharedMemory = memory.NewSharedMemory(ids.Empty)

	pk, err := secp256k1.NewPrivateKey()
	require.NoError(err)
	address := GetEthAddress(pk)
	genesis := newPrefundedGenesis(100_000_000_000_000_000, address)
	genesisBytes, err := genesis.MarshalJSON()
	require.NoError(err)

	sender := &common.FakeSender{
		SentAppGossip: make(chan []byte, 1),
	}
	vm := &VM{
		p2pSender:            sender,
		ethTxPullGossiper:    gossip.NoOpGossiper{},
		atomicTxPullGossiper: gossip.NoOpGossiper{},
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		genesisBytes,
		nil,
		nil,
		make(chan common.Message),
		nil,
		&common.FakeSender{},
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	// issue a tx to the vm
	utxo, err := addUTXO(
		memory,
		snowCtx,
		ids.GenerateTestID(),
		0,
		snowCtx.AVAXAssetID,
		100_000_000_000,
		pk.PublicKey().Address(),
	)
	require.NoError(err)
	tx, err := vm.newImportTxWithUTXOs(vm.ctx.XChainID, address, initialBaseFee, secp256k1fx.NewKeychain(pk), []*avax.UTXO{utxo})
	require.NoError(err)
	require.NoError(vm.mempool.AddLocalTx(tx))

	gossipedTx := &GossipAtomicTx{
		Tx: tx,
	}
	gossipBytes, err := gossipedTx.Marshal()
	require.NoError(err)

	inboundGossip := &sdk.PushGossip{
		Gossip: [][]byte{gossipBytes},
	}
	inboundGossipBytes, err := proto.Marshal(inboundGossip)
	require.NoError(err)

	inboundGossipMsg := append(binary.AppendUvarint(nil, atomicTxGossipProtocol), inboundGossipBytes...)

	require.NoError(vm.AppGossip(ctx, ids.EmptyNodeID, inboundGossipMsg))

	forwardedBytes := <-sender.SentAppGossip
	require.Equal(byte(atomicTxGossipProtocol), forwardedBytes[0])

	forwardedGossipMsg := &sdk.PushGossip{}
	require.NoError(proto.Unmarshal(forwardedBytes[1:], forwardedGossipMsg))
	require.Len(forwardedGossipMsg.Gossip, 1)

	forwardedTx := &GossipAtomicTx{}
	require.NoError(forwardedTx.Unmarshal(forwardedGossipMsg.Gossip[0]))
	require.Equal(tx.ID(), forwardedTx.Tx.ID())
	require.True(vm.mempool.has(tx.ID()))
}

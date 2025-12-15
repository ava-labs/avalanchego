// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"encoding/binary"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/params/paramstest"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/utils/utilstest"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/upgrade/upgradetest"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"

	agoUtils "github.com/ava-labs/avalanchego/utils"
)

func TestEthTxGossip(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()
	validatorState := utilstest.NewTestValidatorState()
	snowCtx := utilstest.NewTestSnowContextWithValidatorState(t, validatorState)

	responseSender := &enginetest.SenderStub{
		SentAppResponse: make(chan []byte, 1),
	}
	vm := &VM{}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest])),
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
		snowCtx.ValidatorState,
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
	client := network.NewClient(p2p.TxGossipHandlerID, validatorSet)

	// we only accept gossip requests from validators
	requestingNodeID := ids.GenerateTestNodeID()
	require.NoError(vm.Network.Connected(ctx, requestingNodeID, nil))
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
	requestBytes, err := gossip.MarshalAppRequest(
		bloom.EmptyFilter.Marshal(),
		agoUtils.RandomBytes(32),
	)
	require.NoError(err)

	wg := &sync.WaitGroup{}
	wg.Add(1)
	onResponse := func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response, err := gossip.ParseAppResponse(responseBytes)
		require.NoError(err)
		require.Empty(response)
		wg.Done()
	}
	require.NoError(client.AppRequest(ctx, set.Of(vm.ctx.NodeID), requestBytes, onResponse))
	require.NoError(vm.AppRequest(ctx, requestingNodeID, 1, time.Time{}, <-peerSender.SentAppRequest))
	require.NoError(network.AppResponse(ctx, snowCtx.NodeID, 1, <-responseSender.SentAppResponse))
	wg.Wait()

	// Issue a tx to the VM
	address := testEthAddrs[0]
	tx := types.NewTransaction(0, address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(err)

	errs := vm.txPool.Add([]*types.Transaction{signedTx}, true, true)
	require.Len(errs, 1)
	require.NoError(errs[0])

	// wait so we aren't throttled by the vm
	time.Sleep(5 * time.Second)

	// Ask the VM for new transactions. We should get the newly issued tx.
	wg.Add(1)
	onResponse = func(_ context.Context, _ ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response, err := gossip.ParseAppResponse(responseBytes)
		require.NoError(err)

		txBytes, err := signedTx.MarshalBinary()
		require.NoError(err)
		require.Equal(
			[][]byte{txBytes},
			response,
		)

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
	ctx := t.Context()
	snowCtx := utilstest.NewTestSnowContext(t)
	sender := &enginetest.SenderStub{
		SentAppGossip: make(chan []byte, 1),
	}

	vm := &VM{
		ethTxPullGossiper: noOpGossiper{},
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest])),
		nil,
		nil,
		nil,
		sender,
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	address := testEthAddrs[0]
	tx := types.NewTransaction(0, address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(err)

	// issue a tx
	require.NoError(vm.txPool.Add([]*types.Transaction{signedTx}, true, true)[0])
	vm.ethTxPushGossiper.Get().Add(&GossipEthTx{signedTx})

	sent := <-sender.SentAppGossip
	got := &sdk.PushGossip{}

	// we should get a message that has the protocol prefix and the gossip
	// message
	require.Equal(byte(p2p.TxGossipHandlerID), sent[0])
	require.NoError(proto.Unmarshal(sent[1:], got))

	marshaller := GossipEthTxMarshaller{}
	require.Len(got.Gossip, 1)
	gossipedTx, err := marshaller.UnmarshalGossip(got.Gossip[0])
	require.NoError(err)
	require.Equal(ids.ID(signedTx.Hash()), gossipedTx.GossipID())
}

// Tests that a gossiped tx is added to the mempool and forwarded
func TestEthTxPushGossipInbound(t *testing.T) {
	require := require.New(t)
	ctx := t.Context()
	snowCtx := utilstest.NewTestSnowContext(t)

	sender := &enginetest.Sender{}
	vm := &VM{
		ethTxPullGossiper: noOpGossiper{},
	}

	require.NoError(vm.Initialize(
		ctx,
		snowCtx,
		memdb.New(),
		[]byte(toGenesisJSON(paramstest.ForkToChainConfig[upgradetest.Latest])),
		nil,
		nil,
		nil,
		sender,
	))
	require.NoError(vm.SetState(ctx, snow.NormalOp))

	defer func() {
		require.NoError(vm.Shutdown(ctx))
	}()

	address := testEthAddrs[0]
	tx := types.NewTransaction(0, address, big.NewInt(10), 21000, big.NewInt(testMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainConfig.ChainID), testKeys[0].ToECDSA())
	require.NoError(err)

	marshaller := GossipEthTxMarshaller{}
	gossipedTx := &GossipEthTx{
		Tx: signedTx,
	}
	gossipedTxBytes, err := marshaller.MarshalGossip(gossipedTx)
	require.NoError(err)

	inboundGossip := &sdk.PushGossip{
		Gossip: [][]byte{gossipedTxBytes},
	}

	inboundGossipBytes, err := proto.Marshal(inboundGossip)
	require.NoError(err)

	inboundGossipMsg := append(binary.AppendUvarint(nil, p2p.TxGossipHandlerID), inboundGossipBytes...)
	require.NoError(vm.AppGossip(ctx, ids.EmptyNodeID, inboundGossipMsg))

	require.True(vm.txPool.Has(signedTx.Hash()))
}

var _ gossip.Gossiper = (*noOpGossiper)(nil)

type noOpGossiper struct{}

func (noOpGossiper) Gossip(context.Context) error {
	return nil
}

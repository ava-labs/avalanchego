// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/proto/pb/sdk"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"go.uber.org/mock/gomock"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
)

func TestEthTxGossip(t *testing.T) {
	require := require.New(t)

	// set up prefunded address
	importAmount := uint64(1_000_000_000)
	issuer, vm, _, _, sender := GenesisVMWithUTXOs(t, true, genesisJSONLatest, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})
	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	importAccepted := make(chan core.NewTxPoolHeadEvent)
	vm.txPool.SubscribeNewHeadEvent(importAccepted)

	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(err)
	require.NoError(vm.issueTx(importTx, true))
	<-issuer

	blk, err := vm.BuildBlock(context.Background())
	require.NoError(err)

	require.NoError(blk.Verify(context.Background()))
	require.NoError(vm.SetPreference(context.Background(), blk.ID()))
	require.NoError(blk.Accept(context.Background()))
	<-importAccepted

	// sender for the peer requesting gossip from [vm]
	ctrl := gomock.NewController(t)
	peerSender := common.NewMockSender(ctrl)
	router := p2p.NewRouter(logging.NoLog{}, peerSender, prometheus.NewRegistry(), "")

	// we're only making client requests, so we don't need a server handler
	client, err := router.RegisterAppProtocol(ethTxGossipProtocol, nil, nil)
	require.NoError(err)

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

	requestingNodeID := ids.GenerateTestNodeID()
	peerSender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) {
		go func() {
			require.NoError(vm.AppRequest(ctx, requestingNodeID, requestID, time.Time{}, appRequestBytes))
		}()
	}).AnyTimes()

	sender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
		go func() {
			require.NoError(router.AppResponse(ctx, nodeID, requestID, appResponseBytes))
		}()
		return nil
	}

	// we only accept gossip requests from validators
	mockValidatorSet, ok := vm.ctx.ValidatorState.(*validators.TestState)
	require.True(ok)
	mockValidatorSet.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return 0, nil
	}
	mockValidatorSet.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{requestingNodeID: nil}, nil
	}

	// Ask the VM for any new transactions. We should get nothing at first.
	wg.Add(1)
	onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Empty(response.Gossip)
		wg.Done()
	}
	require.NoError(client.AppRequest(context.Background(), set.Set[ids.NodeID]{vm.ctx.NodeID: struct{}{}}, requestBytes, onResponse))
	wg.Wait()

	// Issue a tx to the VM
	address := testEthAddrs[0]
	key := testKeys[0].ToECDSA()
	tx := types.NewTransaction(0, address, big.NewInt(10), 100_000, big.NewInt(params.LaunchMinGasPrice), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(vm.chainID), key)
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
	require.NoError(client.AppRequest(context.Background(), set.Set[ids.NodeID]{vm.ctx.NodeID: struct{}{}}, requestBytes, onResponse))
	wg.Wait()
}

func TestAtomicTxGossip(t *testing.T) {
	require := require.New(t)

	// set up prefunded address
	importAmount := uint64(1_000_000_000)
	issuer, vm, _, _, sender := GenesisVMWithUTXOs(t, true, genesisJSONApricotPhase0, "", "", map[ids.ShortID]uint64{
		testShortIDAddrs[0]: importAmount,
	})

	defer func() {
		require.NoError(vm.Shutdown(context.Background()))
	}()

	// sender for the peer requesting gossip from [vm]
	ctrl := gomock.NewController(t)
	peerSender := common.NewMockSender(ctrl)
	router := p2p.NewRouter(logging.NoLog{}, peerSender, prometheus.NewRegistry(), "")

	// we're only making client requests, so we don't need a server handler
	client, err := router.RegisterAppProtocol(atomicTxGossipProtocol, nil, nil)
	require.NoError(err)

	emptyBloomFilter, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	require.NoError(err)
	bloomBytes, err := emptyBloomFilter.Bloom.MarshalBinary()
	require.NoError(err)
	request := &sdk.PullGossipRequest{
		Filter: bloomBytes,
		Salt:   emptyBloomFilter.Salt[:],
	}
	requestBytes, err := proto.Marshal(request)
	require.NoError(err)

	requestingNodeID := ids.GenerateTestNodeID()
	wg := &sync.WaitGroup{}
	peerSender.EXPECT().SendAppRequest(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Do(func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, appRequestBytes []byte) {
		go func() {
			require.NoError(vm.AppRequest(ctx, requestingNodeID, requestID, time.Time{}, appRequestBytes))
		}()
	}).AnyTimes()

	sender.SendAppResponseF = func(ctx context.Context, nodeID ids.NodeID, requestID uint32, appResponseBytes []byte) error {
		go func() {
			require.NoError(router.AppResponse(ctx, nodeID, requestID, appResponseBytes))
		}()
		return nil
	}

	// we only accept gossip requests from validators
	mockValidatorSet, ok := vm.ctx.ValidatorState.(*validators.TestState)
	require.True(ok)
	mockValidatorSet.GetCurrentHeightF = func(context.Context) (uint64, error) {
		return 0, nil
	}
	mockValidatorSet.GetValidatorSetF = func(context.Context, uint64, ids.ID) (map[ids.NodeID]*validators.GetValidatorOutput, error) {
		return map[ids.NodeID]*validators.GetValidatorOutput{requestingNodeID: nil}, nil
	}

	// Ask the VM for any new transactions. We should get nothing at first.
	wg.Add(1)
	onResponse := func(_ context.Context, nodeID ids.NodeID, responseBytes []byte, err error) {
		require.NoError(err)

		response := &sdk.PullGossipResponse{}
		require.NoError(proto.Unmarshal(responseBytes, response))
		require.Empty(response.Gossip)
		wg.Done()
	}
	require.NoError(client.AppRequest(context.Background(), set.Set[ids.NodeID]{vm.ctx.NodeID: struct{}{}}, requestBytes, onResponse))
	wg.Wait()

	// issue a new tx to the vm
	importTx, err := vm.newImportTx(vm.ctx.XChainID, testEthAddrs[0], initialBaseFee, []*secp256k1.PrivateKey{testKeys[0]})
	require.NoError(err)

	require.NoError(vm.issueTx(importTx, true /*=local*/))
	<-issuer

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
		require.Equal(importTx.InputUTXOs(), gotTx.Tx.InputUTXOs())

		wg.Done()
	}
	require.NoError(client.AppRequest(context.Background(), set.Set[ids.NodeID]{vm.ctx.NodeID: struct{}{}}, requestBytes, onResponse))
	wg.Wait()
}

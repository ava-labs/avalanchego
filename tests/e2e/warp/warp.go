// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package e2e runs Ginkgo end-to-end tests for Avalanche warp messaging against
// a tmpnet Subnet-EVM chain and C-Chain (SA-EVM / coreth stack).
package warp

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/libevm/accounts/abi/bind"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "embed"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/graft/coreth/cmd/simulator/metrics"
	"github.com/ava-labs/avalanchego/graft/coreth/cmd/simulator/txs"
	"github.com/ava-labs/avalanchego/graft/coreth/precompile/contracts/warp"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/acp118"
	"github.com/ava-labs/avalanchego/network/peer"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/e2e/warp/testbinding"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/buffer"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/evm/predicate"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	simload "github.com/ava-labs/avalanchego/graft/coreth/cmd/simulator/load"
	p2pmessage "github.com/ava-labs/avalanchego/message"
	avasecp256k1 "github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	ethereum "github.com/ava-labs/libevm"
	ginkgo "github.com/onsi/ginkgo/v2"
)

const (
	warpSendMessageGas   uint64 = 200_000
	warpDeliverVerifyGas uint64 = 5_000_000
	txMiningWaitTimeout         = 30 * time.Second
	warpLoadTxsPerWorker uint64 = 10
	warpLoadBatchSize    uint64 = 10
)

var (
	warpTxGasFeeCap        = big.NewInt(225 * params.GWei)
	warpTxGasTipCap        = big.NewInt(params.GWei)
	warpLoadRequiredAmount = new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether))
	defaultWarpTestPayload = []byte{1, 2, 3}
)

// Subnet provides the basic details of a created subnet
type Subnet struct {
	// SubnetID is the txID of the transaction that created the subnet
	SubnetID ids.ID
	// For simplicity assume a single blockchain per subnet
	BlockchainID ids.ID
	// ChainID and Signer are the EVM chain id and latest signer; set by [newSubnet].
	ChainID *big.Int
	Signer  types.Signer
	// Validators: one tmpnet node plus JSON-RPC client per validator for this chain.
	// [warpTest] uses these nodes when opening ACP-118 peers for signature aggregation.
	Validators []*subnetValidator
}

// subnetValidator is one subnet validator: tmpnet node and JSON-RPC client for its chain.
type subnetValidator struct {
	node *tmpnet.Node

	client       e2e.E2EClient
	preFundedKey *ecdsa.PrivateKey
}

// newSubnet builds a [Subnet] for warp e2e: chain identity, genesis-funded key, and
// one [subnetValidator] per tmpnet node (RPC URI from [subnetValidator.node.URI]).
func newSubnet(
	tc *e2e.GinkgoTestContext,
	subnetID ids.ID,
	blockchainID ids.ID,
	keys []*avasecp256k1.PrivateKey,
	nodes []*tmpnet.Node,
) *Subnet {
	require := require.New(tc)
	ctx := tc.DefaultContext()
	s := &Subnet{
		SubnetID:     subnetID,
		BlockchainID: blockchainID,
		Validators:   make([]*subnetValidator, len(nodes)),
	}

	for i, n := range nodes {
		sp := strings.Split(n.GetAccessibleURI(), "//")
		require.Len(sp, 2)
		nodeAddress := sp[1]
		uri := fmt.Sprintf("ws://%s/ext/bc/%s/ws", nodeAddress, s.BlockchainID.String())
		tc.Log().Info("dialing eth client", zap.String("uri", uri))
		client, err := ethclient.Dial(uri)
		require.NoError(err)
		s.Validators[i] = &subnetValidator{node: n, preFundedKey: keys[i].ToECDSA(), client: e2e.NewE2EClient(client)}
	}

	client := s.Validators[0].client
	chainID, err := client.ChainID(ctx)
	require.NoError(err)
	s.ChainID = chainID
	s.Signer = types.LatestSignerForChainID(chainID)
	return s
}

// warpChainSuiteFixtures holds chain fixtures built from the shared tmpnet (after [e2e.InitSharedTestEnvironment]).
type warpChainSuiteFixtures struct {
	subnetA *Subnet
	subnetB *Subnet
	cChain  *Subnet
}

// newWarpChainSuiteFixtures dials subnet-A and C-Chain clients for this process. Caller must run after suite env init.
func newWarpChainSuiteFixtures(tc *e2e.GinkgoTestContext) *warpChainSuiteFixtures {
	require := require.New(tc)
	network := e2e.GetEnv(tc).GetNetwork()

	require.GreaterOrEqual(len(network.PreFundedKeys), len(network.Nodes), "pre-funded keys must be at least as many as the number of nodes")

	tmpnetSubnetA := network.GetSubnet(SubnetEVMNameA)
	require.NotNil(tmpnetSubnetA)
	chain := tmpnetSubnetA.Chains[0]
	subnetANodes := tmpnet.GetNodesForIDs(network.Nodes, tmpnetSubnetA.ValidatorIDs)
	subnetA := newSubnet(
		tc,
		tmpnetSubnetA.SubnetID,
		chain.ChainID,
		network.PreFundedKeys,
		subnetANodes,
	)

	tmpnetSubnetB := network.GetSubnet(SubnetEVMNameB)
	require.NotNil(tmpnetSubnetB)
	chainB := tmpnetSubnetB.Chains[0]
	subnetBNodes := tmpnet.GetNodesForIDs(network.Nodes, tmpnetSubnetB.ValidatorIDs)
	subnetB := newSubnet(
		tc,
		tmpnetSubnetB.SubnetID,
		chainB.ChainID,
		network.PreFundedKeys,
		subnetBNodes,
	)

	infoClient := info.NewClient(subnetA.Validators[0].node.URI)
	cChainBlockchainID, err := infoClient.GetBlockchainID(tc.DefaultContext(), "C")
	require.NoError(err)
	// C-Chain validators must match nodes we can dial: exclude ephemeral and stopped
	// nodes (same policy as tmpnet.GetNodeURIs / GetNodeWebsocketURIs).
	cChainNodes := tmpnet.FilterAvailableNodes(network.Nodes)
	require.NotEmpty(cChainNodes, "need at least one non-ephemeral running node for C-Chain warp")
	cChain := newSubnet(
		tc,
		constants.PrimaryNetworkID,
		cChainBlockchainID,
		network.PreFundedKeys,
		cChainNodes,
	)
	return &warpChainSuiteFixtures{subnetA: subnetA, subnetB: subnetB, cChain: cChain}
}

var _ = ginkgo.Describe("[Warp]", ginkgo.Ordered, ginkgo.Serial, ginkgo.Label("warp"), func() {
	tc := e2e.NewTestContext()

	var chains *warpChainSuiteFixtures
	ginkgo.BeforeAll(func() {
		network := e2e.GetEnv(tc).GetNetwork()
		if network.GetSubnet(SubnetEVMNameA) == nil || network.GetSubnet(SubnetEVMNameB) == nil {
			ginkgo.Skip("Subnet-EVM subnets are not on this network")
		}
		chains = newWarpChainSuiteFixtures(tc)
	})

	type testCombination struct {
		name            string
		labels          []string
		sendingSubnet   func() *Subnet
		receivingSubnet func() *Subnet
	}

	testCombinations := []testCombination{
		{
			"SubnetA -> C-Chain",
			[]string{"subnet-evm", "c"},
			func() *Subnet { return chains.subnetA }, func() *Subnet { return chains.cChain },
		},
		{
			"C-Chain -> SubnetA",
			[]string{"c", "subnet-evm"},
			func() *Subnet { return chains.cChain }, func() *Subnet { return chains.subnetA },
		},
		{
			"C-Chain -> C-Chain",
			[]string{"c"},
			func() *Subnet { return chains.cChain }, func() *Subnet { return chains.cChain },
		},
		{
			"SubnetA -> SubnetB",
			[]string{"subnet-evm"},
			func() *Subnet { return chains.subnetA }, func() *Subnet { return chains.subnetB },
		},
		{
			"SubnetA -> SubnetA",
			[]string{"subnet-evm"},
			func() *Subnet { return chains.subnetA }, func() *Subnet { return chains.subnetA },
		},
	}

	for _, combination := range testCombinations {
		ginkgo.Describe(combination.name, ginkgo.Label(combination.labels...), ginkgo.Ordered, func() {
			var w *warpTest

			ginkgo.BeforeAll(func() {
				w = newWarpTest(tc, combination.sendingSubnet(), combination.receivingSubnet())
			})

			ginkgo.AfterAll(func() {
				if w != nil {
					w.cleanupWarpTestPeers()
				}
			})

			ginkgo.It("should complete warp send, verify, deliver, and load scenario", func() {
				tc.By("sending warp message from sending subnet", func() {
					w.sendMessageFromSendingSubnet()
				})
				tc.By("aggregating warp signatures", func() {
					w.aggregateWarpSignatures()
				})
				tc.By("delivering addressed-call payload on receiving subnet", func() {
					w.deliverAddressedCallToReceivingSubnet()
				})
				tc.By("delivering block-hash payload on receiving subnet", func() {
					w.deliverBlockHashPayload()
				})
				tc.By("verifying warp bindings", func() {
					w.bindingsTest()
				})
				tc.By("running warp load test", func() {
					w.loadTest()
				})
			})
		})
	}
})

type warpPeer struct {
	peer     peer.Peer
	messages buffer.BlockingDeque[*p2pmessage.InboundMessage]
}

type warpTest struct {
	tc *e2e.GinkgoTestContext

	// network-wide fields set in the constructor
	networkID uint32

	sendingSubnet         *Subnet
	sendingWarpValidators validators.WarpSet

	receivingSubnet *Subnet

	// warpPeers holds ACP-118 test peers; [cleanupWarpTestPeers] closes them.
	warpPeers map[ids.NodeID]*warpPeer
	// stakingCancels from [tmpnet.Node.GetAccessibleStakingAddress]; run after peer close.
	stakingCancels []func()

	// aggregateP2PMu serializes [aggregateWarpSignaturesViaP2P]. Reusing one
	// [signatureAggregator] does not remove the need for this lock: each warpPeer
	// still has a single inbound deque, and concurrent AggregateSignatures would
	// still run multiple response waiters on the same deque (warp load parallel
	// tx generation).
	aggregateP2PMu sync.Mutex

	signatureAggregator *acp118.SignatureAggregator

	// Fields set throughout test execution
	blockID                   ids.ID
	blockPayloadSignedMessage *avalancheWarp.Message

	addressedCallUnsignedMessage *avalancheWarp.UnsignedMessage
	addressedCallSignedMessage   *avalancheWarp.Message
}

func newWarpTest(tc *e2e.GinkgoTestContext, sendingSubnet *Subnet, receivingSubnet *Subnet) *warpTest {
	require := require.New(tc)
	ctx := tc.DefaultContext()

	warpTest := &warpTest{
		tc:              tc,
		sendingSubnet:   sendingSubnet,
		receivingSubnet: receivingSubnet,
	}
	infoClient := info.NewClient(sendingSubnet.Validators[0].node.URI)
	networkID, err := infoClient.GetNetworkID(ctx)
	require.NoError(err)
	warpTest.networkID = networkID

	peers, stakingCancels := initWarpPeers(ctx, tc, sendingSubnet, networkID)
	warpTest.warpPeers = peers
	warpTest.stakingCancels = stakingCancels

	acp118Peers := make(map[ids.NodeID]WarpACP118Peer, len(peers))
	for id, wp := range peers {
		acp118Peers[id] = WarpACP118Peer{Peer: wp.peer, Messages: wp.messages}
	}
	agg, err := NewWarpACP118SignatureAggregator(tc.Log(), sendingSubnet.BlockchainID, acp118Peers)
	require.NoError(err)
	warpTest.signatureAggregator = agg

	receivingClient := warpTest.receivingSubnet.Validators[0].client
	// Issue transactions to activate ProposerVM on the receiving chain
	issueTxsToActivateProposerVMFork(tc, warpTest.receivingSubnet.ChainID, receivingSubnet.Validators[0].preFundedKey, receivingClient)
	return warpTest
}

func (w *warpTest) cleanupWarpTestPeers() {
	if w == nil || w.warpPeers == nil || w.tc == nil {
		return
	}
	require := require.New(w.tc)
	ctx := w.tc.DefaultContext()
	for _, wp := range w.warpPeers {
		wp.messages.Close()
		wp.peer.StartClose()
		require.NoError(wp.peer.AwaitClosed(ctx))
	}
	for i := len(w.stakingCancels) - 1; i >= 0; i-- {
		w.stakingCancels[i]()
	}
	w.warpPeers = nil
	w.stakingCancels = nil
}

func initWarpPeers(ctx context.Context, tc *e2e.GinkgoTestContext, s *Subnet, networkID uint32) (map[ids.NodeID]*warpPeer, []func()) {
	require := require.New(tc)

	vals := s.Validators
	require.NotEmpty(vals)

	peers := make(map[ids.NodeID]*warpPeer, len(vals))
	var stakingCancels []func()

	for i := range vals {
		node := vals[i].node
		messages := buffer.NewUnboundedBlockingDeque[*p2pmessage.InboundMessage](1)
		stakingAddress, cancel, err := node.GetAccessibleStakingAddress(ctx)
		require.NoError(err)
		stakingCancels = append(stakingCancels, cancel)

		p, err := peer.StartTestPeer(
			ctx,
			stakingAddress,
			networkID,
			router.InboundHandlerFunc(func(_ context.Context, m *p2pmessage.InboundMessage) {
				messages.PushRight(m)
			}),
		)
		require.NoError(err)

		peers[node.NodeID] = &warpPeer{
			peer:     p,
			messages: messages,
		}
	}
	return peers, stakingCancels
}

func (w *warpTest) sendMessageFromSendingSubnet() {
	require := require.New(w.tc)
	tc := w.tc
	ctx := tc.DefaultContext()

	var (
		client      = w.sendingSubnet.Validators[0].client
		signedTx    *types.Transaction
		receipt     *types.Receipt
		blockHash   common.Hash
		blockNumber uint64
		unsignedMsg *avalancheWarp.UnsignedMessage
	)

	tc.By("sending sendWarpMessage transaction", func() {
		preFundedKey := w.sendingSubnet.Validators[0].preFundedKey
		startingNonce, err := client.NonceAt(ctx, crypto.PubkeyToAddress(preFundedKey.PublicKey), nil)
		require.NoError(err)

		packedInput, err := warp.PackSendWarpMessage(defaultWarpTestPayload)
		require.NoError(err)
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   w.sendingSubnet.ChainID,
			Nonce:     startingNonce,
			To:        &warp.Module.Address,
			Gas:       warpSendMessageGas,
			GasFeeCap: warpTxGasFeeCap,
			GasTipCap: warpTxGasTipCap,
			Value:     common.Big0,
			Data:      packedInput,
		})
		var errSign error
		signedTx, errSign = types.SignTx(tx, w.sendingSubnet.Signer, preFundedKey)
		require.NoError(errSign)
		tc.Log().Info("sendWarpMessage tx", zap.String("txHash", signedTx.Hash().Hex()))
		require.NoError(client.SendTransaction(ctx, signedTx))
	})

	tc.By("waiting for sendWarpMessage transaction to be mined", func() {
		receiptCtx, cancel := context.WithTimeout(ctx, txMiningWaitTimeout)
		defer cancel()
		var err error
		receipt, err = bind.WaitMined(receiptCtx, client, signedTx)
		require.NoError(err)
		blockHash = receipt.BlockHash
		blockNumber = receipt.BlockNumber.Uint64()
	})

	w.blockID = ids.ID(blockHash)

	tc.By("fetching SendWarpMessage event and parsing unsigned warp message", func() {
		sender := crypto.PubkeyToAddress(w.sendingSubnet.Validators[0].preFundedKey.PublicKey)
		unsignedMsg = verifyAndExtractWarpMessage(tc, client, blockNumber, sender)
		w.addressedCallUnsignedMessage = unsignedMsg
		tc.Log().Info("parsed unsigned warp message",
			zap.Stringer("unsignedWarpMessageID", w.addressedCallUnsignedMessage.ID()),
		)
	})

	tc.By("waiting for all sending-subnet validators to accept the block", func() {
		for _, val := range w.sendingSubnet.Validators {
			vClient := val.client
			nodeID := val.node.NodeID
			w.tc.Eventually(func() bool {
				receivedBlkNum, err := vClient.BlockNumber(ctx)
				require.NoError(err)
				return receivedBlkNum >= blockNumber
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval,
				fmt.Sprintf("validator %s did not accept block height %d", nodeID, blockNumber))
			finalHeight, err := vClient.BlockNumber(ctx)
			require.NoError(err)
			tc.Log().Info("validator accepted block with SendWarpMessage",
				zap.Stringer("client", nodeID),
				zap.Uint64("height", finalHeight),
			)
		}
	})
}

func (w *warpTest) aggregateWarpSignatures() {
	require := require.New(w.tc)
	tc := w.tc
	ctx := tc.DefaultContext()

	tc.By("loading sending subnet warp validator set from P-Chain", func() {
		require.NoError(w.fetchSendingSubnetWarpValidators(ctx))
	})

	warpValidators := w.sendingWarpValidators

	tc.By("aggregating addressed-call warp signatures via P2P", func() {
		addressedCallSignedMsg, err := w.aggregateWarpSignaturesViaP2P(ctx, w.addressedCallUnsignedMessage)
		require.NoError(err)
		requireFullQuorumSignedWarpMessage(tc, addressedCallSignedMsg, w.networkID, warpValidators)
		w.addressedCallSignedMessage = addressedCallSignedMsg
	})

	tc.By("aggregating block-hash warp signatures via P2P", func() {
		blockHashPayload, err := payload.NewHash(w.blockID)
		require.NoError(err)
		unsignedBlockMsg, err := avalancheWarp.NewUnsignedMessage(
			w.networkID,
			w.sendingSubnet.BlockchainID,
			blockHashPayload.Bytes(),
		)
		require.NoError(err)
		blockSignedMsg, err := w.aggregateWarpSignaturesViaP2P(ctx, unsignedBlockMsg)
		require.NoError(err)
		requireFullQuorumSignedWarpMessage(tc, blockSignedMsg, w.networkID, warpValidators)
		w.blockPayloadSignedMessage = blockSignedMsg
	})
}

// fetchSendingSubnetWarpValidators loads the canonical warp validator set from
// the P-Chain (same subnet selection as coreth warp e2e).
func (w *warpTest) fetchSendingSubnetWarpValidators(ctx context.Context) error {
	pChainClient := platformvm.NewClient(w.sendingSubnet.Validators[0].node.URI)
	height, err := pChainClient.GetHeight(ctx)
	if err != nil {
		return fmt.Errorf("get P-Chain height: %w", err)
	}
	var vdrs map[ids.NodeID]*validators.GetValidatorOutput
	if w.sendingSubnet.SubnetID == constants.PrimaryNetworkID {
		vdrs, err = pChainClient.GetValidatorsAt(ctx, w.receivingSubnet.SubnetID, api.Height(height))
	} else {
		vdrs, err = pChainClient.GetValidatorsAt(ctx, w.sendingSubnet.SubnetID, api.Height(height))
	}
	if err != nil {
		return fmt.Errorf("get validators at: %w", err)
	}
	ws, err := validators.FlattenValidatorSet(vdrs)
	if err != nil {
		return fmt.Errorf("flatten validator set: %w", err)
	}
	w.sendingWarpValidators = ws
	return nil
}

// aggregateWarpSignaturesViaP2P aggregates warp signatures using
// [warpTest.signatureAggregator] (built in [newWarpTest] via the e2e ACP-118
// tmpnet bridge).
func (w *warpTest) aggregateWarpSignaturesViaP2P(
	ctx context.Context,
	unsignedMessage *avalancheWarp.UnsignedMessage,
) (*avalancheWarp.Message, error) {
	w.aggregateP2PMu.Lock()
	defer w.aggregateP2PMu.Unlock()

	if unsignedMessage.SourceChainID != w.sendingSubnet.BlockchainID {
		return nil, fmt.Errorf("unsigned message source chain %s != sending subnet blockchain %s",
			unsignedMessage.SourceChainID, w.sendingSubnet.BlockchainID)
	}
	msg := &avalancheWarp.Message{
		UnsignedMessage: *unsignedMessage,
		Signature:       &avalancheWarp.BitSetSignature{},
	}
	signedMsg, _, _, err := w.signatureAggregator.AggregateSignatures(
		ctx,
		msg,
		nil,
		w.sendingWarpValidators.Validators,
		warp.WarpQuorumDenominator,
		warp.WarpQuorumDenominator,
	)
	if err != nil {
		return nil, err
	}
	return signedMsg, nil
}

// requireFullQuorumSignedWarpMessage asserts the signature includes every warp validator
// and verifies full-quorum (WarpQuorumDenominator/WarpQuorumDenominator).
func requireFullQuorumSignedWarpMessage(
	tc *e2e.GinkgoTestContext,
	msg *avalancheWarp.Message,
	networkID uint32,
	warpValidators validators.WarpSet,
) {
	require := require.New(tc)
	numSigners, err := msg.Signature.NumSigners()
	require.NoError(err)
	require.Len(warpValidators.Validators, numSigners)
	require.NoError(msg.Signature.Verify(
		&msg.UnsignedMessage,
		networkID,
		warpValidators,
		warp.WarpQuorumDenominator,
		warp.WarpQuorumDenominator,
	))
}

// deliverVerifiedWarpToReceivingChain sends a getVerified* warp tx on the receiving subnet,
// waits for inclusion, and asserts an empty warp log in the receipt block.
// Scenario-specific labels (e.g. addressed-call vs block-hash) belong in the enclosing tc.By.
func (w *warpTest) deliverVerifiedWarpToReceivingChain(
	packCalldata func() ([]byte, error),
	signedWarpMessage []byte,
) {
	require := require.New(w.tc)
	tc := w.tc
	ctx := tc.DefaultContext()
	client := w.receivingSubnet.Validators[0].client

	var signedTx *types.Transaction
	var receipt *types.Receipt

	tc.By("sending getVerified warp transaction", func() {
		preFundedKey := w.receivingSubnet.Validators[0].preFundedKey
		nonce, err := client.NonceAt(ctx, crypto.PubkeyToAddress(preFundedKey.PublicKey), nil)
		require.NoError(err)

		packedInput, err := packCalldata()
		require.NoError(err)
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   w.receivingSubnet.ChainID,
			Nonce:     nonce,
			To:        &warp.Module.Address,
			Gas:       warpDeliverVerifyGas,
			GasFeeCap: warpTxGasFeeCap,
			GasTipCap: warpTxGasTipCap,
			Value:     common.Big0,
			Data:      packedInput,
			AccessList: types.AccessList{
				{
					Address:     warp.ContractAddress,
					StorageKeys: predicate.New(signedWarpMessage),
				},
			},
		})
		var errSign error
		signedTx, errSign = types.SignTx(tx, w.receivingSubnet.Signer, preFundedKey)
		require.NoError(errSign)
		txBytes, err := signedTx.MarshalBinary()
		require.NoError(err)
		tc.Log().Info("verified warp transaction",
			zap.String("txHash", signedTx.Hash().Hex()),
			zap.String("txBytes", common.Bytes2Hex(txBytes)),
		)
		require.NoError(client.SendTransaction(ctx, signedTx))
	})

	tc.By("waiting for transaction to be mined", func() {
		receiptCtx, cancel := context.WithTimeout(ctx, txMiningWaitTimeout)
		defer cancel()
		var err error
		receipt, err = bind.WaitMined(receiptCtx, client, signedTx)
		require.NoError(err)
	})

	tc.By("asserting successful delivery (no warp logs in receipt block)", func() {
		blockHash := receipt.BlockHash
		logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
			BlockHash: &blockHash,
			Addresses: []common.Address{warp.Module.Address},
		})
		require.NoError(err)
		require.Empty(logs)
		require.Equal(types.ReceiptStatusSuccessful, receipt.Status)
	})
}

func (w *warpTest) deliverAddressedCallToReceivingSubnet() {
	w.deliverVerifiedWarpToReceivingChain(
		func() ([]byte, error) { return warp.PackGetVerifiedWarpMessage(0) },
		w.addressedCallSignedMessage.Bytes(),
	)
}

func (w *warpTest) deliverBlockHashPayload() {
	w.deliverVerifiedWarpToReceivingChain(
		func() ([]byte, error) { return warp.PackGetVerifiedWarpBlockHash(0) },
		w.blockPayloadSignedMessage.Bytes(),
	)
}

func (w *warpTest) bindingsTest() {
	require := require.New(w.tc)
	tc := w.tc
	ctx := tc.DefaultContext()

	client := w.sendingSubnet.Validators[0].client

	tc.Log().Info("Deploying WarpTest proxy contract")
	auth, err := e2e.NewKeyedTxOpts(w.sendingSubnet.Validators[0].preFundedKey, w.sendingSubnet.ChainID, e2e.DefaultContractCallGasLimit)
	require.NoError(err)
	auth.Context = ctx

	proxyAddr, deployTx, warpTestContract, err := testbinding.DeployWarpTest(auth, client, warp.Module.Address)
	require.NoError(err)

	tc.Log().Info("Waiting for WarpTest deployment",
		zap.String("txHash", deployTx.Hash().Hex()),
		zap.String("proxyAddr", proxyAddr.Hex()),
	)
	deployReceipt, err := bind.WaitMined(ctx, client, deployTx)
	require.NoError(err)
	require.Equal(types.ReceiptStatusSuccessful, deployReceipt.Status)

	tc.Log().Info("Calling getBlockchainID via proxy contract")
	returnedBlockchainID, err := warpTestContract.GetBlockchainID(&bind.CallOpts{Context: ctx})
	require.NoError(err)
	require.Equal(w.sendingSubnet.BlockchainID, ids.ID(returnedBlockchainID))
	tc.Log().Info("getBlockchainID returned correct value",
		zap.Stringer("blockchainID", ids.ID(returnedBlockchainID)),
	)

	tc.Log().Info("Sending warp message via proxy contract",
		zap.String("payload", common.Bytes2Hex(defaultWarpTestPayload)),
	)

	sendTx, err := warpTestContract.SendWarpMessage(auth, defaultWarpTestPayload)
	require.NoError(err)

	tc.Log().Info("Waiting for sendWarpMessage transaction",
		zap.String("txHash", sendTx.Hash().Hex()),
	)
	sendReceipt, err := bind.WaitMined(ctx, client, sendTx)
	require.NoError(err)
	require.Equal(types.ReceiptStatusSuccessful, sendReceipt.Status)

	unsignedMsg := verifyAndExtractWarpMessage(tc, client, sendReceipt.BlockNumber.Uint64(), proxyAddr)

	addressedCall, err := payload.ParseAddressedCall(unsignedMsg.Payload)
	require.NoError(err)

	require.Equal(defaultWarpTestPayload, addressedCall.Payload, "payload mismatch in warp message")
	require.Equal(proxyAddr.Bytes(), addressedCall.SourceAddress, "source address should be proxy contract")

	tc.Log().Info("warp bindings test complete",
		zap.String("proxyAddr", proxyAddr.Hex()),
		zap.Stringer("blockchainID", ids.ID(returnedBlockchainID)),
	)
}

// verifyAndExtractWarpMessage queries the given block for SendWarpMessage events
// emitted by the specified sender using the IWarpMessengerFilterer binding. It
// asserts that exactly one such event exists and returns the parsed unsigned
// warp message.
func verifyAndExtractWarpMessage(
	tc *e2e.GinkgoTestContext,
	client bind.ContractFilterer,
	blockNumber uint64,
	sender common.Address,
) *avalancheWarp.UnsignedMessage {
	require := require.New(tc)
	ctx := tc.DefaultContext()

	tc.Log().Info("Filtering SendWarpMessage events using binding")
	warpFilterer, err := NewIWarpMessengerFilterer(warp.Module.Address, client)
	require.NoError(err)

	iter, err := warpFilterer.FilterSendWarpMessage(
		&bind.FilterOpts{
			Start:   blockNumber,
			End:     &blockNumber,
			Context: ctx,
		},
		[]common.Address{sender},
		nil, // messageID filter: any
	)
	require.NoError(err)
	defer iter.Close()

	require.True(iter.Next(), "expected a SendWarpMessage event")
	event := iter.Event

	tc.Log().Info("Found SendWarpMessage event",
		zap.String("sender", event.Sender.Hex()),
		zap.String("messageID", common.Bytes2Hex(event.MessageID[:])),
	)

	require.Equal(sender, event.Sender)

	unsignedMessage, err := avalancheWarp.ParseUnsignedMessage(event.Message)
	require.NoError(err)

	require.False(iter.Next(), "expected exactly one SendWarpMessage event")
	require.NoError(iter.Error())

	return unsignedMessage
}

// buildReceiptWorkers returns one sim load receipt worker per validator on each subnet.
func (w *warpTest) buildReceiptWorkers() (sendingWorkers, receivingWorkers []txs.Worker[*types.Transaction]) {
	for i := range w.sendingSubnet.Validators {
		sendingWorkers = append(sendingWorkers, simload.NewTxReceiptWorker(w.sendingSubnet.Validators[i].client))
	}
	for i := range w.receivingSubnet.Validators {
		receivingWorkers = append(receivingWorkers, simload.NewTxReceiptWorker(w.receivingSubnet.Validators[i].client))
	}
	return sendingWorkers, receivingWorkers
}

// subscribeSendLogs subscribes to SendWarpMessage logs on the sending chain; registers
// subscription cleanup on [tc].
func subscribeSendLogs(
	ctx context.Context,
	tc *e2e.GinkgoTestContext,
	sendingClient e2e.E2EClient,
	numWorkers int,
) chan types.Log {
	require := require.New(tc)
	logsCh := make(chan types.Log, numWorkers*int(warpLoadTxsPerWorker))
	sub, err := sendingClient.SubscribeFilterLogs(ctx, ethereum.FilterQuery{
		Addresses: []common.Address{warp.Module.Address},
	}, logsCh)
	require.NoError(err)
	tc.DeferCleanup(func() {
		sub.Unsubscribe()
		require.NoError(<-sub.Err())
	})
	return logsCh
}

// buildSendSequences builds signed sendWarpMessage txs per sending validator.
func (w *warpTest) buildSendSequences(ctx context.Context) []txs.TxSequence[*types.Transaction] {
	require := require.New(w.tc)
	sequences := make([]txs.TxSequence[*types.Transaction], len(w.sendingSubnet.Validators))
	for i, val := range w.sendingSubnet.Validators {
		key := val.preFundedKey
		startingNonce, err := val.client.PendingNonceAt(ctx, crypto.PubkeyToAddress(key.PublicKey))
		require.NoError(err)
		txsForKey := make([]*types.Transaction, 0, warpLoadTxsPerWorker)
		for j := uint64(0); j < warpLoadTxsPerWorker; j++ {
			nonce := startingNonce + j
			data, err := warp.PackSendWarpMessage(defaultWarpTestPayload)
			require.NoError(err)
			tx := types.NewTx(&types.DynamicFeeTx{
				ChainID:   w.sendingSubnet.ChainID,
				Nonce:     nonce,
				To:        &warp.Module.Address,
				Gas:       warpSendMessageGas,
				GasFeeCap: warpTxGasFeeCap,
				GasTipCap: warpTxGasTipCap,
				Value:     common.Big0,
				Data:      data,
			})
			signedTx, err := types.SignTx(tx, w.sendingSubnet.Signer, key)
			require.NoError(err)
			txsForKey = append(txsForKey, signedTx)
		}
		sequences[i] = txs.ConvertTxSliceToSequence(txsForKey)
	}
	return sequences
}

func executeLoader(
	ctx context.Context,
	tc *e2e.GinkgoTestContext,
	sendingWorkers []txs.Worker[*types.Transaction],
	warpSendSequences []txs.TxSequence[*types.Transaction],
	loadMetrics *metrics.Metrics,
) *simload.Loader[*types.Transaction] {
	require := require.New(tc)
	loader := simload.New(sendingWorkers, warpSendSequences, warpLoadBatchSize, loadMetrics)
	// TODO: execute send and receive loaders concurrently.
	require.NoError(loader.Execute(ctx))
	require.NoError(loader.ConfirmReachedTip(ctx))
	return loader
}

// aggregateSignedBytesFromLogs reads warp send logs, aggregates signatures via P2P for each.
func (w *warpTest) aggregateSignedBytesFromLogs(
	ctx context.Context,
	tc *e2e.GinkgoTestContext,
	logsCh chan types.Log,
	numWorkers int,
) [][]byte {
	require := require.New(w.tc)
	totalEvents := numWorkers * int(warpLoadTxsPerWorker)
	out := make([][]byte, totalEvents)
	for i := range totalEvents {
		warpLog := <-logsCh
		unsignedMessage, err := warp.UnpackSendWarpEventDataToMessage(warpLog.Data)
		require.NoError(err)
		tc.Log().Info("aggregating addressed call signature via P2P for load test")
		signedWarpMessage, err := w.aggregateWarpSignaturesViaP2P(ctx, unsignedMessage)
		require.NoError(err)
		out[i] = signedWarpMessage.Bytes()
	}
	return out
}

// buildDeliverSequences partitions signed warp payloads across receiving validators
// as getVerifiedWarpMessage txs.
func (w *warpTest) buildDeliverSequences(
	ctx context.Context,
	signedWarpPayloads [][]byte,
) []txs.TxSequence[*types.Transaction] {
	require := require.New(w.tc)
	rValidators := w.receivingSubnet.Validators
	totalEvents := len(signedWarpPayloads)

	deliverTxsPerVal := make([]int, len(rValidators))
	for k := 0; k < totalEvents; k++ {
		deliverTxsPerVal[k%len(rValidators)]++
	}

	sequences := make([]txs.TxSequence[*types.Transaction], len(rValidators))
	msgIdx := 0
	for i, val := range rValidators {
		key := val.preFundedKey
		startingNonce, err := val.client.PendingNonceAt(ctx, crypto.PubkeyToAddress(key.PublicKey))
		require.NoError(err)
		n := deliverTxsPerVal[i]
		txsForKey := make([]*types.Transaction, 0, n)
		for j := 0; j < n; j++ {
			packedInput, err := warp.PackGetVerifiedWarpMessage(0)
			require.NoError(err)
			tx := types.NewTx(&types.DynamicFeeTx{
				ChainID:   w.receivingSubnet.ChainID,
				Nonce:     startingNonce + uint64(j),
				To:        &warp.Module.Address,
				Gas:       warpDeliverVerifyGas,
				GasFeeCap: warpTxGasFeeCap,
				GasTipCap: warpTxGasTipCap,
				Value:     common.Big0,
				Data:      packedInput,
				AccessList: types.AccessList{
					{
						Address:     warp.ContractAddress,
						StorageKeys: predicate.New(signedWarpPayloads[msgIdx]),
					},
				},
			})
			msgIdx++
			signedTx, err := types.SignTx(tx, w.receivingSubnet.Signer, key)
			require.NoError(err)
			txsForKey = append(txsForKey, signedTx)
		}
		sequences[i] = txs.ConvertTxSliceToSequence(txsForKey)
	}
	require.Equal(totalEvents, msgIdx)
	return sequences
}

// TODO (ceyonur): move this directly to tests/loadTest or use framework from tests/loadTest and replace graft/coreth/cmd/*.
func (w *warpTest) loadTest() {
	tc := w.tc
	ctx := tc.DefaultContext()

	numWorkers := len(w.sendingSubnet.Validators)
	sendingClient := w.sendingSubnet.Validators[0].client
	loadMetrics := metrics.NewDefaultMetrics()

	tc.By("verifying generated worker keys are pre-funded on both subnets", func() {
		verifyPreFundedKeys(tc, w.sendingSubnet.Validators)
		verifyPreFundedKeys(tc, w.receivingSubnet.Validators)
	})

	var sendingWorkers, receivingWorkers []txs.Worker[*types.Transaction]
	tc.By("creating tx receipt workers for each subnet", func() {
		sendingWorkers, receivingWorkers = w.buildReceiptWorkers()
	})

	var logsCh chan types.Log
	tc.By("subscribing to warp send events on sending subnet", func() {
		logsCh = subscribeSendLogs(ctx, tc, sendingClient, numWorkers)
	})

	var warpSendSequences []txs.TxSequence[*types.Transaction]
	tc.By("generating tx sequence to send warp messages", func() {
		warpSendSequences = w.buildSendSequences(ctx)
	})

	var warpSendLoader *simload.Loader[*types.Transaction]
	tc.By("executing warp send loader", func() {
		warpSendLoader = executeLoader(ctx, tc, sendingWorkers, warpSendSequences, loadMetrics)
	})

	var warpDeliverSequences []txs.TxSequence[*types.Transaction]
	tc.By("generating warp delivery sequences", func() {
		signed := w.aggregateSignedBytesFromLogs(ctx, tc, logsCh, numWorkers)
		warpDeliverSequences = w.buildDeliverSequences(ctx, signed)
	})

	tc.By("executing warp delivery loader", func() {
		warpDeliverLoader := simload.New(receivingWorkers, warpDeliverSequences, warpLoadBatchSize, loadMetrics)
		require.NoError(tc, warpDeliverLoader.Execute(ctx))
		require.NoError(tc, warpSendLoader.ConfirmReachedTip(ctx))
		tc.Log().Info("completed warp delivery successfully")
	})
}

// IssueTxsToActivateProposerVMFork issues transactions at the current
// timestamp, which should be after the ProposerVM activation time (aka
// ApricotPhase4). This should generate a PostForkBlock because its parent block
// (genesis) has a timestamp (0) that is greater than or equal to the fork
// activation time of 0. Therefore, subsequent blocks should be built with
// BuildBlockWithContext.
func issueTxsToActivateProposerVMFork(
	tc *e2e.GinkgoTestContext, chainID *big.Int, fundedKey *ecdsa.PrivateKey,
	client e2e.E2EClient,
) {
	ctx := tc.DefaultContext()
	addr := crypto.PubkeyToAddress(fundedKey.PublicKey)
	nonce, err := client.NonceAt(ctx, addr, nil)
	require.NoError(tc, err)

	txSigner := types.LatestSignerForChainID(chainID)

	// expectedBlockHeight is the block height that activates the proposerVM fork.
	// We issue 2 txs (one per block) to reach block height 2.
	const expectedBlockHeight = 2

	// Send exactly 2 transactions, waiting for each to be included in a block
	for i := 0; i < expectedBlockHeight; i++ {
		tx := types.NewTransaction(
			nonce, addr, common.Big1, params.TxGas, warpTxGasFeeCap, nil)
		triggerTx, err := types.SignTx(tx, txSigner, fundedKey)
		require.NoError(tc, err)
		require.NoError(tc, client.SendTransaction(ctx, triggerTx))

		receiptCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		_, err = bind.WaitMined(receiptCtx, client, triggerTx)
		cancel()
		require.NoError(tc, err)
		nonce++
	}

	tc.Log().Info("Built sufficient blocks to activate proposerVM fork",
		zap.Int("blockHeight", expectedBlockHeight),
	)
}

func verifyPreFundedKeys(tc *e2e.GinkgoTestContext, validators []*subnetValidator) {
	ctx := tc.DefaultContext()
	require := require.New(tc)
	for _, val := range validators {
		client := val.client
		pk := val.preFundedKey
		addr := crypto.PubkeyToAddress(pk.PublicKey)
		balanceA, err := client.BalanceAt(ctx, addr, nil)
		require.NoError(err)
		require.GreaterOrEqual(balanceA.Cmp(warpLoadRequiredAmount), 0)
	}
}

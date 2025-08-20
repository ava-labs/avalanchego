// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements solidity tests.
package warp

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/api"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/crypto"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/accounts/abi/bind"
	"github.com/ava-labs/coreth/cmd/simulator/key"
	"github.com/ava-labs/coreth/cmd/simulator/load"
	"github.com/ava-labs/coreth/cmd/simulator/metrics"
	"github.com/ava-labs/coreth/cmd/simulator/txs"
	"github.com/ava-labs/coreth/ethclient"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/precompile/contracts/warp"
	"github.com/ava-labs/coreth/predicate"
	"github.com/ava-labs/coreth/tests/utils"
	"github.com/ava-labs/coreth/tests/warp/aggregator"

	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	warpBackend "github.com/ava-labs/coreth/warp"
	ethereum "github.com/ava-labs/libevm"
	ginkgo "github.com/onsi/ginkgo/v2"
)

var (
	flagVars *e2e.FlagVars

	cChainSubnetDetails *Subnet

	testPayload = []byte{1, 2, 3}
)

func init() {
	// Configures flags used to configure tmpnet (via SynchronizedBeforeSuite)
	flagVars = e2e.RegisterFlags()
}

// Subnet provides the basic details of a created subnet
type Subnet struct {
	// SubnetID is the txID of the transaction that created the subnet
	SubnetID ids.ID
	// For simplicity assume a single blockchain per subnet
	BlockchainID ids.ID
	// Key funded in the genesis of the blockchain
	PreFundedKey *ecdsa.PrivateKey
	// ValidatorURIs are the base URIs for each participant of the Subnet
	ValidatorURIs []string
}

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "coreth warp e2e test")
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	// Run only once in the first ginkgo process

	tc := e2e.NewTestContext()
	nodes := tmpnet.NewNodesOrPanic(tmpnet.DefaultNodeCount)

	env := e2e.NewTestEnvironment(
		tc,
		flagVars,
		utils.NewTmpnetNetwork(
			"coreth-warp-e2e",
			nodes,
			tmpnet.FlagsMap{},
		),
	)

	return env.Marshal()
}, func(envBytes []byte) {
	// Run in every ginkgo process

	require := require.New(ginkgo.GinkgoT())
	tc := e2e.NewTestContext()

	// Initialize the local test environment from the global state
	if len(envBytes) > 0 {
		e2e.InitSharedTestEnvironment(tc, envBytes)
	}

	network := e2e.GetEnv(tc).GetNetwork()

	// By default all nodes are validating all subnets
	validatorURIs := make([]string, len(network.Nodes))
	for i, node := range network.Nodes {
		validatorURIs[i] = node.URI
	}

	infoClient := info.NewClient(network.Nodes[0].URI)
	cChainBlockchainID, err := infoClient.GetBlockchainID(tc.DefaultContext(), "C")
	require.NoError(err)

	cChainSubnetDetails = &Subnet{
		SubnetID:      constants.PrimaryNetworkID,
		BlockchainID:  cChainBlockchainID,
		PreFundedKey:  tmpnet.HardhatKey.ToECDSA(),
		ValidatorURIs: validatorURIs,
	}
})

var _ = ginkgo.Describe("[Warp]", func() {
	testFunc := func(sendingSubnet *Subnet, receivingSubnet *Subnet) {
		tc := e2e.NewTestContext()
		w := newWarpTest(tc.DefaultContext(), sendingSubnet, receivingSubnet)

		ginkgo.GinkgoLogr.Info("Sending message from A to B")
		w.sendMessageFromSendingSubnet()

		ginkgo.GinkgoLogr.Info("Aggregating signatures via API")
		w.aggregateSignaturesViaAPI()

		ginkgo.GinkgoLogr.Info("Aggregating signatures via p2p aggregator")
		w.aggregateSignatures()

		ginkgo.GinkgoLogr.Info("Delivering addressed call payload to receiving subnet")
		w.deliverAddressedCallToReceivingSubnet()

		ginkgo.GinkgoLogr.Info("Delivering block hash payload to receiving subnet")
		w.deliverBlockHashPayload()

		ginkgo.GinkgoLogr.Info("Executing warp load test")
		w.warpLoad()
	}
	// TODO: Uncomment these tests when we have a way to run them in CI, currently we should not depend on Subnet-EVM
	// as Coreth and Subnet-EVM have different release cycles. The problem is that once we update AvalancheGo (protocol version),
	// we need to update Subnet-EVM to the same protocol version. Until then all Subnet-EVM tests are broken, so it's blocking Coreth development.
	// It's best to not run these tests until we have a way to run them in CI.
	// ginkgo.It("SubnetA -> C-Chain", func() { testFunc(subnetA, cChainSubnetDetails) })
	// ginkgo.It("C-Chain -> SubnetA", func() { testFunc(cChainSubnetDetails, subnetA) })
	ginkgo.It("C-Chain -> C-Chain", func() { testFunc(cChainSubnetDetails, cChainSubnetDetails) })
})

type warpTest struct {
	// network-wide fields set in the constructor
	networkID uint32

	// sendingSubnet fields set in the constructor
	sendingSubnet              *Subnet
	sendingSubnetURIs          []string
	sendingSubnetClients       []*ethclient.Client
	sendingSubnetFundedKey     *ecdsa.PrivateKey
	sendingSubnetFundedAddress common.Address
	sendingSubnetChainID       *big.Int
	sendingSubnetSigner        types.Signer

	// receivingSubnet fields set in the constructor
	receivingSubnet              *Subnet
	receivingSubnetClients       []*ethclient.Client
	receivingSubnetFundedKey     *ecdsa.PrivateKey
	receivingSubnetFundedAddress common.Address
	receivingSubnetChainID       *big.Int
	receivingSubnetSigner        types.Signer

	// Fields set throughout test execution
	blockID                     ids.ID
	blockPayload                *payload.Hash
	blockPayloadUnsignedMessage *avalancheWarp.UnsignedMessage
	blockPayloadSignedMessage   *avalancheWarp.Message

	addressedCallUnsignedMessage *avalancheWarp.UnsignedMessage
	addressedCallSignedMessage   *avalancheWarp.Message
}

func newWarpTest(ctx context.Context, sendingSubnet *Subnet, receivingSubnet *Subnet) *warpTest {
	require := require.New(ginkgo.GinkgoT())

	sendingSubnetFundedKey := sendingSubnet.PreFundedKey
	receivingSubnetFundedKey := receivingSubnet.PreFundedKey

	warpTest := &warpTest{
		sendingSubnet:                sendingSubnet,
		sendingSubnetURIs:            sendingSubnet.ValidatorURIs,
		receivingSubnet:              receivingSubnet,
		sendingSubnetFundedKey:       sendingSubnetFundedKey,
		sendingSubnetFundedAddress:   crypto.PubkeyToAddress(sendingSubnetFundedKey.PublicKey),
		receivingSubnetFundedKey:     receivingSubnetFundedKey,
		receivingSubnetFundedAddress: crypto.PubkeyToAddress(receivingSubnetFundedKey.PublicKey),
	}
	infoClient := info.NewClient(sendingSubnet.ValidatorURIs[0])
	networkID, err := infoClient.GetNetworkID(ctx)
	require.NoError(err)
	warpTest.networkID = networkID

	warpTest.initClients()

	sendingClient := warpTest.sendingSubnetClients[0]
	sendingSubnetChainID, err := sendingClient.ChainID(ctx)
	require.NoError(err)
	warpTest.sendingSubnetChainID = sendingSubnetChainID
	warpTest.sendingSubnetSigner = types.LatestSignerForChainID(sendingSubnetChainID)

	receivingClient := warpTest.receivingSubnetClients[0]
	receivingChainID, err := receivingClient.ChainID(ctx)
	require.NoError(err)
	// Issue transactions to activate ProposerVM on the receiving chain
	require.NoError(utils.IssueTxsToActivateProposerVMFork(ctx, receivingChainID, receivingSubnetFundedKey, receivingClient))
	warpTest.receivingSubnetChainID = receivingChainID
	warpTest.receivingSubnetSigner = types.LatestSignerForChainID(receivingChainID)

	return warpTest
}

func (w *warpTest) initClients() {
	require := require.New(ginkgo.GinkgoT())

	w.sendingSubnetClients = make([]*ethclient.Client, 0, len(w.sendingSubnetClients))
	for _, uri := range w.sendingSubnet.ValidatorURIs {
		wsURI := toWebsocketURI(uri, w.sendingSubnet.BlockchainID.String())
		ginkgo.GinkgoLogr.Info("Creating ethclient for blockchain A", "blockchainID", w.sendingSubnet.BlockchainID)
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)
		w.sendingSubnetClients = append(w.sendingSubnetClients, client)
	}

	w.receivingSubnetClients = make([]*ethclient.Client, 0, len(w.receivingSubnetClients))
	for _, uri := range w.receivingSubnet.ValidatorURIs {
		wsURI := toWebsocketURI(uri, w.receivingSubnet.BlockchainID.String())
		ginkgo.GinkgoLogr.Info("Creating ethclient for blockchain B", "blockchainID", w.receivingSubnet.BlockchainID)
		client, err := ethclient.Dial(wsURI)
		require.NoError(err)
		w.receivingSubnetClients = append(w.receivingSubnetClients, client)
	}
}

func (w *warpTest) sendMessageFromSendingSubnet() {
	tc := e2e.NewTestContext()
	ctx := tc.DefaultContext()
	require := require.New(ginkgo.GinkgoT())

	client := w.sendingSubnetClients[0]

	startingNonce, err := client.NonceAt(ctx, w.sendingSubnetFundedAddress, nil)
	require.NoError(err)

	packedInput, err := warp.PackSendWarpMessage(testPayload)
	require.NoError(err)
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   w.sendingSubnetChainID,
		Nonce:     startingNonce,
		To:        &warp.Module.Address,
		Gas:       200_000,
		GasFeeCap: big.NewInt(225 * params.GWei),
		GasTipCap: big.NewInt(params.GWei),
		Value:     common.Big0,
		Data:      packedInput,
	})
	signedTx, err := types.SignTx(tx, w.sendingSubnetSigner, w.sendingSubnetFundedKey)
	require.NoError(err)
	ginkgo.GinkgoLogr.Info("Sending sendWarpMessage transaction", "txHash", signedTx.Hash())
	require.NoError(client.SendTransaction(ctx, signedTx))

	ginkgo.GinkgoLogr.Info("Waiting for transaction to be accepted")
	receiptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	receipt, err := bind.WaitMined(receiptCtx, client, signedTx)
	require.NoError(err)
	blockHash := receipt.BlockHash
	blockNumber := receipt.BlockNumber.Uint64()

	ginkgo.GinkgoLogr.Info("Constructing warp block hash unsigned message", "blockHash", blockHash)
	w.blockID = ids.ID(blockHash) // Set blockID to construct a warp message containing a block hash payload later
	w.blockPayload, err = payload.NewHash(w.blockID)
	require.NoError(err)
	w.blockPayloadUnsignedMessage, err = avalancheWarp.NewUnsignedMessage(w.networkID, w.sendingSubnet.BlockchainID, w.blockPayload.Bytes())
	require.NoError(err)

	ginkgo.GinkgoLogr.Info("Fetching relevant warp logs from the newly produced block")
	logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
		BlockHash: &blockHash,
		Addresses: []common.Address{warp.Module.Address},
	})
	require.NoError(err)
	require.Len(logs, 1)

	// Check for relevant warp log from subscription and ensure that it matches
	// the log extracted from the last block.
	txLog := logs[0]
	ginkgo.GinkgoLogr.Info("Parsing logData as unsigned warp message")
	unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(txLog.Data)
	require.NoError(err)

	// Set local variables for the duration of the test
	w.addressedCallUnsignedMessage = unsignedMsg
	ginkgo.GinkgoLogr.Info("Parsed unsignedWarpMsg", "unsignedWarpMessageID", w.addressedCallUnsignedMessage.ID(), "unsignedWarpMessage", w.addressedCallUnsignedMessage)

	// Loop over each client on chain A to ensure they all have time to accept the block.
	// Note: if we did not confirm this here, the next stage could be racy since it assumes every node
	// has accepted the block.
	for i, client := range w.sendingSubnetClients {
		// Loop until each node has advanced to >= the height of the block that emitted the warp log
		for {
			receivedBlkNum, err := client.BlockNumber(ctx)
			require.NoError(err)
			if receivedBlkNum >= blockNumber {
				ginkgo.GinkgoLogr.Info("client accepted the block containing SendWarpMessage", "client", i, "height", receivedBlkNum)
				break
			}
		}
	}
}

func (w *warpTest) aggregateSignaturesViaAPI() {
	require := require.New(ginkgo.GinkgoT())
	tc := e2e.NewTestContext()
	ctx := tc.DefaultContext()

	warpAPIs := make(map[ids.NodeID]warpBackend.Client, len(w.sendingSubnetURIs))
	for _, uri := range w.sendingSubnetURIs {
		client, err := warpBackend.NewClient(uri, w.sendingSubnet.BlockchainID.String())
		require.NoError(err)

		infoClient := info.NewClient(uri)
		nodeID, _, err := infoClient.GetNodeID(ctx)
		require.NoError(err)
		warpAPIs[nodeID] = client
	}

	pChainClient := platformvm.NewClient(w.sendingSubnetURIs[0])
	pChainHeight, err := pChainClient.GetHeight(ctx)
	require.NoError(err)
	// If the source subnet is the Primary Network, then we only need to aggregate signatures from the receiving
	// subnet's validator set instead of the entire Primary Network.
	// If the destination turns out to be the Primary Network as well, then this is a no-op.
	var validators map[ids.NodeID]*validators.GetValidatorOutput
	if w.sendingSubnet.SubnetID == constants.PrimaryNetworkID {
		validators, err = pChainClient.GetValidatorsAt(ctx, w.receivingSubnet.SubnetID, api.Height(pChainHeight))
	} else {
		validators, err = pChainClient.GetValidatorsAt(ctx, w.sendingSubnet.SubnetID, api.Height(pChainHeight))
	}
	require.NoError(err)
	require.NotZero(len(validators))

	totalWeight := uint64(0)
	warpValidators := make([]*avalancheWarp.Validator, 0, len(validators))
	for nodeID, validator := range validators {
		warpValidators = append(warpValidators, &avalancheWarp.Validator{
			PublicKey: validator.PublicKey,
			Weight:    validator.Weight,
			NodeIDs:   []ids.NodeID{nodeID},
		})
		totalWeight += validator.Weight
	}

	ginkgo.GinkgoLogr.Info("Aggregating signatures from validator set", "numValidators", len(warpValidators), "totalWeight", totalWeight)
	apiSignatureGetter := NewAPIFetcher(warpAPIs)
	signatureResult, err := aggregator.New(apiSignatureGetter, warpValidators, totalWeight).AggregateSignatures(ctx, w.addressedCallUnsignedMessage, 100)
	require.NoError(err)
	require.Equal(signatureResult.SignatureWeight, signatureResult.TotalWeight)
	require.Equal(signatureResult.SignatureWeight, totalWeight)

	w.addressedCallSignedMessage = signatureResult.Message

	signatureResult, err = aggregator.New(apiSignatureGetter, warpValidators, totalWeight).AggregateSignatures(ctx, w.blockPayloadUnsignedMessage, 100)
	require.NoError(err)
	require.Equal(signatureResult.SignatureWeight, signatureResult.TotalWeight)
	require.Equal(signatureResult.SignatureWeight, totalWeight)
	w.blockPayloadSignedMessage = signatureResult.Message

	ginkgo.GinkgoLogr.Info("Aggregated signatures for warp messages", "addressedCallMessage", common.Bytes2Hex(w.addressedCallSignedMessage.Bytes()), "blockPayloadMessage", common.Bytes2Hex(w.blockPayloadSignedMessage.Bytes()))
}

func (w *warpTest) aggregateSignatures() {
	require := require.New(ginkgo.GinkgoT())
	tc := e2e.NewTestContext()
	ctx := tc.DefaultContext()

	// Verify that the signature aggregation matches the results of manually constructing the warp message
	client, err := warpBackend.NewClient(w.sendingSubnetURIs[0], w.sendingSubnet.BlockchainID.String())
	require.NoError(err)

	ginkgo.GinkgoLogr.Info("Fetching addressed call aggregate signature via p2p API")
	subnetIDStr := ""
	if w.sendingSubnet.SubnetID == constants.PrimaryNetworkID {
		subnetIDStr = w.receivingSubnet.SubnetID.String()
	}
	signedWarpMessageBytes, err := client.GetMessageAggregateSignature(ctx, w.addressedCallSignedMessage.ID(), warp.WarpQuorumDenominator, subnetIDStr)
	require.NoError(err)
	require.Equal(w.addressedCallSignedMessage.Bytes(), signedWarpMessageBytes)

	ginkgo.GinkgoLogr.Info("Fetching block payload aggregate signature via p2p API")
	signedWarpBlockBytes, err := client.GetBlockAggregateSignature(ctx, w.blockID, warp.WarpQuorumDenominator, subnetIDStr)
	require.NoError(err)
	require.Equal(w.blockPayloadSignedMessage.Bytes(), signedWarpBlockBytes)
}

func (w *warpTest) deliverAddressedCallToReceivingSubnet() {
	require := require.New(ginkgo.GinkgoT())
	tc := e2e.NewTestContext()
	ctx := tc.DefaultContext()

	client := w.receivingSubnetClients[0]

	nonce, err := client.NonceAt(ctx, w.receivingSubnetFundedAddress, nil)
	require.NoError(err)

	packedInput, err := warp.PackGetVerifiedWarpMessage(0)
	require.NoError(err)
	tx := predicate.NewPredicateTx(
		w.receivingSubnetChainID,
		nonce,
		&warp.Module.Address,
		5_000_000,
		big.NewInt(225*params.GWei),
		big.NewInt(params.GWei),
		common.Big0,
		packedInput,
		types.AccessList{},
		warp.ContractAddress,
		w.addressedCallSignedMessage.Bytes(),
	)
	signedTx, err := types.SignTx(tx, w.receivingSubnetSigner, w.receivingSubnetFundedKey)
	require.NoError(err)
	txBytes, err := signedTx.MarshalBinary()
	require.NoError(err)
	ginkgo.GinkgoLogr.Info("Sending getVerifiedWarpMessage transaction", "txHash", signedTx.Hash(), "txBytes", common.Bytes2Hex(txBytes))
	require.NoError(client.SendTransaction(ctx, signedTx))

	ginkgo.GinkgoLogr.Info("Waiting for transaction to be accepted")
	receiptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	receipt, err := bind.WaitMined(receiptCtx, client, signedTx)
	require.NoError(err)
	blockHash := receipt.BlockHash

	ginkgo.GinkgoLogr.Info("Fetching relevant warp logs and receipts from new block")
	logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
		BlockHash: &blockHash,
		Addresses: []common.Address{warp.Module.Address},
	})
	require.NoError(err)
	require.Len(logs, 0)
	require.NoError(err)
	require.Equal(receipt.Status, types.ReceiptStatusSuccessful)
}

func (w *warpTest) deliverBlockHashPayload() {
	require := require.New(ginkgo.GinkgoT())
	tc := e2e.NewTestContext()
	ctx := tc.DefaultContext()

	client := w.receivingSubnetClients[0]

	nonce, err := client.NonceAt(ctx, w.receivingSubnetFundedAddress, nil)
	require.NoError(err)

	packedInput, err := warp.PackGetVerifiedWarpBlockHash(0)
	require.NoError(err)
	tx := predicate.NewPredicateTx(
		w.receivingSubnetChainID,
		nonce,
		&warp.Module.Address,
		5_000_000,
		big.NewInt(225*params.GWei),
		big.NewInt(params.GWei),
		common.Big0,
		packedInput,
		types.AccessList{},
		warp.ContractAddress,
		w.blockPayloadSignedMessage.Bytes(),
	)
	signedTx, err := types.SignTx(tx, w.receivingSubnetSigner, w.receivingSubnetFundedKey)
	require.NoError(err)
	txBytes, err := signedTx.MarshalBinary()
	require.NoError(err)
	ginkgo.GinkgoLogr.Info("Sending getVerifiedWarpBlockHash transaction", "txHash", signedTx.Hash(), "txBytes", common.Bytes2Hex(txBytes))
	require.NoError(client.SendTransaction(ctx, signedTx))

	ginkgo.GinkgoLogr.Info("Waiting for transaction to be accepted")
	receiptCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	receipt, err := bind.WaitMined(receiptCtx, client, signedTx)
	require.NoError(err)
	blockHash := receipt.BlockHash
	ginkgo.GinkgoLogr.Info("Fetching relevant warp logs and receipts from new block")
	logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
		BlockHash: &blockHash,
		Addresses: []common.Address{warp.Module.Address},
	})
	require.NoError(err)
	require.Len(logs, 0)
	require.NoError(err)
	require.Equal(receipt.Status, types.ReceiptStatusSuccessful)
}

func (w *warpTest) warpLoad() {
	require := require.New(ginkgo.GinkgoT())
	tc := e2e.NewTestContext()
	ctx := tc.DefaultContext()

	var (
		numWorkers           = len(w.sendingSubnetClients)
		txsPerWorker  uint64 = 10
		batchSize     uint64 = 10
		sendingClient        = w.sendingSubnetClients[0]
	)

	chainAKeys, chainAPrivateKeys := generateKeys(w.sendingSubnetFundedKey, numWorkers)
	chainBKeys, chainBPrivateKeys := generateKeys(w.receivingSubnetFundedKey, numWorkers)

	loadMetrics := metrics.NewDefaultMetrics()

	ginkgo.GinkgoLogr.Info("Distributing funds on sending subnet", "numKeys", len(chainAKeys))
	chainAKeys, err := load.DistributeFunds(ctx, sendingClient, chainAKeys, len(chainAKeys), new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether)), loadMetrics)
	require.NoError(err)

	ginkgo.GinkgoLogr.Info("Distributing funds on receiving subnet", "numKeys", len(chainBKeys))
	_, err = load.DistributeFunds(ctx, w.receivingSubnetClients[0], chainBKeys, len(chainBKeys), new(big.Int).Mul(big.NewInt(100), big.NewInt(params.Ether)), loadMetrics)
	require.NoError(err)

	ginkgo.GinkgoLogr.Info("Creating workers for each subnet...")
	chainAWorkers := make([]txs.Worker[*types.Transaction], 0, len(chainAKeys))
	for i := range chainAKeys {
		chainAWorkers = append(chainAWorkers, load.NewTxReceiptWorker(ctx, w.sendingSubnetClients[i]))
	}
	chainBWorkers := make([]txs.Worker[*types.Transaction], 0, len(chainBKeys))
	for i := range chainBKeys {
		chainBWorkers = append(chainBWorkers, load.NewTxReceiptWorker(ctx, w.receivingSubnetClients[i]))
	}

	ginkgo.GinkgoLogr.Info("Subscribing to warp send events on sending subnet")
	logs := make(chan types.Log, numWorkers*int(txsPerWorker))
	sub, err := sendingClient.SubscribeFilterLogs(ctx, ethereum.FilterQuery{
		Addresses: []common.Address{warp.Module.Address},
	}, logs)
	require.NoError(err)
	defer func() {
		sub.Unsubscribe()
		require.NoError(<-sub.Err())
	}()

	ginkgo.GinkgoLogr.Info("Generating tx sequence to send warp messages...")
	warpSendSequences, err := txs.GenerateTxSequences(ctx, func(key *ecdsa.PrivateKey, nonce uint64) (*types.Transaction, error) {
		data, err := warp.PackSendWarpMessage([]byte(fmt.Sprintf("Jets %d-%d Dolphins", key.X.Int64(), nonce)))
		if err != nil {
			return nil, err
		}
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   w.sendingSubnetChainID,
			Nonce:     nonce,
			To:        &warp.Module.Address,
			Gas:       200_000,
			GasFeeCap: big.NewInt(225 * params.GWei),
			GasTipCap: big.NewInt(params.GWei),
			Value:     common.Big0,
			Data:      data,
		})
		return types.SignTx(tx, w.sendingSubnetSigner, key)
	}, w.sendingSubnetClients[0], chainAPrivateKeys, txsPerWorker, false)
	require.NoError(err)
	ginkgo.GinkgoLogr.Info("Executing warp send loader...")
	warpSendLoader := load.New(chainAWorkers, warpSendSequences, batchSize, loadMetrics)
	// TODO: execute send and receive loaders concurrently.
	require.NoError(warpSendLoader.Execute(ctx))
	require.NoError(warpSendLoader.ConfirmReachedTip(ctx))

	warpClient, err := warpBackend.NewClient(w.sendingSubnetURIs[0], w.sendingSubnet.BlockchainID.String())
	require.NoError(err)
	subnetIDStr := ""
	if w.sendingSubnet.SubnetID == constants.PrimaryNetworkID {
		subnetIDStr = w.receivingSubnet.SubnetID.String()
	}

	ginkgo.GinkgoLogr.Info("Executing warp delivery sequences...")
	warpDeliverSequences, err := txs.GenerateTxSequences(ctx, func(key *ecdsa.PrivateKey, nonce uint64) (*types.Transaction, error) {
		// Wait for the next warp send log
		warpLog := <-logs

		unsignedMessage, err := warp.UnpackSendWarpEventDataToMessage(warpLog.Data)
		if err != nil {
			return nil, err
		}
		ginkgo.GinkgoLogr.Info("Fetching addressed call aggregate signature via p2p API")

		signedWarpMessageBytes, err := warpClient.GetMessageAggregateSignature(ctx, unsignedMessage.ID(), warp.WarpDefaultQuorumNumerator, subnetIDStr)
		if err != nil {
			return nil, err
		}

		packedInput, err := warp.PackGetVerifiedWarpMessage(0)
		if err != nil {
			return nil, err
		}
		tx := predicate.NewPredicateTx(
			w.receivingSubnetChainID,
			nonce,
			&warp.Module.Address,
			5_000_000,
			big.NewInt(225*params.GWei),
			big.NewInt(params.GWei),
			common.Big0,
			packedInput,
			types.AccessList{},
			warp.ContractAddress,
			signedWarpMessageBytes,
		)
		return types.SignTx(tx, w.receivingSubnetSigner, key)
	}, w.receivingSubnetClients[0], chainBPrivateKeys, txsPerWorker, true)
	require.NoError(err)

	ginkgo.GinkgoLogr.Info("Executing warp delivery...")
	warpDeliverLoader := load.New(chainBWorkers, warpDeliverSequences, batchSize, loadMetrics)
	require.NoError(warpDeliverLoader.Execute(ctx))
	require.NoError(warpSendLoader.ConfirmReachedTip(ctx))
	ginkgo.GinkgoLogr.Info("Completed warp delivery successfully.")
}

func generateKeys(preFundedKey *ecdsa.PrivateKey, numWorkers int) ([]*key.Key, []*ecdsa.PrivateKey) {
	keys := []*key.Key{
		key.CreateKey(preFundedKey),
	}
	privateKeys := []*ecdsa.PrivateKey{
		preFundedKey,
	}
	for i := 1; i < numWorkers; i++ {
		newKey, err := key.Generate()
		require.NoError(ginkgo.GinkgoT(), err)
		keys = append(keys, newKey)
		privateKeys = append(privateKeys, newKey.PrivKey)
	}
	return keys, privateKeys
}

func toWebsocketURI(uri string, blockchainID string) string {
	return fmt.Sprintf("ws://%s/ext/bc/%s/ws", strings.TrimPrefix(uri, "http://"), blockchainID)
}

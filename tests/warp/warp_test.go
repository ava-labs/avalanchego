// Copyright (C) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements solidity tests.
package warp

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ava-labs/avalanche-network-runner/rpcpb"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/set"
	avalancheWarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/ethclient"
	"github.com/ava-labs/subnet-evm/interfaces"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/plugin/evm"
	"github.com/ava-labs/subnet-evm/predicate"
	"github.com/ava-labs/subnet-evm/tests/utils"
	"github.com/ava-labs/subnet-evm/tests/utils/runner"
	warpBackend "github.com/ava-labs/subnet-evm/warp"
	"github.com/ava-labs/subnet-evm/x/warp"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

const fundedKeyStr = "56289e99c94b6912bfc12adc093c9b51124f0dc54ac7a766b2bc5ccf558d8027"

var (
	config                         = runner.NewDefaultANRConfig()
	manager                        = runner.NewNetworkManager(config)
	warpChainConfigPath            string
	unsignedWarpMsg                *avalancheWarp.UnsignedMessage
	unsignedWarpMessageID          ids.ID
	signedWarpMsg                  *avalancheWarp.Message
	blockchainIDA, blockchainIDB   ids.ID
	chainAURIs, chainBURIs         []string
	chainAWSClient, chainBWSClient ethclient.Client
	chainID                        = big.NewInt(99999)
	fundedKey                      *ecdsa.PrivateKey
	fundedAddress                  common.Address
	payload                        = []byte{1, 2, 3}
	txSigner                       = types.LatestSignerForChainID(chainID)
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "subnet-evm warp e2e test")
}

func toWebsocketURI(uri string, blockchainID string) string {
	return fmt.Sprintf("ws://%s/ext/bc/%s/ws", strings.TrimPrefix(uri, "http://"), blockchainID)
}

// BeforeSuite starts the default network and adds 10 new nodes as validators with BLS keys
// registered on the P-Chain.
// Adds two disjoint sets of 5 of the new validator nodes to validate two new subnets with a
// a single Subnet-EVM blockchain.
var _ = ginkgo.BeforeSuite(func() {
	ctx := context.Background()
	var err error
	// Name 10 new validators (which should have BLS key registered)
	subnetANodeNames := make([]string, 0)
	subnetBNodeNames := []string{}
	for i := 1; i <= 10; i++ {
		n := fmt.Sprintf("node%d-bls", i)
		if i <= 5 {
			subnetANodeNames = append(subnetANodeNames, n)
		} else {
			subnetBNodeNames = append(subnetBNodeNames, n)
		}
	}
	f, err := os.CreateTemp(os.TempDir(), "config.json")
	gomega.Expect(err).Should(gomega.BeNil())
	_, err = f.Write([]byte(`{"warp-api-enabled": true}`))
	gomega.Expect(err).Should(gomega.BeNil())
	warpChainConfigPath = f.Name()

	// Construct the network using the avalanche-network-runner
	_, err = manager.StartDefaultNetwork(ctx)
	gomega.Expect(err).Should(gomega.BeNil())
	err = manager.SetupNetwork(
		ctx,
		config.AvalancheGoExecPath,
		[]*rpcpb.BlockchainSpec{
			{
				VmName:      evm.IDStr,
				Genesis:     "./tests/precompile/genesis/warp.json",
				ChainConfig: warpChainConfigPath,
				SubnetSpec: &rpcpb.SubnetSpec{
					SubnetConfig: "",
					Participants: subnetANodeNames,
				},
			},
			{
				VmName:      evm.IDStr,
				Genesis:     "./tests/precompile/genesis/warp.json",
				ChainConfig: warpChainConfigPath,
				SubnetSpec: &rpcpb.SubnetSpec{
					SubnetConfig: "",
					Participants: subnetBNodeNames,
				},
			},
		},
	)
	gomega.Expect(err).Should(gomega.BeNil())

	// Issue transactions to activate the proposerVM fork on the receiving chain
	chainID := big.NewInt(99999)
	fundedKey, err = crypto.HexToECDSA(fundedKeyStr)
	fundedAddress = crypto.PubkeyToAddress(fundedKey.PublicKey)
	gomega.Expect(err).Should(gomega.BeNil())
	subnetB := manager.GetSubnets()[1]
	subnetBDetails, ok := manager.GetSubnet(subnetB)
	gomega.Expect(ok).Should(gomega.BeTrue())

	chainBID := subnetBDetails.BlockchainID
	uri := toWebsocketURI(subnetBDetails.ValidatorURIs[0], chainBID.String())
	client, err := ethclient.Dial(uri)
	gomega.Expect(err).Should(gomega.BeNil())

	err = utils.IssueTxsToActivateProposerVMFork(ctx, chainID, fundedKey, client)
	gomega.Expect(err).Should(gomega.BeNil())

	subnetIDs := manager.GetSubnets()
	gomega.Expect(len(subnetIDs)).Should(gomega.Equal(2))

	subnetA := subnetIDs[0]
	subnetADetails, ok := manager.GetSubnet(subnetA)
	gomega.Expect(ok).Should(gomega.BeTrue())
	blockchainIDA = subnetADetails.BlockchainID
	gomega.Expect(len(subnetADetails.ValidatorURIs)).Should(gomega.Equal(5))
	chainAURIs = append(chainAURIs, subnetADetails.ValidatorURIs...)

	subnetB = subnetIDs[1]
	subnetBDetails, ok = manager.GetSubnet(subnetB)
	gomega.Expect(ok).Should(gomega.BeTrue())
	blockchainIDB = subnetBDetails.BlockchainID
	gomega.Expect(len(subnetBDetails.ValidatorURIs)).Should(gomega.Equal(5))
	chainBURIs = append(chainBURIs, subnetBDetails.ValidatorURIs...)

	log.Info("Created URIs for both subnets", "ChainAURIs", chainAURIs, "ChainBURIs", chainBURIs, "blockchainIDA", blockchainIDA, "blockchainIDB", blockchainIDB)

	chainAWSURI := toWebsocketURI(chainAURIs[0], blockchainIDA.String())
	log.Info("Creating ethclient for blockchainA", "wsURI", chainAWSURI)
	chainAWSClient, err = ethclient.Dial(chainAWSURI)
	gomega.Expect(err).Should(gomega.BeNil())

	chainBWSURI := toWebsocketURI(chainBURIs[0], blockchainIDB.String())
	log.Info("Creating ethclient for blockchainB", "wsURI", chainBWSURI)
	chainBWSClient, err = ethclient.Dial(chainBWSURI)
	gomega.Expect(err).Should(gomega.BeNil())
})

var _ = ginkgo.AfterSuite(func() {
	gomega.Expect(manager).ShouldNot(gomega.BeNil())
	gomega.Expect(manager.TeardownNetwork()).Should(gomega.BeNil())
	gomega.Expect(os.Remove(warpChainConfigPath)).Should(gomega.BeNil())
	// TODO: bootstrap an additional node to ensure that we can bootstrap the test data correctly
})

var _ = ginkgo.Describe("[Warp]", ginkgo.Ordered, func() {
	// Send a transaction to Subnet A to issue a Warp Message to Subnet B
	ginkgo.It("Send Message from A to B", ginkgo.Label("Warp", "SendWarp"), func() {
		ctx := context.Background()

		log.Info("Subscribing to new heads")
		newHeads := make(chan *types.Header, 10)
		sub, err := chainAWSClient.SubscribeNewHead(ctx, newHeads)
		gomega.Expect(err).Should(gomega.BeNil())
		defer sub.Unsubscribe()

		startingNonce, err := chainAWSClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		packedInput, err := warp.PackSendWarpMessage(payload)
		gomega.Expect(err).Should(gomega.BeNil())
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     startingNonce,
			To:        &warp.Module.Address,
			Gas:       200_000,
			GasFeeCap: big.NewInt(225 * params.GWei),
			GasTipCap: big.NewInt(params.GWei),
			Value:     common.Big0,
			Data:      packedInput,
		})
		signedTx, err := types.SignTx(tx, txSigner, fundedKey)
		gomega.Expect(err).Should(gomega.BeNil())
		log.Info("Sending sendWarpMessage transaction", "txHash", signedTx.Hash())
		err = chainAWSClient.SendTransaction(ctx, signedTx)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Waiting for new block confirmation")
		newHead := <-newHeads
		blockHash := newHead.Hash()

		log.Info("Fetching relevant warp logs from the newly produced block")
		logs, err := chainAWSClient.FilterLogs(ctx, interfaces.FilterQuery{
			BlockHash: &blockHash,
			Addresses: []common.Address{warp.Module.Address},
		})
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(len(logs)).Should(gomega.Equal(1))

		// Check for relevant warp log from subscription and ensure that it matches
		// the log extracted from the last block.
		txLog := logs[0]
		log.Info("Parsing logData as unsigned warp message")
		unsignedMsg, err := warp.UnpackSendWarpEventDataToMessage(txLog.Data)
		gomega.Expect(err).Should(gomega.BeNil())

		// Set local variables for the duration of the test
		unsignedWarpMessageID = unsignedMsg.ID()
		unsignedWarpMsg = unsignedMsg
		log.Info("Parsed unsignedWarpMsg", "unsignedWarpMessageID", unsignedWarpMessageID, "unsignedWarpMessage", unsignedWarpMsg)

		// Loop over each client on chain A to ensure they all have time to accept the block.
		// Note: if we did not confirm this here, the next stage could be racy since it assumes every node
		// has accepted the block.
		for i, uri := range chainAURIs {
			chainAWSURI := toWebsocketURI(uri, blockchainIDA.String())
			log.Info("Creating ethclient for blockchainA", "wsURI", chainAWSURI)
			client, err := ethclient.Dial(chainAWSURI)
			gomega.Expect(err).Should(gomega.BeNil())

			// Loop until each node has advanced to >= the height of the block that emitted the warp log
			for {
				block, err := client.BlockByNumber(ctx, nil)
				gomega.Expect(err).Should(gomega.BeNil())
				if block.NumberU64() >= newHead.Number.Uint64() {
					log.Info("client accepted the block containing SendWarpMessage", "client", i, "height", block.NumberU64())
					break
				}
			}
		}
	})

	// Aggregate a Warp Signature by sending an API request to each node requesting its signature and manually
	// constructing a valid Avalanche Warp Message
	ginkgo.It("Aggregate Warp Signature via API", ginkgo.Label("Warp", "ReceiveWarp", "AggregateWarpManually"), func() {
		ctx := context.Background()

		blsSignatures := make([]*bls.Signature, 0, len(chainAURIs))
		for i, uri := range chainAURIs {
			client, err := warpBackend.NewClient(uri, blockchainIDA.String())
			gomega.Expect(err).Should(gomega.BeNil())
			log.Info("Fetching warp signature from node")
			rawSignatureBytes, err := client.GetSignature(ctx, unsignedWarpMessageID)
			gomega.Expect(err).Should(gomega.BeNil())

			blsSignature, err := bls.SignatureFromBytes(rawSignatureBytes)
			gomega.Expect(err).Should(gomega.BeNil())

			infoClient := info.NewClient(uri)
			nodeID, blsSigner, err := infoClient.GetNodeID(ctx)
			gomega.Expect(err).Should(gomega.BeNil())

			blsSignatures = append(blsSignatures, blsSignature)

			blsPublicKey := blsSigner.Key()
			log.Info("Verifying BLS Signature from node", "nodeID", nodeID, "nodeIndex", i)
			gomega.Expect(bls.Verify(blsPublicKey, blsSignature, unsignedWarpMsg.Bytes())).Should(gomega.BeTrue())
		}

		blsAggregatedSignature, err := bls.AggregateSignatures(blsSignatures)
		gomega.Expect(err).Should(gomega.BeNil())

		signersBitSet := set.NewBits()
		for i := 0; i < len(blsSignatures); i++ {
			signersBitSet.Add(i)
		}
		warpSignature := &avalancheWarp.BitSetSignature{
			Signers: signersBitSet.Bytes(),
		}

		blsAggregatedSignatureBytes := bls.SignatureToBytes(blsAggregatedSignature)
		copy(warpSignature.Signature[:], blsAggregatedSignatureBytes)

		warpMsg, err := avalancheWarp.NewMessage(
			unsignedWarpMsg,
			warpSignature,
		)
		gomega.Expect(err).Should(gomega.BeNil())
		signedWarpMsg = warpMsg
	})

	// Aggregate a Warp Signature using the node's Signature Aggregation API call and verifying that its output matches the
	// the manual construction
	ginkgo.It("Aggregate Warp Signature via Aggregator", ginkgo.Label("Warp", "ReceiveWarp", "AggregatorWarp"), func() {
		ctx := context.Background()

		// Verify that the signature aggregation matches the results of manually constructing the warp message
		client, err := warpBackend.NewClient(chainAURIs[0], blockchainIDA.String())
		gomega.Expect(err).Should(gomega.BeNil())

		// Specify WarpQuorumDenominator to retrieve signatures from every validator
		signedWarpMessageBytes, err := client.GetAggregateSignature(ctx, unsignedWarpMessageID, params.WarpQuorumDenominator)
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(signedWarpMessageBytes).Should(gomega.Equal(signedWarpMsg.Bytes()))
	})

	// Verify successful delivery of the Avalanche Warp Message from Chain A to Chain B
	ginkgo.It("Verify Message from A to B", ginkgo.Label("Warp", "VerifyMessage"), func() {
		ctx := context.Background()

		log.Info("Subscribing to new heads")
		newHeads := make(chan *types.Header, 10)
		sub, err := chainBWSClient.SubscribeNewHead(ctx, newHeads)
		gomega.Expect(err).Should(gomega.BeNil())
		defer sub.Unsubscribe()

		nonce, err := chainBWSClient.NonceAt(ctx, fundedAddress, nil)
		gomega.Expect(err).Should(gomega.BeNil())

		packedInput, err := warp.PackGetVerifiedWarpMessage(0)
		gomega.Expect(err).Should(gomega.BeNil())
		tx := predicate.NewPredicateTx(
			chainID,
			nonce,
			&warp.Module.Address,
			5_000_000,
			big.NewInt(225*params.GWei),
			big.NewInt(params.GWei),
			common.Big0,
			packedInput,
			types.AccessList{},
			warp.ContractAddress,
			signedWarpMsg.Bytes(),
		)
		signedTx, err := types.SignTx(tx, txSigner, fundedKey)
		gomega.Expect(err).Should(gomega.BeNil())
		txBytes, err := signedTx.MarshalBinary()
		gomega.Expect(err).Should(gomega.BeNil())
		log.Info("Sending getVerifiedWarpMessage transaction", "txHash", signedTx.Hash(), "txBytes", common.Bytes2Hex(txBytes))
		err = chainBWSClient.SendTransaction(ctx, signedTx)
		gomega.Expect(err).Should(gomega.BeNil())

		log.Info("Waiting for new block confirmation")
		newHead := <-newHeads
		blockHash := newHead.Hash()
		log.Info("Fetching relevant warp logs and receipts from new block")
		logs, err := chainBWSClient.FilterLogs(ctx, interfaces.FilterQuery{
			BlockHash: &blockHash,
			Addresses: []common.Address{warp.Module.Address},
		})
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(len(logs)).Should(gomega.Equal(0))
		receipt, err := chainBWSClient.TransactionReceipt(ctx, signedTx.Hash())
		gomega.Expect(err).Should(gomega.BeNil())
		gomega.Expect(receipt.Status).Should(gomega.Equal(types.ReceiptStatusSuccessful))
	})
})

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

//go:build test

package vms

import (
	"fmt"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/export"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/importtx"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"

	ginkgo "github.com/onsi/ginkgo/v2"
)

const pollingInterval = 50 * time.Millisecond

var (
	subnetAName = "xsvm-a"
	subnetBName = "xsvm-b"
)

func XSVMSubnetsOrPanic(nodes ...*tmpnet.Node) []*tmpnet.Subnet {
	key, err := secp256k1.NewPrivateKey()
	if err != nil {
		panic(err)
	}
	subnetANodes := nodes
	subnetBNodes := nodes
	if len(nodes) > 1 {
		// Validate tmpnet bootstrap of a disjoint validator set
		midpoint := len(nodes) / 2
		subnetANodes = nodes[:midpoint]
		subnetBNodes = nodes[midpoint:]
	}
	return []*tmpnet.Subnet{
		subnet.NewXSVMOrPanic(subnetAName, key, subnetANodes...),
		subnet.NewXSVMOrPanic(subnetBName, key, subnetBNodes...),
	}
}

var _ = ginkgo.Describe("[XSVM]", func() {
	require := require.New(ginkgo.GinkgoT())

	ginkgo.It("should support transfers between subnets", func() {
		network := e2e.Env.GetNetwork()

		sourceSubnet := network.GetSubnet(subnetAName)
		require.NotNil(sourceSubnet)
		destinationSubnet := network.GetSubnet(subnetBName)
		require.NotNil(destinationSubnet)

		sourceChain := sourceSubnet.Chains[0]
		destinationChain := destinationSubnet.Chains[0]

		sourceValidators := getNodesForIDs(network.Nodes, sourceSubnet.ValidatorIDs)
		require.NotEmpty(sourceValidators)
		sourceAPINode := sourceValidators[0]
		tests.Outf(" issuing transactions for source subnet on %s (%s)\n", sourceAPINode.NodeID, sourceAPINode.URI)

		destinationValidators := getNodesForIDs(network.Nodes, destinationSubnet.ValidatorIDs)
		require.NotEmpty(destinationValidators)
		destinationAPINode := destinationValidators[0]
		tests.Outf(" issuing transactions for destination subnet on %s (%s)\n", destinationAPINode.NodeID, destinationAPINode.URI)

		destinationKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		ginkgo.By("checking that the funded key has sufficient funds for the export")
		sourceClient := api.NewClient(sourceAPINode.URI, sourceChain.ChainID.String())
		initialSourcedBalance, err := sourceClient.Balance(
			e2e.DefaultContext(),
			sourceChain.PreFundedKey.Address(),
			sourceChain.ChainID,
		)
		require.NoError(err)
		require.GreaterOrEqual(initialSourcedBalance, units.Schmeckle)

		ginkgo.By(fmt.Sprintf("exporting from chain %s on subnet %s", sourceChain.ChainID, sourceSubnet.SubnetID))
		exportTxStatus, err := export.Export(
			e2e.DefaultContext(),
			&export.Config{
				URI:                sourceAPINode.URI,
				SourceChainID:      sourceChain.ChainID,
				DestinationChainID: destinationChain.ChainID,
				Amount:             units.Schmeckle,
				To:                 destinationKey.Address(),
				PrivateKey:         sourceChain.PreFundedKey,
			},
		)
		require.NoError(err)
		tests.Outf(" issued transaction with ID: %s\n", exportTxStatus.TxID)

		ginkgo.By("checking that the export transaction has been accepted on all nodes")
		for _, node := range sourceValidators[1:] {
			require.NoError(api.AwaitTxAccepted(
				e2e.DefaultContext(),
				api.NewClient(node.URI, sourceChain.ChainID.String()),
				sourceChain.PreFundedKey.Address(),
				exportTxStatus.Nonce,
				pollingInterval,
			))
		}

		ginkgo.By(fmt.Sprintf("issuing transaction on chain %s on subnet %s to activate snowman++ consensus",
			destinationChain.ChainID, destinationSubnet.SubnetID))
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		transferTxStatus, err := transfer.Transfer(
			e2e.DefaultContext(),
			&transfer.Config{
				URI:        destinationAPINode.URI,
				ChainID:    destinationChain.ChainID,
				AssetID:    destinationChain.ChainID,
				Amount:     units.Schmeckle,
				To:         recipientKey.Address(),
				PrivateKey: destinationChain.PreFundedKey,
			},
		)
		require.NoError(err)
		tests.Outf(" issued transaction with ID: %s\n", transferTxStatus.TxID)

		ginkgo.By(fmt.Sprintf("importing to blockchain %s on subnet %s", destinationChain.ChainID, destinationSubnet.SubnetID))
		sourceURIs := make([]string, len(sourceValidators))
		for i, node := range sourceValidators {
			sourceURIs[i] = node.URI
		}
		importTxStatus, err := importtx.Import(
			e2e.DefaultContext(),
			&importtx.Config{
				URI:                destinationAPINode.URI,
				SourceURIs:         sourceURIs,
				SourceChainID:      sourceChain.ChainID.String(),
				DestinationChainID: destinationChain.ChainID.String(),
				TxID:               exportTxStatus.TxID,
				PrivateKey:         destinationKey,
			},
		)
		require.NoError(err)
		tests.Outf(" issued transaction with ID: %s\n", importTxStatus.TxID)

		ginkgo.By("checking that the balance of the source key has decreased")
		sourceBalance, err := sourceClient.Balance(e2e.DefaultContext(), sourceChain.PreFundedKey.Address(), sourceChain.ChainID)
		require.NoError(err)
		require.GreaterOrEqual(initialSourcedBalance-units.Schmeckle, sourceBalance)

		ginkgo.By("checking that the balance of the destination key is non-zero")
		destinationClient := api.NewClient(destinationAPINode.URI, destinationChain.ChainID.String())
		destinationBalance, err := destinationClient.Balance(e2e.DefaultContext(), destinationKey.Address(), sourceChain.ChainID)
		require.NoError(err)
		require.Equal(units.Schmeckle, destinationBalance)
	})
})

// Retrieve the nodes corresponding to the provided IDs
func getNodesForIDs(nodes []*tmpnet.Node, nodeIDs []ids.NodeID) []*tmpnet.Node {
	desiredNodes := make([]*tmpnet.Node, 0, len(nodeIDs))
	for _, node := range nodes {
		for _, nodeID := range nodeIDs {
			if node.NodeID == nodeID {
				desiredNodes = append(desiredNodes, node)
			}
		}
	}
	return desiredNodes
}

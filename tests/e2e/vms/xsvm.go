// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package vms

import (
	"fmt"
	"math"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/export"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/importtx"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var (
	subnetAName = "xsvm-a"
	subnetBName = "xsvm-b"

	numTriggerTxs = 2 // Number of txs needed to activate the proposer VM fork
)

func XSVMSubnets(nodes ...*tmpnet.Node) []*tmpnet.Subnet {
	return []*tmpnet.Subnet{
		newXSVMSubnet(subnetAName, nodes...),
		newXSVMSubnet(subnetBName, nodes...),
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

		apiNode := network.Nodes[0]
		tests.Outf(" issuing transactions on %s (%s)\n", apiNode.NodeID, apiNode.URI)

		destinationKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)

		ginkgo.By("checking that the funded key has sufficient funds for the export")
		sourceClient := api.NewClient(apiNode.URI, sourceChain.ChainID.String())
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
				URI:                apiNode.URI,
				SourceChainID:      sourceChain.ChainID,
				DestinationChainID: destinationChain.ChainID,
				Amount:             units.Schmeckle,
				To:                 destinationKey.Address(),
				PrivateKey:         sourceChain.PreFundedKey,
			},
		)
		require.NoError(err)
		tests.Outf(" issued transaction with ID: %s\n", exportTxStatus.TxID)

		ginkgo.By(fmt.Sprintf("issuing transactions on chain %s on subnet %s to activate snowman++ consensus",
			destinationChain.ChainID, destinationSubnet.SubnetID))
		recipientKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		for i := 0; i < numTriggerTxs; i++ {
			transferTxStatus, err := transfer.Transfer(
				e2e.DefaultContext(),
				&transfer.Config{
					URI:        apiNode.URI,
					ChainID:    destinationChain.ChainID,
					AssetID:    destinationChain.ChainID,
					Amount:     units.Schmeckle,
					To:         recipientKey.Address(),
					PrivateKey: destinationChain.PreFundedKey,
				},
			)
			require.NoError(err)
			tests.Outf(" issued transaction with ID: %s\n", transferTxStatus.TxID)
		}

		ginkgo.By(fmt.Sprintf("importing to blockchain %s on subnet %s", destinationChain.ChainID, destinationSubnet.SubnetID))
		sourceURIs := make([]string, len(network.Nodes))
		for i, node := range network.Nodes {
			sourceURIs[i] = node.URI
		}
		importTxStatus, err := importtx.Import(
			e2e.DefaultContext(),
			&importtx.Config{
				URI:                apiNode.URI,
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
		destinationClient := api.NewClient(apiNode.URI, destinationChain.ChainID.String())
		destinationBalance, err := destinationClient.Balance(e2e.DefaultContext(), destinationKey.Address(), destinationChain.ChainID)
		require.NoError(err)
		require.NotZero(destinationBalance)
	})
})

func newXSVMSubnet(name string, nodes ...*tmpnet.Node) *tmpnet.Subnet {
	if len(nodes) == 0 {
		panic("a subnet must be validated by at least one node")
	}

	key, err := secp256k1.NewPrivateKey()
	if err != nil {
		panic(err)
	}

	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{
		Timestamp: 0,
		Allocations: []genesis.Allocation{
			{
				Address: key.Address(),
				Balance: math.MaxUint64,
			},
		},
	})
	if err != nil {
		panic(err)
	}

	return &tmpnet.Subnet{
		Name: name,
		Chains: []*tmpnet.Chain{
			{
				VMID:         xsvm.ID,
				Genesis:      genesisBytes,
				PreFundedKey: key,
			},
		},
		ValidatorIDs: tmpnet.NodesToIDs(nodes...),
	}
}

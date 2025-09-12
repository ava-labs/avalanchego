// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package s

import (
	"fmt"
	"math"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/cmd/issue/transfer"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
)

var simplexSubnetName = "simplex-a"

func NewSimplexSubnetOrPanic(name string, key *secp256k1.PrivateKey, nodes ...*tmpnet.Node) *tmpnet.Subnet {
	if len(nodes) == 0 {
		panic("a subnet must be validated by at least one node")
	}

	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, &genesis.Genesis{
		Timestamp: time.Now().Unix(),
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
		Config: tmpnet.ConfigMap{
			// Reducing this from the 1s default speeds up tx acceptance
			"proposerMinBlockDelay": 0,
			"consensusConfig": map[string]interface{}{
				"simplexParameters": map[string]interface{}{
					"enabled": true,
				},
			},
		},
		Chains: []*tmpnet.Chain{
			{
				VMID:         constants.XSVMID,
				Genesis:      genesisBytes,
				PreFundedKey: key,
				VersionArgs:  []string{"version-json"},
			},
		},
		ValidatorIDs: tmpnet.NodesToIDs(nodes...),
	}
}

func SimplexSubnetsOrPanic(nodes ...*tmpnet.Node) []*tmpnet.Subnet {
	key, err := secp256k1.NewPrivateKey()
	if err != nil {
		panic(err)
	}

	return []*tmpnet.Subnet{
		NewSimplexSubnetOrPanic(simplexSubnetName, key, nodes...),
	}
}

var _ = e2e.DescribeSimplex("Create a Simplex [L1]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("should support transfers between subnets", func() {
		network := e2e.GetEnv(tc).GetNetwork()

		sourceSubnet := network.GetSubnet(simplexSubnetName)
		require.NotNil(sourceSubnet)

		sourceChain := sourceSubnet.Chains[0]
		sourceValidators := getNodesForIDs(network.Nodes, sourceSubnet.ValidatorIDs)
		require.NotEmpty(sourceValidators)
		sourceAPINode := sourceValidators[0] // set to 1 if bb
		sourceAPINodeURI := sourceAPINode.GetAccessibleURI()
		tc.Log().Info("issuing transactions for source subnet",
			zap.String("subnetName", simplexSubnetName),
			zap.Stringer("nodeID", sourceAPINode.NodeID),
			zap.String("nodeURI", sourceAPINodeURI),
		)

		tc.By(fmt.Sprintf("issuing transaction on chain %s on subnet %s to activate snowman++ consensus",
			sourceChain.ChainID, sourceSubnet.SubnetID))
		recipientKey := e2e.NewPrivateKey(tc)
		transferTxStatus, err := transfer.Transfer(
			tc.DefaultContext(),
			&transfer.Config{
				URI:        sourceAPINodeURI,
				ChainID:    sourceChain.ChainID,
				AssetID:    sourceChain.ChainID,
				Amount:     units.Schmeckle,
				To:         recipientKey.Address(),
				PrivateKey: sourceChain.PreFundedKey,
			},
		)
		require.NoError(err)
		tc.Log().Info("issued transfer transaction",
			zap.Stringer("txID", transferTxStatus.TxID),
		)

		// tc.By(fmt.Sprintf("importing to blockchain %s on subnet %s", destinationChain.ChainID, destinationSubnet.SubnetID))
		// sourceURIs := make([]string, len(sourceValidators))
		// for i, node := range sourceValidators {
		// 	sourceURIs[i] = node.GetAccessibleURI()
		// }
		// importTxStatus, err := importtx.Import(
		// 	tc.DefaultContext(),
		// 	&importtx.Config{
		// 		URI:                destinationAPINodeURI,
		// 		SourceURIs:         sourceURIs,
		// 		SourceChainID:      sourceChain.ChainID.String(),
		// 		DestinationChainID: destinationChain.ChainID.String(),
		// 		TxID:               exportTxStatus.TxID,
		// 		PrivateKey:         destinationKey,
		// 	},
		// )
		// require.NoError(err)
		// tc.Log().Info("issued import transaction",
		// 	zap.Stringer("txID", importTxStatus.TxID),
		// )

		// tc.By("checking that the balance of the source key has decreased")
		// sourceBalance, err := sourceClient.Balance(tc.DefaultContext(), sourceChain.PreFundedKey.Address(), sourceChain.ChainID)
		// require.NoError(err)
		// require.GreaterOrEqual(initialSourcedBalance-units.Schmeckle, sourceBalance)

		// tc.By("checking that the balance of the destination key is non-zero")
		// destinationClient := api.NewClient(destinationAPINodeURI, destinationChain.ChainID.String())
		// destinationBalance, err := destinationClient.Balance(tc.DefaultContext(), destinationKey.Address(), sourceChain.ChainID)
		// require.NoError(err)
		// require.Equal(units.Schmeckle, destinationBalance)

		_ = e2e.CheckBootstrapIsPossible(tc, network)
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

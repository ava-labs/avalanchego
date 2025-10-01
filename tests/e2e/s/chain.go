// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package s

import (
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
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
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
			"consensusConfig": map[string]interface{}{
				"simplexParameters": map[string]interface{}{},
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

	ginkgo.It("Issue and Finalize a Transaction", func() {
		network := e2e.GetEnv(tc).GetNetwork()

		simplexSubnet := network.GetSubnet(simplexSubnetName)
		require.NotNil(simplexSubnet)

		simplexChain := simplexSubnet.Chains[0]
		simplexValidators := getNodesForIDs(network.Nodes, simplexSubnet.ValidatorIDs)
		require.NotEmpty(simplexValidators)

		sortNodes(simplexValidators)

		// advance two rounds
		buildBlock(tc, simplexChain, simplexValidators)
		buildBlock(tc, simplexChain, simplexValidators)

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})

// Builds and proposes a new block by issuing a transfer transaction from the leader of the next round.
// Verifies that all nodes have accepted the block and advanced to the next round.
func buildBlock(tc *e2e.GinkgoTestContext, chain *tmpnet.Chain, nodes []*tmpnet.Node) {
	require := require.New(tc)

	// grab the current round
	client := api.NewClient(nodes[0].GetAccessibleURI(), chain.ChainID.String())
	_, latestBlock, err := client.LastAccepted(tc.DefaultContext())
	require.NoError(err)

	round := latestBlock.Height + 1
	leader := getLeaderForRound(nodes, round)
	tc.Log().Info("current height and round",
		zap.Uint64("height", latestBlock.Height),
		zap.Uint64("round", round),
		zap.Stringer("leader", leader.NodeID),
	)

	// issue a transfer transaction to the leader
	leaderURI := leader.GetAccessibleURI()
	tc.Log().Info("issuing XSVM transfer transaction for simplex subnet",
		zap.Stringer("leader", leader.NodeID),
		zap.String("leaderURI", leaderURI),
	)
	recipientKey := e2e.NewPrivateKey(tc)
	transferTxStatus, err := transfer.Transfer(
		tc.DefaultContext(),
		&transfer.Config{
			URI:        leaderURI,
			ChainID:    chain.ChainID,
			AssetID:    chain.ChainID,
			Amount:     units.Schmeckle,
			To:         recipientKey.Address(),
			PrivateKey: chain.PreFundedKey,
		},
	)
	require.NoError(err)

	tc.Log().Info("successfully issued XSVM transfer transaction",
		zap.Stringer("txID", transferTxStatus.TxID),
		zap.Uint64("round", round),
	)

	tc.By("checking all nodes have accepted the tx")
	for _, node := range nodes {
		client = api.NewClient(node.GetAccessibleURI(), chain.ChainID.String())
		balance, err := client.Balance(tc.DefaultContext(), recipientKey.Address(), chain.ChainID)
		require.NoError(err)
		require.Equal(units.Schmeckle, balance)

		_, statelessBlock, err := client.LastAccepted(tc.DefaultContext())
		require.NoError(err)

		require.Len(statelessBlock.Txs, 1)
		require.Equal(statelessBlock.Txs[0], transferTxStatus.Tx)

		tc.Log().Info("node has accepted the tx",
			zap.Stringer("node", node.NodeID),
			zap.Stringer("txID", transferTxStatus.TxID),
			zap.Uint64("height", statelessBlock.Height),
			zap.Uint64("round", round),
		)
	}
}

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

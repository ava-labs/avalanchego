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
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/api"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/tx"
)

var simplexSubnetName = "simplex"

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

	initialValidators := set.NewSet[ids.NodeID](len(nodes))
	for _, node := range nodes {
		initialValidators.Add(node.NodeID)
	}

	return &tmpnet.Subnet{
		Name: name,
		Config: tmpnet.ConfigMap{
			"simplexParameters": map[string]interface{}{
				"initialValidators": initialValidators,
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
		issueAndConfirmTx(tc, simplexChain, simplexValidators)
		issueAndConfirmTx(tc, simplexChain, simplexValidators)

		_ = e2e.CheckBootstrapIsPossible(tc, network)
	})
})

// Builds and proposes a new block by issuing a transfer transaction from the leader of the next round.
// Verifies that all nodes have accepted the block and advanced to the next round.
func issueAndConfirmTx(tc *e2e.GinkgoTestContext, chain *tmpnet.Chain, nodes []*tmpnet.Node) {
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

	// record the nonces of all nodes before issuing the tx
	nonces := make(map[ids.NodeID]uint64)
	for _, node := range nodes {
		client = api.NewClient(node.GetAccessibleURI(), chain.ChainID.String())
		nonce, err := client.Nonce(tc.DefaultContext(), chain.PreFundedKey.Address())
		require.NoError(err)
		nonces[node.NodeID] = nonce
	}

	// issue a transfer transaction to the leader
	leaderURI := leader.GetAccessibleURI()
	tc.Log().Info("issuing XSVM transfer transaction for simplex subnet",
		zap.Stringer("leader", leader.NodeID),
		zap.String("leaderURI", leaderURI),
	)
	recipientKey := e2e.NewPrivateKey(tc)

	// create and sign the transaction
	utx := &tx.Transfer{
		ChainID: chain.ChainID,
		Nonce:   nonces[leader.NodeID],
		MaxFee:  0,
		AssetID: chain.ChainID,
		Amount:  units.Schmeckle,
		To:      recipientKey.Address(),
	}
	stx, err := tx.Sign(utx, chain.PreFundedKey)
	require.NoError(err)
	txID, err := stx.ID()
	require.NoError(err)

	// issue txs to all nodes, because XSVM does not have mempool gossiping
	for i, node := range nodes {
		client = api.NewClient(node.GetAccessibleURI(), chain.ChainID.String())
		_, err := client.IssueTx(tc.DefaultContext(), stx)
		require.NoError(err, "node %d failed to issue tx", i)
	}

	tc.Log().Info("successfully issued XSVM transfer transactions",
		zap.Stringer("txID", txID),
		zap.Uint64("round", round),
	)

	tc.By("checking all nodes have accepted the tx")
	address := chain.PreFundedKey.Address()
	for _, node := range nodes {
		client = api.NewClient(node.GetAccessibleURI(), chain.ChainID.String())
		err = api.AwaitTxAccepted(tc.DefaultContext(), client, address, nonces[node.NodeID], api.DefaultPollingInterval)
		require.NoError(err, "node %s failed to accept tx in round %d", node.NodeID, round)

		balance, err := client.Balance(tc.DefaultContext(), recipientKey.Address(), chain.ChainID)
		require.NoError(err)
		require.Equal(units.Schmeckle, balance)

		_, statelessBlock, err := client.LastAccepted(tc.DefaultContext())
		require.NoError(err)

		require.Len(statelessBlock.Txs, 1)
		require.Equal(statelessBlock.Txs[0], stx)

		tc.Log().Info("node has accepted the tx",
			zap.Stringer("node", node.NodeID),
			zap.Stringer("txID", txID),
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

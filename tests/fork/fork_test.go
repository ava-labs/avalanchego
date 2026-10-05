// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fork

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"math/big"
	"net"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/ava-labs/libevm/core/types"
	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/fork"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/vms/platformvm/status"
	"github.com/ava-labs/avalanchego/vms/secp256k1fx"

	ethereum "github.com/ava-labs/libevm"
	ethcommon "github.com/ava-labs/libevm/common"
)

func TestFork(t *testing.T) {
	ginkgo.RunSpecs(t, "fork test suite")
}

const (
	// numSourceNodes matches the spec's five-validator source network.
	numSourceNodes = 5
	numForkNodes   = 3
	// forkLeadTime must cover starting and bootstrapping the fork nodes.
	forkLeadTime = 3 * time.Minute
	gracePeriod  = 20 * time.Second
)

var avalancheGoExecPath string

func init() {
	flag.StringVar(&avalancheGoExecPath, "avalanchego-path", "", "avalanchego executable path")
}

var _ = ginkgo.Describe("[Fork]", func() {
	tc := e2e.NewTestContext()

	ginkgo.It("forks a private network off a source network", func() {
		network := tmpnet.NewDefaultNetwork("avalanchego-fork")
		network.Nodes = tmpnet.NewNodesOrPanic(numSourceNodes)
		network.DefaultRuntimeConfig = tmpnet.NodeRuntimeConfig{
			Process: &tmpnet.ProcessRuntimeConfig{AvalancheGoPath: avalancheGoExecPath},
		}
		e2e.StartNetwork(tc, network, "" /* rootNetworkDir */, 0 /* shutdownDelay */, e2e.EmptyNetworkCmd)

		tc.By("preparing fork nodes")
		forkNodes := make([]*tmpnet.Node, numForkNodes)
		vdrs := make([]fork.Validator, numForkNodes)
		for i := range forkNodes {
			node := tmpnet.NewEphemeralNode(tmpnet.FlagsMap{})
			require.NoError(tc, node.EnsureKeys(), "EnsureKeys()")
			pop, err := node.GetProofOfPossession()
			require.NoError(tc, err, "GetProofOfPossession()")
			port := freePort(tc)
			node.Flags[config.StakingPortKey] = strconv.Itoa(int(port))
			vdrs[i] = fork.Validator{
				NodeID: node.NodeID,
				Weight: 100,
				Signer: pop,
				IP:     netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), port),
			}
			forkNodes[i] = node
		}
		forkTime := time.Now().Add(forkLeadTime).Truncate(time.Second)
		forkCfg, err := fork.New(forkTime, gracePeriod, vdrs)
		require.NoError(tc, err, "fork.New()")
		forkCfgJSON, err := json.Marshal(forkCfg)
		require.NoError(tc, err, "json.Marshal(forkCfg)")

		tc.By("starting fork nodes before the fork time")
		for _, node := range forkNodes {
			node.Flags[config.ForkConfigFileContentKey] = base64.StdEncoding.EncodeToString(forkCfgJSON)
			e2e.AddEphemeralNode(tc, network, node)
		}
		for _, node := range forkNodes {
			e2e.WaitForHealthy(tc, node)
		}
		require.True(tc, time.Now().Before(forkTime), "fork nodes bootstrapped after the fork time; increase forkLeadTime")

		tc.By("issuing transactions on the source network before the fork time")
		sourceURI := tmpnet.NodeURI{NodeID: network.Nodes[0].NodeID, URI: network.Nodes[0].URI}
		issueTxs(tc, sourceURI, network.PreFundedKeys[2])
		tc.Log().Info("pre-fork issuance complete", zap.Duration("marginBeforeForkTime", time.Until(forkTime)))
		require.True(tc, time.Now().Before(forkTime), "pre-fork issuance finished after the fork time; increase forkLeadTime")

		tc.By("waiting for every fork node to switch")
		for _, node := range forkNodes {
			tc.Eventually(func() bool {
				return getForkReport(tc, node).Phase == fork.Switched.String()
			}, forkLeadTime+gracePeriod+time.Minute, e2e.DefaultPollingInterval, "fork node did not switch")
		}

		tc.By("checking that fork nodes only peer with each other")
		forkIDs := forkCfg.NodeIDs()
		for _, node := range forkNodes {
			tc.Eventually(func() bool {
				return peersSubsetOf(tc, node, forkIDs)
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "fork node still has source-network peers")
		}

		tc.By("issuing transactions on the fork")
		forkURI := tmpnet.NodeURI{NodeID: forkNodes[0].NodeID, URI: forkNodes[0].URI}
		forkKey := network.PreFundedKeys[0]
		forkPTxID, forkXTxID, forkCTxHash := issueTxs(tc, forkURI, forkKey)

		tc.By("issuing transactions on the source network")
		sourceKey := network.PreFundedKeys[1]
		sourcePTxID, sourceXTxID, sourceCTxHash := issueTxs(tc, sourceURI, sourceKey)

		tc.By("checking that the networks diverged")
		requirePTxUnknown(tc, sourceURI.URI, forkPTxID)
		requirePTxUnknown(tc, forkURI.URI, sourcePTxID)
		requireXTxUnknown(tc, sourceURI.URI, forkXTxID)
		requireXTxUnknown(tc, forkURI.URI, sourceXTxID)
		requireCTxUnknown(tc, sourceURI, forkCTxHash)
		requireCTxUnknown(tc, forkURI, sourceCTxHash)

		tc.By("checking that all fork nodes agree on H_fork and the fork points")
		var want fork.Report
		tc.Eventually(func() bool {
			want = getForkReport(tc, forkNodes[0])
			return want.ForkHeight != nil && len(want.ForkPoints) == 3
		}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "fork node 0 did not record H_fork and three fork points")
		for chainID, p := range want.ForkPoints {
			tc.Log().Info("fork point", zap.String("chainID", chainID), zap.Stringer("blockID", p.BlockID), zap.Uint64("height", p.Height))
			require.Positive(tc, p.Height, "fork point of chain %s is genesis; pre-fork issuance did not land before the fork time", chainID)
		}
		for _, node := range forkNodes[1:] {
			tc.Eventually(func() bool {
				got := getForkReport(tc, node)
				return got.ForkHeight != nil && *got.ForkHeight == *want.ForkHeight && equalForkPoints(got.ForkPoints, want.ForkPoints)
			}, e2e.DefaultTimeout, e2e.DefaultPollingInterval, "fork node disagrees on the fork")
		}

		tc.By("restarting a fork node after the fork")
		restarted := forkNodes[numForkNodes-1]
		require.NoError(tc, restarted.Stop(tc.DefaultContext()), "Stop()")
		require.NoError(tc, network.StartNode(tc.DefaultContext(), restarted), "StartNode()")
		e2e.WaitForHealthy(tc, restarted)
		got := getForkReport(tc, restarted)
		require.Equal(tc, fork.Switched.String(), got.Phase, "restarted node phase")
		require.True(tc, equalForkPoints(got.ForkPoints, want.ForkPoints), "restarted node fork points")
		require.True(tc, peersSubsetOf(tc, restarted, forkIDs), "restarted node peers")
	})
})

func freePort(tc tests.TestContext) uint16 {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(tc, err, "net.Listen()")
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(tc, l.Close(), "Close()")
	return uint16(port)
}

func getForkReport(tc tests.TestContext, node *tmpnet.Node) fork.Report {
	reply, err := health.NewClient(node.URI).Health(tc.DefaultContext(), nil)
	require.NoError(tc, err, "health.Health()")
	result, ok := reply.Checks["fork"]
	require.True(tc, ok, "fork health check missing on %s", node.NodeID)
	b, err := json.Marshal(result.Details)
	require.NoError(tc, err, "json.Marshal(details)")
	var report fork.Report
	require.NoError(tc, json.Unmarshal(b, &report), "json.Unmarshal(report)")
	return report
}

func peersSubsetOf(tc tests.TestContext, node *tmpnet.Node, allowed set.Set[ids.NodeID]) bool {
	peers, err := info.NewClient(node.URI).Peers(tc.DefaultContext(), nil)
	require.NoError(tc, err, "info.Peers()")
	for _, p := range peers {
		if !allowed.Contains(p.ID) {
			return false
		}
	}
	return true
}

// issueTxs issues a P-chain, an X-chain, and a C-chain transaction against
// [uri] and returns the P-chain tx ID, the X-chain tx ID, and the C-chain tx
// hash.
func issueTxs(tc tests.TestContext, uri tmpnet.NodeURI, key *secp256k1.PrivateKey) (ids.ID, ids.ID, ethcommon.Hash) {
	wallet := e2e.NewWallet(tc, secp256k1fx.NewKeychain(key), uri)
	owner := secp256k1fx.OutputOwners{Threshold: 1, Addrs: []ids.ShortID{key.Address()}}

	pAssetID := wallet.P().Builder().Context().AVAXAssetID
	pTx, err := wallet.P().IssueBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{ID: pAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: units.Avax, OutputOwners: owner},
	}})
	require.NoError(tc, err, "P.IssueBaseTx()")

	xAssetID := wallet.X().Builder().Context().AVAXAssetID
	xTx, err := wallet.X().IssueBaseTx([]*avax.TransferableOutput{{
		Asset: avax.Asset{ID: xAssetID},
		Out:   &secp256k1fx.TransferOutput{Amt: units.Avax, OutputOwners: owner},
	}})
	require.NoError(tc, err, "X.IssueBaseTx()")

	ethClient := e2e.NewEthClient(tc, uri)
	nonce, err := ethClient.NonceAt(tc.DefaultContext(), key.EthAddress(), nil)
	require.NoError(tc, err, "NonceAt()")
	chainID, err := ethClient.ChainID(tc.DefaultContext())
	require.NoError(tc, err, "ChainID()")
	tx := types.NewTransaction(nonce, key.EthAddress(), big.NewInt(1), e2e.DefaultGasLimit, e2e.SuggestGasPrice(tc, ethClient), nil)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(chainID), key.ToECDSA())
	require.NoError(tc, err, "SignTx()")
	receipt := e2e.SendEthTransaction(tc, ethClient, signedTx)
	require.Equal(tc, types.ReceiptStatusSuccessful, receipt.Status, "C-chain receipt status")

	return pTx.ID(), xTx.ID(), signedTx.Hash()
}

func requirePTxUnknown(tc tests.TestContext, uri string, txID ids.ID) {
	resp, err := platformvm.NewClient(uri).GetTxStatus(tc.DefaultContext(), txID)
	require.NoError(tc, err, "GetTxStatus()")
	require.Equal(tc, status.Unknown, resp.Status, "P-chain tx %s status on %s", txID, uri)
}

func requireXTxUnknown(tc tests.TestContext, uri string, txID ids.ID) {
	// GetTxStatus reports only Accepted or Unknown, which is exactly the
	// distinction checked here.
	got, err := avm.NewClient(uri, "X").GetTxStatus(tc.DefaultContext(), txID)
	require.NoError(tc, err, "avm.GetTxStatus()")
	require.Equal(tc, choices.Unknown, got, "X-chain tx %s status on %s", txID, uri)
}

func requireCTxUnknown(tc tests.TestContext, uri tmpnet.NodeURI, hash ethcommon.Hash) {
	_, err := e2e.NewEthClient(tc, uri).TransactionReceipt(tc.DefaultContext(), hash)
	require.ErrorIs(tc, err, ethereum.NotFound, "C-chain tx %x receipt on %s", hash, uri.URI)
}

func equalForkPoints(a, b map[string]fork.ForkPoint) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

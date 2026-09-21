// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package membership exercises certificate-based subnet membership end to end.
//
// A private subnet declares a member CA. A node holding a certificate from that
// CA joins the subnet although it validates nothing and no other node has heard
// of it; a node presenting the stock self-signed certificate stays connected to
// the same validators and learns nothing about the subnet.
//
// The CA's signing key never leaves this process, so the suite builds its own
// network rather than sharing one across ginkgo processes.
package membership

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/config"
	"github.com/ava-labs/avalanchego/config/node"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking/stakingtest"
	"github.com/ava-labs/avalanchego/subnets"
	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/subnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet/flags"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/units"

	xsvmapi "github.com/ava-labs/avalanchego/vms/example/xsvm/api"
)

const (
	subnetName   = "private-subnet"
	caCommonName = "Test Member CA"

	// validatorCount is the fleet that is stood up once, before any
	// certificate is issued, and that is never touched again.
	validatorCount = 3

	// certValidity outlives the run, so that nothing here depends on the
	// expiry path.
	certValidity = time.Hour

	// elevatedFrameSize is what the subnet declares through largeMessages. A
	// deployment-scale value costs nothing, because the frame size only ever
	// bounds a read; no message here comes close to it.
	elevatedFrameSize = 160 * units.MiB

	// joinTimeout bounds how long a member may take to bootstrap the subnet
	// chain after it starts.
	joinTimeout = 2 * time.Minute

	// exclusionWindow is how long a non-member is watched failing to
	// bootstrap. A member joins well inside it, so a non-member still stuck at
	// the end of it is stuck because it is not a member.
	exclusionWindow = 30 * time.Second

	// refusalWindow is how long a refused connection is watched staying
	// refused. The outsider has been dialing for the whole exclusion window by
	// the time this runs, so it only has to catch a connection that flaps.
	refusalWindow = 10 * time.Second

	pollInterval = time.Second
)

func TestMembership(t *testing.T) {
	ginkgo.RunSpecs(t, "certificate-based subnet membership")
}

var runtimeConfigVars *flags.RuntimeConfigVars

func init() {
	runtimeConfigVars = flags.NewRuntimeConfigFlagVars()
}

var _ = ginkgo.Describe("[Membership]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("admits certificate holders to a private subnet and excludes everyone else", func() {
		nodeRuntimeConfig, err := runtimeConfigVars.GetNodeRuntimeConfig()
		require.NoError(err)

		tc.By("minting a member CA")
		memberCA, err := stakingtest.NewRootCA(caCommonName, certValidity)
		require.NoError(err)

		tc.By(fmt.Sprintf("starting %d validators of a private subnet that trusts the CA", validatorCount))
		network := tmpnet.NewDefaultNetwork("avalanchego-membership")
		network.Nodes = tmpnet.NewNodesOrPanic(validatorCount)
		network.DefaultRuntimeConfig = *nodeRuntimeConfig
		network.DefaultFlags = tmpnet.DefaultE2EFlags()

		chainKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		privateSubnet := subnet.NewXSVMOrPanic(subnetName, chainKey, network.Nodes...)
		// validatorOnly closes the subnet; memberCA is who may be let back in
		// without validating it.
		privateSubnet.Config = tmpnet.ConfigMap{
			"validatorOnly": true,
			"memberCA":      []string{string(memberCA.CertPEM())},
			"largeMessages": map[string]any{
				"maxMessageSize": elevatedFrameSize,
			},
		}
		network.Subnets = []*tmpnet.Subnet{privateSubnet}

		e2e.StartNetwork(tc, network, "" /* rootNetworkDir */, 0 /* shutdownDelay */, e2e.EmptyNetworkCmd)

		var (
			validators = network.Nodes
			subnetID   = privateSubnet.SubnetID
			chainID    = privateSubnet.Chains[0].ChainID
			observer   = validators[0]
		)
		tc.Log().Info("private subnet is running",
			zap.Stringer("subnetID", subnetID),
			zap.Stringer("chainID", chainID),
			zap.Stringers("validators", tmpnet.NodesToIDs(validators...)),
		)

		tc.By("confirming the subnet's own validators hold no certificate")
		// A validator's credential is its P-Chain entry, so the CA exists for
		// the nodes that have no on-chain identity. The fleet is the control:
		// stock self-signed certificates the member CA rejects, on nodes that
		// are members regardless.
		roots, err := subnets.ParseMemberCA(memberCA.CertPEM())
		require.NoError(err)
		for _, validator := range validators {
			chain := stakingCertChain(tc, validator)
			require.Len(chain, 1, "%s presents more than a self-signed certificate", validator.NodeID)
			_, certified := roots.VerifyUntil(chain)
			require.False(certified, "%s holds a certificate from the member CA", validator.NodeID)
		}
		awaitFrameSize(tc, observer, validators[1].NodeID, elevatedFrameSize)

		tc.By("issuing a certificate to a node that validates nothing")
		firstMember := newMemberNode(tc, memberCA, "member-1", subnetID)
		e2e.AddEphemeralNode(tc, network, firstMember)
		awaitSubnetChain(tc, firstMember, chainID)

		tc.By("reading private subnet state from the certificate holder")
		_, _, err = xsvmapi.NewClient(firstMember.GetAccessibleURI(), chainID.String()).
			LastAccepted(tc.ContextWithTimeout(tests.DefaultTimeout))
		require.NoError(err)

		tc.By("starting a node with a stock self-signed certificate")
		outsider := tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
			config.TrackSubnetsKey: subnetID.String(),
		})
		outsider.Flags.SetDefaults(bootstrapVia(tc, network, firstMember))
		e2e.AddEphemeralNode(tc, network, outsider)
		tc.Log().Info("started a node with no certificate",
			zap.Stringer("nodeID", outsider.NodeID),
		)

		tc.By("confirming the outsider is connected to the subnet's validators")
		awaitPeered(tc, observer, outsider.NodeID)

		tc.By("comparing the message stack a validator chose for each peer")
		requirePeerFrameSize(tc, observer, firstMember.NodeID, elevatedFrameSize)
		requirePeerFrameSize(tc, observer, outsider.NodeID, constants.DefaultMaxMessageSize)

		tc.By("confirming the validators drop the outsider's subnet messages")
		awaitDroppedMessages(tc, validators, outsider.NodeID)

		tc.By(fmt.Sprintf("confirming the outsider is still shut out after %s", exclusionWindow))
		requireNeverBootstraps(tc, outsider, chainID)
		requireUnanswered(tc, outsider, chainID)
		// The validators never disconnected the outsider, they only never
		// answered it - a subnet's validators withhold data, they do not close
		// connections. Re-checking here is what makes that a claim rather than
		// an assumption.
		requirePeerFrameSize(tc, observer, outsider.NodeID, constants.DefaultMaxMessageSize)

		tc.By("confirming the outsider's own view is not the one that governs")
		// The outsider tracks the subnet, so it builds an elevated stack and
		// selects it for every peer it reads as a member - which the subnet's
		// validators are. It only reaches that view once it has synced the
		// subnet's validator set, and it never gets the frame it selected,
		// because what a validator sends is decided by the validator. This is
		// why the outsider's own info.peers is the wrong place to read
		// membership.
		awaitFrameSize(tc, outsider, observer.NodeID, elevatedFrameSize)

		tc.By("recording the configuration and process of every validator")
		before := snapshotFleet(tc, validators)

		tc.By("onboarding a second member by issuing it a certificate, and nothing else")
		secondMember := newMemberNode(tc, memberCA, "member-2", subnetID)
		secondMember.Flags.SetDefaults(bootstrapVia(tc, network, firstMember))
		e2e.AddEphemeralNode(tc, network, secondMember)
		awaitSubnetChain(tc, secondMember, chainID)

		tc.By("confirming no validator was reconfigured or restarted to admit it")
		requireFleetUntouched(tc, validators, before)

		tc.By("confirming connection admission honours the certificate too")
		// Both nodes were configured to dial member-1 and neither validates
		// anything, so the certificate is the only thing between them. Without
		// the dial there would be nothing to refuse: a non-validator's IP is
		// never gossiped, so no peer finds one by itself.
		awaitPeered(tc, firstMember, secondMember.NodeID)
		requireNotPeered(tc, firstMember, outsider.NodeID)
	})
})

// newMemberNode issues a staking certificate from [ca] and returns a node that
// presents it and tracks [subnetID].
//
// The leaf and its issuer travel with the node as its staking certificate, so
// the chain a validator verifies is the one the TLS handshake already carries.
func newMemberNode(
	tc tests.TestContext,
	ca *stakingtest.CA,
	commonName string,
	subnetID ids.ID,
) *tmpnet.Node {
	certPEM, keyPEM, err := ca.IssueNodeCert(commonName, certValidity)
	require.NoError(tc, err)

	memberNode := tmpnet.NewEphemeralNode(tmpnet.FlagsMap{
		config.StakingCertContentKey:   base64.StdEncoding.EncodeToString(certPEM),
		config.StakingTLSKeyContentKey: base64.StdEncoding.EncodeToString(keyPEM),
		config.TrackSubnetsKey:         subnetID.String(),
		// A private fleet's non-validator nodes run with this on, which is the
		// third thing membership decides: without a certificate, a node that
		// is not a validator itself drops every peer that is not a validator
		// or a beacon.
		config.NetworkRequireValidatorToConnectKey: "true",
	})
	// The node ID is a hash of the leaf, so it only exists once the
	// certificate does.
	require.NoError(tc, memberNode.EnsureNodeID())

	tc.Log().Info("issued a staking certificate",
		zap.String("commonName", commonName),
		zap.Stringer("nodeID", memberNode.NodeID),
	)
	return memberNode
}

// awaitSubnetChain blocks until [chainID] reports bootstrapped on [node].
func awaitSubnetChain(tc tests.TestContext, node *tmpnet.Node, chainID ids.ID) {
	var (
		client = info.NewClient(node.GetAccessibleURI())
		ctx    = tc.ContextWithTimeout(joinTimeout)
	)
	tc.Eventually(func() bool {
		bootstrapped, err := client.IsBootstrapped(ctx, chainID.String())
		return err == nil && bootstrapped
	}, joinTimeout, pollInterval, fmt.Sprintf("%s never bootstrapped the private subnet's chain", node.NodeID))
}

// requireNeverBootstraps waits for [node] to know about [chainID] - proving it
// tracks the subnet and started the chain - and then requires that the chain
// stays unbootstrapped for [exclusionWindow].
func requireNeverBootstraps(tc tests.TestContext, node *tmpnet.Node, chainID ids.ID) {
	var (
		client = info.NewClient(node.GetAccessibleURI())
		ctx    = tc.ContextWithTimeout(joinTimeout + exclusionWindow)
	)
	tc.Eventually(func() bool {
		_, err := client.IsBootstrapped(ctx, chainID.String())
		return err == nil
	}, joinTimeout, pollInterval, fmt.Sprintf("%s never started the private subnet's chain", node.NodeID))

	for deadline := time.Now().Add(exclusionWindow); time.Now().Before(deadline); time.Sleep(pollInterval) {
		bootstrapped, err := client.IsBootstrapped(ctx, chainID.String())
		require.NoError(tc, err)
		require.False(tc, bootstrapped, "%s bootstrapped a subnet it is not a member of", node.NodeID)
	}
}

// awaitPeered blocks until [observer] reports a connection to [nodeID], and
// returns what it reports about it.
func awaitPeered(tc tests.TestContext, observer *tmpnet.Node, nodeID ids.NodeID) info.Peer {
	var (
		client = info.NewClient(observer.GetAccessibleURI())
		ctx    = tc.ContextWithTimeout(tests.DefaultTimeout)
		peer   info.Peer
	)
	tc.Eventually(func() bool {
		peers, err := client.Peers(ctx, []ids.NodeID{nodeID})
		if err != nil || len(peers) != 1 {
			return false
		}
		peer = peers[0]
		return true
	}, tests.DefaultTimeout, pollInterval, fmt.Sprintf("%s never connected to %s", observer.NodeID, nodeID))

	return peer
}

// bootstrapVia returns the bootstrap flags a node needs to dial [extra] on top
// of the network's own bootstrappers. A non-validator's IP is never gossiped,
// so without this nothing would ever try to connect to one, and a connection
// that was never attempted would prove nothing about admission.
func bootstrapVia(tc tests.TestContext, network *tmpnet.Network, extra *tmpnet.Node) tmpnet.FlagsMap {
	stakingAddress, cancel, err := extra.GetAccessibleStakingAddress(tc.ContextWithTimeout(tests.DefaultTimeout))
	require.NoError(tc, err)
	tc.DeferCleanup(cancel)

	ips, ids := network.GetBootstrapIPsAndIDs(nil)
	return tmpnet.FlagsMap{
		config.BootstrapIPsKey: strings.Join(append(ips, stakingAddress.String()), ","),
		config.BootstrapIDsKey: strings.Join(append(ids, extra.NodeID.String()), ","),
	}
}

// requireNotPeered requires that [observer] holds no connection to [nodeID]
// throughout [refusalWindow], having refused it at the handshake.
func requireNotPeered(tc tests.TestContext, observer *tmpnet.Node, nodeID ids.NodeID) {
	var (
		client = info.NewClient(observer.GetAccessibleURI())
		ctx    = tc.ContextWithTimeout(refusalWindow + tests.DefaultTimeout)
	)
	for deadline := time.Now().Add(refusalWindow); time.Now().Before(deadline); time.Sleep(pollInterval) {
		peers, err := client.Peers(ctx, []ids.NodeID{nodeID})
		require.NoError(tc, err)
		require.Empty(tc, peers, "%s accepted a connection from %s", observer.NodeID, nodeID)
	}
}

// stakingCertChain returns the certificate chain [node] presents to its peers,
// leaf first, read from the flag it was started with.
func stakingCertChain(tc tests.TestContext, node *tmpnet.Node) []*x509.Certificate {
	pemBytes, err := base64.StdEncoding.DecodeString(node.Flags[config.StakingCertContentKey])
	require.NoError(tc, err)

	var chain []*x509.Certificate
	for rest := pemBytes; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return chain
		}

		cert, err := x509.ParseCertificate(block.Bytes)
		require.NoError(tc, err)
		chain = append(chain, cert)
	}
}

// awaitFrameSize blocks until [observer] reports its connection to [nodeID] at
// [expected] bytes. Unlike a validator's verdict on a certificate, which is
// fixed at the handshake, a verdict that rests on the validator set only
// settles once that set has been synced.
func awaitFrameSize(tc tests.TestContext, observer *tmpnet.Node, nodeID ids.NodeID, expected uint32) {
	var (
		client = info.NewClient(observer.GetAccessibleURI())
		ctx    = tc.ContextWithTimeout(joinTimeout)
	)
	tc.Eventually(func() bool {
		peers, err := client.Peers(ctx, []ids.NodeID{nodeID})
		return err == nil && len(peers) == 1 && peers[0].MaxFrameSize == expected
	}, joinTimeout, pollInterval, fmt.Sprintf("%s never saw %s at a frame size of %d", observer.NodeID, nodeID, expected))
}

// requirePeerFrameSize requires that [observer] established its connection to
// [nodeID] on the stack membership selects. That stack is the verdict the
// observer reached while the TLS handshake still held the peer's certificate
// chain, and info.peers is the only place it is readable over an API.
func requirePeerFrameSize(tc tests.TestContext, observer *tmpnet.Node, nodeID ids.NodeID, expected uint32) {
	peer := awaitPeered(tc, observer, nodeID)
	require.Equal(tc, expected, peer.MaxFrameSize, "%s is on the wrong message stack", nodeID)
}

// responseOps are the subnet chain messages that only another peer can supply.
// Counting them is what states the exclusion in the feature's own terms: a
// non-member is not slow to bootstrap, it is never answered.
var responseOps = []string{"accepted_frontier", "accepted", "ancestors", "put", "chits"}

const handlerMessagesMetric = "avalanche_handler_messages"

func requireUnanswered(tc tests.TestContext, node *tmpnet.Node, chainID ids.ID) {
	handled := chainMessages(tc, node, chainID)

	for _, op := range responseOps {
		require.Zero(tc, handled[op], "%s was sent a %s for a subnet it is not a member of", node.NodeID, op)
	}

	var timedOut float64
	for op, count := range handled {
		if strings.HasSuffix(op, "_failed") {
			timedOut += count
		}
	}
	require.NotZero(tc, timedOut, "%s never even asked for the subnet's state", node.NodeID)
}

// chainMessages returns how many messages [node]'s handler for [chainID] has
// taken, per message op.
func chainMessages(tc tests.TestContext, node *tmpnet.Node, chainID ids.ID) map[string]float64 {
	families, err := metrics.NewClient(node.GetAccessibleURI()).
		GetMetrics(tc.ContextWithTimeout(tests.DefaultTimeout))
	require.NoError(tc, err)

	handled := map[string]float64{}
	for _, metric := range families[handlerMessagesMetric].GetMetric() {
		var op string
		forThisChain := false
		for _, label := range metric.GetLabel() {
			switch label.GetName() {
			case "chain":
				forThisChain = label.GetValue() == chainID.String()
			case "op":
				op = label.GetValue()
			}
		}
		if forThisChain {
			handled[op] = metric.GetCounter().GetValue()
		}
	}
	return handled
}

// awaitDroppedMessages blocks until one of [validators] logs that it dropped a
// message from [nodeID], which is the enforcement point itself: the chain
// router refuses to hand a non-member's message to the subnet's handler.
func awaitDroppedMessages(tc tests.TestContext, validators []*tmpnet.Node, nodeID ids.NodeID) {
	const marker = "received message from non-allowed node"

	tc.Eventually(func() bool {
		for _, validator := range validators {
			if logContains(validator, nodeID.String(), marker) {
				return true
			}
		}
		return false
	}, exclusionWindow, pollInterval, fmt.Sprintf("no validator reported dropping a message from %s", nodeID))
}

// logContains reports whether any line of [node]'s main log holds all of
// [substrings]. A log that cannot be read yet simply holds nothing.
func logContains(node *tmpnet.Node, substrings ...string) bool {
	logBytes, err := os.ReadFile(filepath.Join(node.DataDir, "logs", "main.log"))
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(logBytes), "\n") {
		matched := true
		for _, substring := range substrings {
			if !strings.Contains(line, substring) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// fleetState is what onboarding must leave alone: the flags a node was started
// with - which carry the subnet configuration, and so the member CA and the
// allowedNodes list - and the process reading them.
type fleetState struct {
	flags string
	pid   int
}

func snapshotFleet(tc tests.TestContext, validators []*tmpnet.Node) map[ids.NodeID]fleetState {
	snapshot := make(map[ids.NodeID]fleetState, len(validators))
	for _, validator := range validators {
		snapshot[validator.NodeID] = readFleetState(tc, validator)
	}
	return snapshot
}

func requireFleetUntouched(
	tc tests.TestContext,
	validators []*tmpnet.Node,
	before map[ids.NodeID]fleetState,
) {
	for _, validator := range validators {
		after := readFleetState(tc, validator)
		require.Equal(tc, before[validator.NodeID].flags, after.flags,
			"%s was reconfigured to admit a new member", validator.NodeID)
		require.Equal(tc, before[validator.NodeID].pid, after.pid,
			"%s was restarted to admit a new member", validator.NodeID)
	}
}

func readFleetState(tc tests.TestContext, validator *tmpnet.Node) fleetState {
	flagsBytes, err := os.ReadFile(validator.GetFlagsPath())
	require.NoError(tc, err)

	processBytes, err := os.ReadFile(filepath.Join(validator.DataDir, config.DefaultProcessContextFilename))
	require.NoError(tc, err)
	processContext := node.ProcessContext{}
	require.NoError(tc, json.Unmarshal(processBytes, &processContext))

	return fleetState{
		flags: string(flagsBytes),
		pid:   processContext.PID,
	}
}

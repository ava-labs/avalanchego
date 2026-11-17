// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package membership exercises certificate-based subnet membership end to end.
//
// A private subnet declares a membership CA. A node holding a certificate from that
// CA joins the subnet although it validates nothing and no other node has heard
// of it; a node presenting the stock self-signed certificate stays connected to
// the same validators and learns nothing about the subnet. Once a revocation
// list naming a holder's certificate is in the subnet config, that holder is
// treated like the stock node.
//
// The subnet's validators must start with a membership CA this spec mints, and the
// CA's signing key never leaves this process, so the spec starts a private
// network rather than using the suite's shared one.
package membership

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"

	xsvmapi "github.com/ava-labs/avalanchego/vms/example/xsvm/api"
)

const (
	subnetName          = "private-subnet"
	caCommonName        = "Test Membership CA"
	issuingCACommonName = "Test Nodes CA"

	// validatorCount is the fleet that is stood up once, before any
	// certificate is issued, and that is never touched again.
	validatorCount = 3

	// certValidity outlives the run, so that nothing here depends on the
	// expiry path.
	certValidity = time.Hour

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

// Serial because the private network adds six nodes beside the shared one.
var _ = ginkgo.Describe("[Membership]", ginkgo.Label("membership"), ginkgo.Serial, func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("admits certificate holders to a private subnet, excludes everyone else and evicts a revoked holder", func() {
		env := e2e.GetEnv(tc)
		sharedNetwork := env.GetNetwork()
		if sharedNetwork.DefaultRuntimeConfig.Kube != nil {
			ginkgo.Skip("the fleet check reads the process context of local nodes, which kube-hosted nodes lack")
		}

		tc.By("minting a membership CA and the intermediate that issues node certificates")
		membershipCA := stakingtest.NewRootCA(tc, caCommonName, certValidity)
		// The subnet trusts only the root, as a deployment does.
		issuingCA := membershipCA.NewIntermediateCA(tc, issuingCACommonName, certValidity)

		tc.By(fmt.Sprintf("starting %d validators of a private subnet that trusts the CA", validatorCount))
		network := tmpnet.NewDefaultNetwork("avalanchego-e2e-membership")
		network.Nodes = tmpnet.NewNodesOrPanic(validatorCount)
		// The suite's defaults carry settings like the upgrade config.
		network.DefaultFlags = tmpnet.FlagsMap{}
		network.DefaultFlags.SetDefaults(sharedNetwork.DefaultFlags)

		chainKey, err := secp256k1.NewPrivateKey()
		require.NoError(err)
		privateSubnet := subnet.NewXSVMOrPanic(subnetName, chainKey, network.Nodes...)
		// validatorOnly closes the subnet; membershipCA is who may be let back in
		// without validating it.
		privateSubnet.Config = tmpnet.ConfigMap{
			"validatorOnly": true,
			"membershipCA":  []string{string(membershipCA.CertPEM())},
		}
		network.Subnets = []*tmpnet.Subnet{privateSubnet}

		env.StartPrivateNetwork(network)
		// The shared network's metrics link would point at the wrong network.
		e2e.EmitMetricsLink = false

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
		// stock self-signed certificates the membership CA rejects, on nodes that
		// are members regardless.
		roots, err := subnets.ParseMembershipCA(membershipCA.CertPEM())
		require.NoError(err)
		for _, validator := range validators {
			chain := stakingCertChain(tc, validator)
			require.Len(chain, 1, "%s presents more than a self-signed certificate", validator.NodeID)
			_, certified := roots.Certify(chain)
			require.False(certified, "%s holds a certificate from the membership CA", validator.NodeID)
		}

		tc.By("issuing a certificate to a node that validates nothing")
		firstMember := newMemberNode(tc, issuingCA, "member-1", subnetID)
		e2e.AddEphemeralNode(tc, network, firstMember)
		awaitSubnetChain(tc, firstMember, chainID)

		tc.By("reading private subnet state from the certificate holder")
		_, _, err = xsvmapi.NewClient(firstMember.GetAccessibleURI(), chainID.String()).
			LastAccepted(tc.ContextWithTimeout(tests.DefaultTimeout))
		require.NoError(err)

		tc.By("starting a node with a stock self-signed certificate")
		droppedBefore := droppedUnallowed(tc, validators)
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

		tc.By("confirming the validators drop the outsider's subnet messages")
		awaitDroppedUnallowed(tc, validators, droppedBefore)

		tc.By(fmt.Sprintf("confirming the outsider is still shut out after %s", exclusionWindow))
		requireNeverBootstraps(tc, outsider, chainID)
		requireUnanswered(tc, outsider, chainID)
		// The validators never disconnected the outsider, they only never
		// answered it - a subnet's validators withhold data, they do not close
		// connections. Re-checking here is what makes that a claim rather than
		// an assumption.
		awaitPeered(tc, observer, outsider.NodeID)

		tc.By("recording the configuration and process of every validator")
		before := snapshotFleet(tc, validators)

		tc.By("onboarding a second member by issuing it a certificate, and nothing else")
		secondMember := newMemberNode(tc, issuingCA, "member-2", subnetID)
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

		tc.By("revoking the second member's certificate in the subnet config")
		crlPEM := issuingCA.CRLPEM(tc, 1, certValidity, stakingCertChain(tc, secondMember)[0])
		privateSubnet.Config["membershipCRL"] = []string{string(crlPEM)}

		tc.By("restarting the validators and the first member to load the list")
		// The subnet config is read at startup, as every other part of it is.
		restartNodes(tc, append(slices.Clone(validators), firstMember)...)

		tc.By("restarting the second member, which now has to bootstrap again")
		// It keeps its certificate. Having synced the chain before proves it
		// was a member; what it needs now is answers from validators that hold
		// the list. Its bootstrap flags still name the addresses the restarted
		// nodes listened on before, so they are taken afresh.
		for key, value := range bootstrapVia(tc, network, firstMember) {
			secondMember.Flags[key] = value
		}
		require.NoError(secondMember.Restart(tc.DefaultContext()))

		tc.By(fmt.Sprintf("confirming the revoked member is shut out like the outsider for %s", exclusionWindow))
		requireNeverBootstraps(tc, secondMember, chainID)
		requireUnanswered(tc, secondMember, chainID)

		tc.By("confirming connection admission refuses the revoked certificate")
		// The second member still dials the first, its bootstrapper, so this is
		// a refusal and not a dial that was never made.
		requireNotPeered(tc, firstMember, secondMember.NodeID)
	})
})

// restartNodes restarts [nodes] one at a time, so the subnet keeps a quorum,
// and waits for all of them to report healthy.
func restartNodes(tc tests.TestContext, nodes ...*tmpnet.Node) {
	ctx := tc.ContextWithTimeout(time.Duration(len(nodes)) * tests.DefaultTimeout)
	for _, node := range nodes {
		require.NoError(tc, node.Restart(ctx))
		require.NoError(tc, tmpnet.WaitForHealthyNodes(ctx, tc.Log(), []*tmpnet.Node{node}))
	}
}

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
	certPEM, keyPEM := ca.IssueNodeCert(tc, commonName, certValidity)

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

// awaitPeered blocks until [observer] reports a connection to [nodeID].
func awaitPeered(tc tests.TestContext, observer *tmpnet.Node, nodeID ids.NodeID) {
	var (
		client = info.NewClient(observer.GetAccessibleURI())
		ctx    = tc.ContextWithTimeout(tests.DefaultTimeout)
	)
	tc.Eventually(func() bool {
		peers, err := client.Peers(ctx, []ids.NodeID{nodeID})
		return err == nil && len(peers) == 1
	}, tests.DefaultTimeout, pollInterval, fmt.Sprintf("%s never connected to %s", observer.NodeID, nodeID))
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

// droppedUnallowedMetric counts the enforcement point itself: the chain router
// refusing to hand a non-member's message to a subnet's handler.
const droppedUnallowedMetric = "avalanche_requests_dropped_unallowed"

// awaitDroppedUnallowed blocks until one of [validators] has dropped a message
// from a non-member since [before] was taken. The outsider is the only
// non-member connected, so the increase is its traffic.
func awaitDroppedUnallowed(tc tests.TestContext, validators []*tmpnet.Node, before map[ids.NodeID]float64) {
	tc.Eventually(func() bool {
		for nodeID, dropped := range droppedUnallowed(tc, validators) {
			if dropped > before[nodeID] {
				return true
			}
		}
		return false
	}, exclusionWindow, pollInterval, "no validator dropped a message from a non-member")
}

func droppedUnallowed(tc tests.TestContext, validators []*tmpnet.Node) map[ids.NodeID]float64 {
	dropped := make(map[ids.NodeID]float64, len(validators))
	for _, validator := range validators {
		families, err := metrics.NewClient(validator.GetAccessibleURI()).
			GetMetrics(tc.ContextWithTimeout(tests.DefaultTimeout))
		require.NoError(tc, err)

		for _, metric := range families[droppedUnallowedMetric].GetMetric() {
			dropped[validator.NodeID] = metric.GetCounter().GetValue()
		}
	}
	return dropped
}

// fleetState is what onboarding must leave alone: the flags a node was started
// with - which carry the subnet configuration, and so the membership CA and the
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

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package interchain_test

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	_ "github.com/ava-labs/avalanchego/tests/bootstrap/interchain"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

func TestE2E(t *testing.T) {
	ginkgo.RunSpecs(t, "bootstrap e2e test suite")
}

var flagVars *e2e.FlagVars

func init() {
	flagVars = e2e.RegisterFlags(e2e.WithDefaultOwner("avalanchego-e2e-bootstrap"))
}

var _ = ginkgo.SynchronizedBeforeSuite(func() []byte {
	tc := e2e.NewEventHandlerTestContext()

	nodeCount, err := flagVars.NodeCount()
	require.NoError(tc, err)
	nodes := tmpnet.NewNodesOrPanic(nodeCount)

	upgrades := tmpnet.UpgradeConfig(flagVars.ActivateLatestAfter())
	tc.Log().Info("setting upgrades", zap.Reflect("upgrades", upgrades))

	defaultFlags, err := tmpnet.UpgradeFlags(upgrades)
	require.NoError(tc, err)
	defaultFlags.SetDefaults(tmpnet.DefaultE2EFlags())

	return e2e.NewTestEnvironment(
		tc,
		flagVars,
		&tmpnet.Network{
			Owner:        flagVars.NetworkOwner(),
			DefaultFlags: defaultFlags,
			Nodes:        nodes,
		},
	).Marshal()
}, func(envBytes []byte) {
	e2e.InitSharedTestEnvironment(e2e.NewTestContext(), envBytes)
})

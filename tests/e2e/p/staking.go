// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p

import (
	"time"

	"github.com/onsi/ginkgo/v2"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/pchain"
	"github.com/ava-labs/avalanchego/utils"
)

var _ = e2e.DescribePChain("[Staking]", func() {
	tc := e2e.NewTestContext()

	ginkgo.It("should add a validator that every node observes", func() {
		var (
			env     = e2e.GetEnv(tc)
			nodeURI = env.GetRandomNodeURI()
			wallet  = e2e.NewWallet(tc, env.NewKeychain(), nodeURI)
		)
		pchain.AddValidator(
			tc,
			wallet.P(),
			e2e.NewPChainNode(nodeURI),
			e2e.NewPChainNodes(env.GetNetwork()),
			ids.NodeID(utils.RandomBytes(ids.NodeIDLen)),
			e2e.NewPrivateKey(tc).Address(),
			e2e.NewPrivateKey(tc).Address(),
		)
	})

	ginkgo.It("should add a delegator that every node observes", func() {
		var (
			env     = e2e.GetEnv(tc)
			nodeURI = env.GetRandomNodeURI()
			wallet  = e2e.NewWallet(tc, env.NewKeychain(), nodeURI)
		)
		pchain.AddDelegator(
			tc,
			wallet.P(),
			e2e.NewPChainNode(nodeURI),
			e2e.NewPChainNodes(env.GetNetwork()),
			nodeURI.NodeID,
			time.Time{},
			e2e.NewPrivateKey(tc).Address(),
		)
	})
})

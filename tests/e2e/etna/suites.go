// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements tests for the banff network upgrade.
package banff

import (
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/upgrade"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("[Etna]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can detect if Etna is activated",
		func() {
			env := e2e.GetEnv(tc)
			infoClient := info.NewClient(env.GetRandomNodeURI().URI)

			var upgrades *upgrade.Config
			tc.By("get upgrade config", func() {
				var err error
				upgrades, err = infoClient.Upgrades(tc.DefaultContext())
				require.NoError(err)
			})

			now := time.Now()
			if !upgrades.IsEtnaActivated(now) {
				tc.Outf("{{green}}Etna is not activated{{/}}: %s (now) < %s (EtnaTime)\n", now, upgrades.EtnaTime)
				return
			}

			tc.Outf("{{green}}Etna is activated{{/}}: %s (now) >= %s (EtnaTime)\n", now, upgrades.EtnaTime)
		})
})

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements tests for the etna network upgrade.
package etna

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
)

var _ = ginkgo.Describe("[Etna]", func() {
	tc := e2e.NewTestContext()
	require := require.New(tc)

	ginkgo.It("can detect if Etna is activated",
		func() {
			env := e2e.GetEnv(tc)
			infoClient := info.NewClient(env.GetRandomNodeURI().URI)

			tc.By("get upgrade config")
			upgrades, err := infoClient.Upgrades(tc.DefaultContext())
			require.NoError(err)

			now := time.Now()
			if !upgrades.IsEtnaActivated(now) {
				tc.Outf("{{green}}Etna is not activated{{/}}: %s (now) < %s (EtnaTime)\n", now, upgrades.EtnaTime)
				return
			}

			tc.Outf("{{green}}Etna is activated{{/}}: %s (now) >= %s (EtnaTime)\n", now, upgrades.EtnaTime)
		})
})

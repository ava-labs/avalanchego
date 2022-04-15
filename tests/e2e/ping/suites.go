// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements ping tests, requires network-runner cluster.
package ping

import (
	"context"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"

	"github.com/onsi/gomega"

	"github.com/chain4travel/caminogo/tests/e2e"
)

var _ = e2e.DescribeLocal("[Ping]", func() {
	ginkgo.It("can ping network-runner RPC server", func() {
		if e2e.GetRunnerGRPCEndpoint() == "" {
			ginkgo.Skip("no local network-runner, skipping")
		}

		runnerCli := e2e.GetRunnerClient()
		gomega.Expect(runnerCli).ShouldNot(gomega.BeNil())

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := runnerCli.Ping(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
	})
})

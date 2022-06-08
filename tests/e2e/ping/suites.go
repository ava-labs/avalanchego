// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Implements ping tests, requires network-runner cluster.
package ping

import (
	"context"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	"github.com/ava-labs/subnet-evm/tests/e2e"
	"github.com/ava-labs/subnet-evm/tests/e2e/runner"
)

var _ = e2e.DescribeLocal("[Ping]", func() {
	ginkgo.It("can ping network-runner RPC server", func() {
		runnerCli := runner.GetClient()
		gomega.Expect(runnerCli).ShouldNot(gomega.BeNil())

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		_, err := runnerCli.Ping(ctx)
		cancel()
		gomega.Expect(err).Should(gomega.BeNil())
	})
})

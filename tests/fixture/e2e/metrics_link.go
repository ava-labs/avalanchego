// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"strconv"
	"time"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"

	ginkgo "github.com/onsi/ginkgo/v2"
)

// The ginkgo event handlers defined in this file will be automatically
// applied to all ginkgo suites importing this package.

// Whether a spec-scoped metrics link should be emitted after the current
// spec finishes executing.
var EmitMetricsLink bool

// This event handler ensures that by default a spec-scoped metrics link
// will be emitted at the end of spec execution. If the test uses a
// private network, it can disable this behavior by setting
// EmitMetricsLink to false.
//
// TODO(marun) Make this conditional on metrics collection being enabled
var _ = ginkgo.BeforeEach(func() {
	EmitMetricsLink = true
})

// This event handler attempts to emit a metrics link scoped to the duration
// of the current spec.
//
// TODO(marun) Make this conditional on metrics collection being enabled
var _ = ginkgo.AfterEach(func() {
	tc := NewTestContext()
	env := GetEnv(tc)
	if env == nil || !EmitMetricsLink {
		return
	}

	specReport := ginkgo.CurrentSpecReport()
	startTime := specReport.StartTime.UnixMilli()
	// Extend the end time by the shutdown delay (a proxy for the metrics
	// scrape interval) to maximize the chances of the specified duration
	// including all metrics relevant to the current spec.
	endTime := time.Now().Add(networkShutdownDelay).UnixMilli()
	metricsLink := tmpnet.MetricsLinkForNetwork(
		env.GetNetwork().UUID,
		strconv.FormatInt(startTime, 10),
		strconv.FormatInt(endTime, 10),
	)
	tc.Outf("Test Metrics: %s\n", metricsLink)
})

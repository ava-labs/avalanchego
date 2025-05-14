// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package e2e

import (
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
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
var _ = ginkgo.BeforeEach(func() {
	EmitMetricsLink = true
})

// This event handler attempts to emit a metrics link scoped to the duration
// of the current spec.
var _ = ginkgo.AfterEach(func() {
	tc := NewTestContext()
	env := GetEnv(tc)
	// The global env isn't guaranteed to be initialized by importers
	// of this package since initializing a package-local env is also
	// supported.
	if env == nil || !EmitMetricsLink {
		return
	}

	specReport := ginkgo.CurrentSpecReport()
	startTimeMs := specReport.StartTime.UnixMilli()
	metricsLink := MetricsLink(startTimeMs)
	tc.Log().Info(tmpnet.MetricsAvailableMessage,
		zap.String("uri", metricsLink),
	)
})

// MetricsLink generates a URL spanning from provided start time to now plus shutdown delay to capture all relevant metrics for the Grafana dashboard
func MetricsLink(startTimeMs int64) string {
	// Extend the end time by the shutdown delay (a proxy for the metrics
	// scrape interval) to maximize the chances of the specified duration
	// including all metrics relevant to the current spec.
	endTimeMs := time.Now().Add(tmpnet.NetworkShutdownDelay).UnixMilli()
	metricsLink := tmpnet.MetricsLinkForNetwork(
		env.GetNetwork().UUID,
		strconv.FormatInt(startTimeMs, 10),
		strconv.FormatInt(endTimeMs, 10),
	)

	return metricsLink
}

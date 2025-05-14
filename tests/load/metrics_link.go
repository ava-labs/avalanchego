// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"strconv"
	"time"

	"github.com/onsi/ginkgo/v2"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
)

const (
	dashboardID   = "eabddd1d-0a06-4ba1-8e68-a44504e37535"
	dashboardName = "C-Chain Load"
)

var (
	// Disable default metrics link generation to prevent duplicate links.
	// We generate load specific links.
	_ = ginkgo.JustBeforeEach(func() {
		e2e.EmitMetricsLink = false
	})

	_ = ginkgo.AfterEach(func() {
		tc := e2e.NewTestContext()
		env := e2e.GetEnv(tc)

		if env == nil {
			return
		}

		specReport := ginkgo.CurrentSpecReport()
		startTimeMs := specReport.StartTime.UnixMilli()

		metricsLink := tmpnet.CustomMetricsLinkForNetwork(
			env.GetNetwork().UUID,
			strconv.FormatInt(startTimeMs, 10),
			strconv.FormatInt(time.Now().Add(tmpnet.NetworkShutdownDelay).UnixMilli(), 10),
			tmpnet.WithDashboard(dashboardID, dashboardName),
			tmpnet.WithoutEphemeralNodeFilter(),
		)

		tc.Log().Info(tmpnet.MetricsAvailableMessage,
			zap.String("uri", metricsLink),
		)
	})
)

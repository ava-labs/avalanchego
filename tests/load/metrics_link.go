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

		grafanaLink := tmpnet.BuildMonitoringURLForNetwork(dashboardID, dashboardName, env.GetNetwork().UUID, tmpnet.GrafanaFilterOptions{
			StartTime: strconv.FormatInt(startTimeMs, 10),
			EndTime:   strconv.FormatInt(time.Now().Add(tmpnet.NetworkShutdownDelay).UnixMilli(), 10),
		})

		tc.Log().Info(tmpnet.MetricsAvailableMessage,
			zap.String("uri", grafanaLink),
		)
	})
)

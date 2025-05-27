// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/e2e"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	dashboardID   = "eabddd1d-0a06-4ba1-8e68-a44504e37535"
	dashboardName = "C-Chain Load"
)

func GenerateMetricsLink(env *e2e.TestEnvironment, log logging.Logger, startTime time.Time) {
	grafanaLink := tmpnet.BuildMonitoringURLForNetwork(dashboardID, dashboardName, env.GetNetwork().UUID, tmpnet.GrafanaFilterOptions{
		StartTime: strconv.FormatInt(startTime.UnixMilli(), 10),
		EndTime:   strconv.FormatInt(time.Now().Add(tmpnet.NetworkShutdownDelay).UnixMilli(), 10),
	})

	log.Info(tmpnet.MetricsAvailableMessage,
		zap.String("uri", grafanaLink),
	)
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package reexecute

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests"
	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// startServer starts a Prometheus server for the provided gatherer and returns
// the server address.
func startServer(
	tb testing.TB,
	log logging.Logger,
	gatherer prometheus.Gatherer,
	port uint64,
) string {
	r := require.New(tb)

	server, err := tests.NewPrometheusServerWithPort(gatherer, port)
	r.NoError(err)

	log.Info("metrics endpoint available",
		zap.String("url", fmt.Sprintf("http://%s/ext/metrics", server.Address())),
	)

	tb.Cleanup(func() {
		r.NoError(server.Stop())
	})

	return server.Address()
}

// startCollector starts a Prometheus collector configured to scrape the server
// listening on serverAddr. startCollector also attaches the provided labels +
// Github labels if available to the collected metrics.
func startCollector(
	tb testing.TB,
	log logging.Logger,
	name string,
	labels map[string]string,
	serverAddr string,
	networkUUID string,
	dashboardPath string,
) {
	r := require.New(tb)

	startPromCtx, cancel := context.WithTimeout(tb.Context(), tests.DefaultTimeout)
	defer cancel()

	logger := tests.NewDefaultLogger("prometheus")
	r.NoError(tmpnet.StartPrometheus(startPromCtx, logger))

	var sdConfigFilePath string
	tb.Cleanup(func() {
		// Ensure a final metrics scrape.
		// This default delay is set above the default scrape interval used by StartPrometheus.
		time.Sleep(tmpnet.NetworkShutdownDelay)

		r.NoError(func() error {
			if sdConfigFilePath != "" {
				return os.Remove(sdConfigFilePath)
			}
			return nil
		}(),
		)

		//nolint:usetesting // t.Context() is already canceled inside the cleanup function
		checkMetricsCtx, cancel := context.WithTimeout(context.Background(), tests.DefaultTimeout)
		defer cancel()
		r.NoError(tmpnet.CheckMetricsExist(checkMetricsCtx, logger, networkUUID))
	})

	sdConfigFilePath, err := tmpnet.WritePrometheusSDConfig(name, tmpnet.SDConfig{
		Targets: []string{serverAddr},
		Labels:  labels,
	}, true /* withGitHubLabels */)
	r.NoError(err)

	var (
		grafanaURI = tmpnet.DefaultBaseGrafanaURI + dashboardPath
		startTime  = strconv.FormatInt(time.Now().UnixMilli(), 10)
	)

	log.Info("metrics available via grafana",
		zap.String(
			"url",
			tmpnet.NewGrafanaURI(networkUUID, startTime, "", grafanaURI),
		),
	)
}

type consensusMetrics struct {
	lastAcceptedHeight prometheus.Gauge
}

// newConsensusMetrics creates a subset of the metrics from snowman consensus
// [engine](../../snow/engine/snowman/metrics.go).
//
// The registry passed in is expected to be registered with the prefix
// "avalanche_snowman" and the chain label (ex. chain="C") that would be handled
// by the[chain manager](../../../chains/manager.go).
func newConsensusMetrics(registry prometheus.Registerer) (*consensusMetrics, error) {
	m := &consensusMetrics{
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "last_accepted_height",
			Help: "last height accepted",
		}),
	}
	if err := registry.Register(m.lastAcceptedHeight); err != nil {
		return nil, fmt.Errorf("failed to register last accepted height metric: %w", err)
	}
	return m, nil
}

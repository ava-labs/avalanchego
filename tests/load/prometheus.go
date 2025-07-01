// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/perms"
)

type MetricsServer struct {
	addr     string
	registry *prometheus.Registry
	server   http.Server
}

func NewPrometheusServer(addr string, registry *prometheus.Registry) *MetricsServer {
	return &MetricsServer{
		addr:     addr,
		registry: registry,
	}
}

func (*MetricsServer) String() string {
	return "metrics server"
}

func (s *MetricsServer) Start() (runError <-chan error, err error) {
	const metricsPattern = "/ext/metrics"

	mux := http.NewServeMux()
	handlerOpts := promhttp.HandlerOpts{Registry: s.registry}
	mux.Handle(metricsPattern, promhttp.HandlerFor(s.registry, handlerOpts))

	listener, err := net.Listen("tcp", s.addr)
	if err != nil {
		return nil, err
	}
	s.addr = listener.Addr().String()

	s.server = http.Server{
		Addr:              s.addr,
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
	}

	runErrorBiDirectional := make(chan error)
	runError = runErrorBiDirectional
	ready := make(chan struct{})
	go func() {
		close(ready)
		err = s.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		runErrorBiDirectional <- err
	}()
	<-ready

	return runError, nil
}

func (s *MetricsServer) Stop() (err error) {
	const shutdownTimeout = time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.server.Shutdown(shutdownCtx)
}

// GenerateMonitoringConfig generates and writes the Prometheus collector configuration
// so tmpnet can dynamically discover new scrape target via file-based service discovery
// It returns the collector file path.
func (s *MetricsServer) GenerateMonitoringConfig(log logging.Logger, monitoringLabels map[string]string) (string, error) {
	discoveryDir, err := tmpnet.GetPrometheusServiceDiscoveryDir()
	if err != nil {
		return "", fmt.Errorf("getting tmpnet service discovery directory: %w", err)
	}
	_, err = os.Stat(discoveryDir)
	if errors.Is(err, os.ErrNotExist) {
		log.Warn("The prometheus service discovery directory does not exist. Prometheus may not be running.",
			zap.String("dir", discoveryDir),
		)
		if err := os.MkdirAll(discoveryDir, perms.ReadWriteExecute); err != nil {
			return "", fmt.Errorf("failed to create service discovery dir: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("failed to stat service discovery dir: %w", err)
	}

	collectorFilePath := filepath.Join(discoveryDir, "load-test.json")
	config, err := json.MarshalIndent([]tmpnet.ConfigMap{
		{
			"targets": []string{s.addr},
			"labels":  monitoringLabels,
		},
	}, "", "  ")
	if err != nil {
		return "", err
	}

	return collectorFilePath, os.WriteFile(collectorFilePath, config, perms.ReadWrite)
}

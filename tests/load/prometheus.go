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

	"github.com/ava-labs/avalanchego/tests/fixture/tmpnet"
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

func (m *MetricsServer) Start() (runError <-chan error, err error) {
	const metricsPattern = "/ext/metrics"

	mux := http.NewServeMux()
	handlerOpts := promhttp.HandlerOpts{Registry: m.registry}
	mux.Handle(metricsPattern, promhttp.HandlerFor(m.registry, handlerOpts))

	listener, err := net.Listen("tcp", m.addr)
	if err != nil {
		return nil, err
	}
	m.addr = listener.Addr().String()

	m.server = http.Server{
		Addr:              m.addr,
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
	}

	runErrorBiDirectional := make(chan error)
	runError = runErrorBiDirectional
	ready := make(chan struct{})
	go func() {
		close(ready)
		err = m.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		runErrorBiDirectional <- err
	}()
	<-ready

	return runError, nil
}

func (m *MetricsServer) Stop() (err error) {
	const shutdownTimeout = time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return m.server.Shutdown(shutdownCtx)
}

// GenerateMonitoringConfig generates and writes the Prometheus collector configuration
// so tmpnet can dynamically discover new scrape target via file-based service discovery
// It returns the collector file path.
func (m *MetricsServer) GenerateMonitoringConfig(monitoringLabels map[string]string) (string, error) {
	discoveryDir, err := tmpnet.GetPrometheusServiceDiscoveryDir()
	if err != nil {
		return "", fmt.Errorf("getting tmpnet service discovery directory: %w", err)
	}

	collectorFilePath := filepath.Join(discoveryDir, "load-test.json")
	config, err := json.MarshalIndent([]tmpnet.ConfigMap{
		{
			"targets": []string{m.addr},
			"labels":  monitoringLabels,
		},
	}, "", "  ")
	if err != nil {
		return "", err
	}

	return collectorFilePath, os.WriteFile(collectorFilePath, config, perms.ReadWrite)
}

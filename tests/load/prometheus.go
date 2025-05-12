// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/ava-labs/avalanchego/utils/logging"
)

type MetricsServer struct {
	addr     string
	registry *prometheus.Registry
	server   http.Server
	logger   logging.Logger
}

func NewPrometheusServer(addr string, registry *prometheus.Registry, logger logging.Logger) *MetricsServer {
	return &MetricsServer{
		addr:     addr,
		registry: registry,
		logger:   logger,
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

	s.logger.Info(fmt.Sprintf("Metrics server available at http://%s%s", listener.Addr(), metricsPattern))

	return runError, nil
}

func (s *MetricsServer) Stop() (err error) {
	const shutdownTimeout = time.Second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	return s.server.Shutdown(shutdownCtx)
}

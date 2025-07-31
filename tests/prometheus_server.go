// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tests

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const defaultPrometheusListenAddr = "127.0.0.1:0"

// PrometheusServer is a HTTP server that serves Prometheus metrics from the provided
// gahterer.
// Listens on localhost with a dynamic port and serves metrics at /ext/metrics.
type PrometheusServer struct {
	gatherer prometheus.Gatherer
	server   http.Server
	errChan  chan error
}

// NewPrometheusServer creates and starts a Prometheus server with the provided gatherer
// listening on 127.0.0.1:0 and serving /ext/metrics.
func NewPrometheusServer(gatherer prometheus.Gatherer) (*PrometheusServer, error) {
	server := &PrometheusServer{
		gatherer: gatherer,
	}

	if err := server.start(); err != nil {
		return nil, err
	}

	return server, nil
}

// start the Prometheus server on a dynamic port.
func (s *PrometheusServer) start() error {
	mux := http.NewServeMux()
	mux.Handle("/ext/metrics", promhttp.HandlerFor(s.gatherer, promhttp.HandlerOpts{}))

	listener, err := net.Listen("tcp", defaultPrometheusListenAddr)
	if err != nil {
		return err
	}

	s.server = http.Server{
		Addr:              listener.Addr().String(),
		Handler:           mux,
		ReadHeaderTimeout: time.Second,
		ReadTimeout:       time.Second,
	}

	s.errChan = make(chan error, 1)
	go func() {
		err := s.server.Serve(listener)
		if !errors.Is(err, http.ErrServerClosed) {
			s.errChan <- err
		}
		close(s.errChan)
	}()

	return nil
}

// Stop gracefully shuts down the Prometheus server.
// Waits for the server to shut down and returns any error that occurred during shutdown.
func (s *PrometheusServer) Stop() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return errors.Join(
		s.server.Shutdown(shutdownCtx),
		<-s.errChan,
	)
}

// Address returns the address the server is listening on.
// If the server has not started, the address will be empty.
func (s *PrometheusServer) Address() string {
	return s.server.Addr
}

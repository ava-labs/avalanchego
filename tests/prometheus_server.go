// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

type PrometheusServer struct {
	gatherer prometheus.Gatherer
	server   http.Server
	errChan  chan error
}

// NewPrometheusServer creates a Prometheus server listening on 127.0.0.1:0 and serving /ext/metrics.
func NewPrometheusServer(gatherer prometheus.Gatherer) *PrometheusServer {
	return &PrometheusServer{
		gatherer: gatherer,
	}
}

func (s *PrometheusServer) Start() error {
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

func (s *PrometheusServer) Stop() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return errors.Join(
		s.server.Shutdown(shutdownCtx),
		<-s.errChan,
	)
}

// Address returns the address the server is listening on.
//
// If the server has not started, the address will be empty.
func (s *PrometheusServer) Address() string {
	return s.server.Addr
}

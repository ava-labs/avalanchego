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

type PrometheusServer struct {
	addr     string
	path     string
	gatherer prometheus.Gatherer
	server   http.Server
}

func NewPrometheusServer(addr string, path string, gatherer prometheus.Gatherer) *PrometheusServer {
	return &PrometheusServer{
		addr:     addr,
		path:     path,
		gatherer: gatherer,
	}
}

func (s *PrometheusServer) Start() (<-chan error, error) {
	mux := http.NewServeMux()
	mux.Handle(s.path, promhttp.HandlerFor(s.gatherer, promhttp.HandlerOpts{}))

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

	errChan := make(chan error)
	go func() {
		defer close(errChan)
		err := s.server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		errChan <- err
	}()

	return errChan, nil
}

func (s *PrometheusServer) Stop() error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	return s.server.Shutdown(shutdownCtx)
}

// Address returns the address the server is listening on.
//
// If the server has not started, the address will be empty.
func (s *PrometheusServer) Address() string {
	return s.addr
}

// URL returns the URL to request the prometheus metrics from the server.
func (s *PrometheusServer) URL() string {
	return "http://" + s.addr + s.path
}

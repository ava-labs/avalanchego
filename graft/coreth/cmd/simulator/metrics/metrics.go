// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"github.com/ava-labs/libevm/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	reg *prometheus.Registry
	// Summary of the quantiles of Individual Issuance Tx Times
	IssuanceTxTimes prometheus.Summary
	// Summary of the quantiles of Individual Confirmation Tx Times
	ConfirmationTxTimes prometheus.Summary
	// Summary of the quantiles of Individual Issuance To Confirmation Tx Times
	IssuanceToConfirmationTxTimes prometheus.Summary
}

func NewDefaultMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	return NewMetrics(registry)
}

// NewMetrics creates and returns a Metrics and registers it with a Collector
func NewMetrics(reg *prometheus.Registry) *Metrics {
	m := &Metrics{
		reg: reg,
		IssuanceTxTimes: prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "tx_issuance_time",
			Help:       "Individual Tx Issuance Times for a Load Test",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
		ConfirmationTxTimes: prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "tx_confirmation_time",
			Help:       "Individual Tx Confirmation Times for a Load Test",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
		IssuanceToConfirmationTxTimes: prometheus.NewSummary(prometheus.SummaryOpts{
			Name:       "tx_issuance_to_confirmation_time",
			Help:       "Individual Tx Issuance To Confirmation Times for a Load Test",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.99: 0.001},
		}),
	}
	reg.MustRegister(m.IssuanceTxTimes)
	reg.MustRegister(m.ConfirmationTxTimes)
	reg.MustRegister(m.IssuanceToConfirmationTxTimes)
	return m
}

type MetricsServer struct {
	cancel context.CancelFunc
	stopCh chan struct{}
}

func (m *Metrics) Serve(ctx context.Context, metricsPort string, metricsEndpoint string) *MetricsServer {
	ctx, cancel := context.WithCancel(ctx)
	// Create a prometheus server to expose individual tx metrics
	server := &http.Server{
		Addr: ":" + metricsPort,
	}

	// Start up go routine to listen for SIGINT notifications to gracefully shut down server
	go func() {
		// Blocks until signal is received
		<-ctx.Done()

		if err := server.Shutdown(ctx); err != nil {
			log.Error("Metrics server error: %v", err)
		}
		log.Info("Received a SIGINT signal: Gracefully shutting down metrics server")
	}()

	// Start metrics server
	ms := &MetricsServer{
		stopCh: make(chan struct{}),
		cancel: cancel,
	}
	go func() {
		defer close(ms.stopCh)

		http.Handle(metricsEndpoint, promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{Registry: m.reg}))
		log.Info(fmt.Sprintf("Metrics Server: localhost:%s%s", metricsPort, metricsEndpoint))
		if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			log.Error("Metrics server error: %v", err)
		}
	}()

	return ms
}

func (ms *MetricsServer) Shutdown() {
	ms.cancel()
	<-ms.stopCh
}

func (m *Metrics) Print(outputFile string) error {
	metrics, err := m.reg.Gather()
	if err != nil {
		return err
	}

	if outputFile == "" {
		// Printout to stdout
		fmt.Println("*** Metrics ***")
		for _, mf := range metrics {
			for _, m := range mf.GetMetric() {
				fmt.Printf("Type: %s, Name: %s, Description: %s, Values: %s\n", mf.GetType().String(), mf.GetName(), mf.GetHelp(), m.String())
			}
		}
		fmt.Println("***************")
	} else {
		jsonFile, err := os.Create(outputFile)
		if err != nil {
			return err
		}
		defer jsonFile.Close()

		if err := json.NewEncoder(jsonFile).Encode(metrics); err != nil {
			return err
		}
	}

	return nil
}

// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum/log"
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
	metricsPort     string
	metricsEndpoint string

	cancel context.CancelFunc
	stopCh chan struct{}
}

func (m *Metrics) Serve(ctx context.Context, metricsPort string, metricsEndpoint string) *MetricsServer {
	ctx, cancel := context.WithCancel(ctx)
	// Create a prometheus server to expose individual tx metrics
	server := &http.Server{
		Addr: fmt.Sprintf(":%s", metricsPort),
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
		metricsPort:     metricsPort,
		metricsEndpoint: metricsEndpoint,
		stopCh:          make(chan struct{}),
		cancel:          cancel,
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

func (ms *MetricsServer) Print() {
	// Get response from server
	resp, err := http.Get(fmt.Sprintf("http://localhost:%s%s", ms.metricsPort, ms.metricsEndpoint))
	if err != nil {
		log.Error("cannot get response from metrics servers", "err", err)
		return
	}
	// Read response body
	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Error("cannot read response body", "err", err)
		return
	}
	// Print out formatted individual metrics
	parts := strings.Split(string(respBody), "\n")
	for _, s := range parts {
		fmt.Printf("       \t\t\t%s\n", s)
	}
}

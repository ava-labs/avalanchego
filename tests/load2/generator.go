// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package load2

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/tests"
)

type Metrics struct {
	txsIssuedCounter      prometheus.Counter
	txsAcceptedCounter    prometheus.Counter
	txIssuanceLatency     prometheus.Histogram
	txConfirmationLatency prometheus.Histogram
	txTotalLatency        prometheus.Histogram
}

func NewMetrics(namespace string, registry *prometheus.Registry) (*Metrics, error) {
	m := &Metrics{
		txsIssuedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "txs_issued",
			Help:      "Number of transactions issued",
		}),
		txsAcceptedCounter: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "txs_confirmed",
			Help:      "Number of transactions confirmed",
		}),
		txIssuanceLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_issuance_latency",
			Help:      "Issuance latency of transactions",
		}),
		txConfirmationLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_confirmation_latency",
			Help:      "Confirmation latency of transactions",
		}),
		txTotalLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "tx_total_latency",
			Help:      "Total latency of transactions",
		}),
	}

	if err := errors.Join(
		registry.Register(m.txsIssuedCounter),
		registry.Register(m.txsAcceptedCounter),
		registry.Register(m.txIssuanceLatency),
		registry.Register(m.txConfirmationLatency),
		registry.Register(m.txTotalLatency),
	); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Metrics) Issue(d time.Duration) {
	m.txsIssuedCounter.Inc()
	m.txIssuanceLatency.Observe(float64(d.Milliseconds()))
}

func (m *Metrics) Accept(confirmationDuration time.Duration, totalDuration time.Duration) {
	m.txsAcceptedCounter.Inc()
	m.txTotalLatency.Observe(float64(totalDuration.Milliseconds()))
	m.txConfirmationLatency.Observe(float64(confirmationDuration.Milliseconds()))
}

type Test interface {
	Run(tests.TestContext, context.Context, *Wallet)
}

type Generator struct {
	wallets []*Wallet
	txTests []Test
}

func NewGenerator(
	wallets []*Wallet,
	txTests []Test,
) (Generator, error) {
	if len(wallets) != len(txTests) {
		return Generator{}, fmt.Errorf(
			"wallet and tx builder count mismatch: got %d wallets and %d txBuilders",
			len(wallets),
			len(txTests),
		)
	}

	return Generator{
		wallets: wallets,
		txTests: txTests,
	}, nil
}

func (g Generator) Run(tc tests.TestContext, ctx context.Context) {
	wg := sync.WaitGroup{}

	for i := range g.wallets {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				g.txTests[i].Run(tc, ctx, g.wallets[i])
			}
		}()
	}

	<-ctx.Done()
	wg.Wait()
}

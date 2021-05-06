// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/gorilla/rpc/v2"
	"github.com/prometheus/client_golang/prometheus"
)

type contextKey int

const requestTimestampKey contextKey = iota

func newCallsMetric(namespace, name string) prometheus.Counter {
	return prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      fmt.Sprintf("%s_calls", name),
		Help:      fmt.Sprintf("Number of times %s has been called", name),
	})
}

type metrics struct {
	numBootstrappingCalls, numBootstrappedCalls, numCreateHandlersCalls,
	numPendingCalls, numParseCalls, numGetCalls prometheus.Counter

	numTxRefreshes, numTxRefreshHits, numTxRefreshMisses prometheus.Counter

	requestErrors   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

func (m *metrics) Initialize(
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.numBootstrappingCalls = newCallsMetric(namespace, "bootstrapping")
	m.numBootstrappedCalls = newCallsMetric(namespace, "bootstrapped")
	m.numCreateHandlersCalls = newCallsMetric(namespace, "create_handlers")
	m.numPendingCalls = newCallsMetric(namespace, "pending")
	m.numParseCalls = newCallsMetric(namespace, "parse")
	m.numGetCalls = newCallsMetric(namespace, "get")

	m.numTxRefreshes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tx_refreshes",
		Help:      "Number of times unique txs have been refreshed",
	})
	m.numTxRefreshHits = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tx_refresh_hits",
		Help:      "Number of times unique txs have not been unique, but were cached",
	})
	m.numTxRefreshMisses = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tx_refresh_misses",
		Help:      "Number of times unique txs have not been unique and weren't cached",
	})

	m.requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "request_duration_ms",
		Buckets:   utils.MillisecondsBuckets,
	}, []string{"method"})

	m.requestErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "request_error_count",
	}, []string{"method"})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numBootstrappingCalls),
		registerer.Register(m.numBootstrappedCalls),
		registerer.Register(m.numCreateHandlersCalls),
		registerer.Register(m.numPendingCalls),
		registerer.Register(m.numParseCalls),
		registerer.Register(m.numGetCalls),
		registerer.Register(m.numTxRefreshes),
		registerer.Register(m.numTxRefreshHits),
		registerer.Register(m.numTxRefreshMisses),

		registerer.Register(m.requestDuration),
		registerer.Register(m.requestErrors),
	)
	return errs.Err
}

func (m *metrics) InterceptRequestFunc(i *rpc.RequestInfo) *http.Request {
	return i.Request.WithContext(context.WithValue(i.Request.Context(), requestTimestampKey, time.Now()))
}

func (m *metrics) AfterRequestFunc(i *rpc.RequestInfo) {
	timestamp := i.Request.Context().Value(requestTimestampKey)
	timeCast, ok := timestamp.(time.Time)
	if !ok {
		return
	}
	totalTime := time.Since(timeCast)
	m.requestDuration.With(prometheus.Labels{"method": i.Method}).Observe(float64(totalTime.Milliseconds()))

	if i.Error != nil {
		m.requestErrors.With(prometheus.Labels{"method": i.Method}).Inc()
	}
}

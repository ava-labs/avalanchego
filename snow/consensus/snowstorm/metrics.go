// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowstorm

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/prometheus/client_golang/prometheus"
)

type metrics struct {
	// numProcessing keeps track of the number of transactions currently
	// processing in a snowstorm instance
	numProcessing prometheus.Gauge

	// accepted tracks the number of milliseconds that a transaction was
	// processing before being accepted
	accepted prometheus.Histogram

	// rejected tracks the number of milliseconds that a transaction was
	// processing before being rejected
	rejected prometheus.Histogram

	// clock gives access to the current wall clock time
	clock timer.Clock

	// processing keeps track of the time that each transaction was issued into
	// the snowstorm instance. This is used to calculate the amount of time to
	// accept or reject the transaction
	processingTxs *ProcessingTxs

	log logging.Logger
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(
	log logging.Logger,
	namespace string,
	registerer prometheus.Registerer,
) error {
	m.processingTxs = NewProcessingTxs()
	m.log = log

	m.numProcessing = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "tx_processing",
		Help:      "Number of processing transactions",
	})
	m.accepted = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "tx_accepted",
		Help:      "Time spent processing before being accepted in milliseconds",
		Buckets:   timer.MillisecondsBuckets,
	})
	m.rejected = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "tx_rejected",
		Help:      "Time spent processing before being rejected in milliseconds",
		Buckets:   timer.MillisecondsBuckets,
	})

	errs := wrappers.Errs{}
	errs.Add(
		registerer.Register(m.numProcessing),
		registerer.Register(m.accepted),
		registerer.Register(m.rejected),
	)
	return errs.Err
}

// Issued marks that a transaction with the provided ID was added to the
// snowstorm consensus instance. It is assumed that either Accept or Reject will
// be called with this same ID in the future.
func (m *metrics) Issued(id ids.ID) {
	m.processingTxs.PutTx(id, m.clock.Time())
	m.numProcessing.Inc()
}

// Accepted marks that a transaction with the provided ID was accepted. It is
// assumed that Issued was previously called with this ID.
func (m *metrics) Accepted(id ids.ID) {
	start, ok := m.processingTxs.GetTx(id)
	if !ok {
		// TODO-pedro should we log this ? I don't think this can happen though
		m.log.Warn("unable to measure Accepted transaction %v", id.String())
		return
	}
	end := m.clock.Time()

	m.processingTxs.Evict(id)

	m.accepted.Observe(float64(end.Sub(start.Time).Milliseconds()))
	m.numProcessing.Dec()
}

// Rejected marks that a transaction with the provided ID was rejected. It is
// assumed that Issued was previously called with this ID.
func (m *metrics) Rejected(id ids.ID) {
	start, ok := m.processingTxs.GetTx(id)
	if !ok {
		// TODO-pedro should we log this ? I don't think this can happen though
		m.log.Warn("unable to measure Rejected transaction %v", id.String())
		return
	}
	end := m.clock.Time()

	m.processingTxs.Evict(id)

	m.rejected.Observe(float64(end.Sub(start.Time).Milliseconds()))
	m.numProcessing.Dec()
}

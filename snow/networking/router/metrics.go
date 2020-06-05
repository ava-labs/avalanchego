// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/gecko/utils/timer"
	"github.com/ava-labs/gecko/utils/wrappers"
)

func initHistogram(namespace, name string, registerer prometheus.Registerer, errs *wrappers.Errs) prometheus.Histogram {
	histogram := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      name,
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})

	if err := registerer.Register(histogram); err != nil {
		errs.Add(fmt.Errorf("failed to register %s statistics due to %s", name, err))
	}
	return histogram
}

type metrics struct {
	getAcceptedFrontier, acceptedFrontier, getAcceptedFrontierFailed,
	getAccepted, accepted, getAcceptedFailed,
	get, put, getFailed,
	pushQuery, pullQuery, chits, queryFailed,
	notify,
	gossip,
	shutdown prometheus.Histogram
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(namespace string, registerer prometheus.Registerer) error {
	errs := wrappers.Errs{}
	m.getAcceptedFrontier = initHistogram()
	return errs.Err
	m.getAcceptedFrontier = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "get_accepted_frontier",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.acceptedFrontier = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "accepted_frontier",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.getAcceptedFrontierFailed = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "get_accepted_frontier_failed",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.getAccepted = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "get_accepted",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.accepted = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "accepted",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.getAcceptedFailed = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "get_accepted_failed",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.get = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "get",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.put = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "put",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.getFailed = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "getFailed",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})
	m.getFailed = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      "getFailed",
			Help:      "Time spent processing this request in nanoseconds",
			Buckets:   timer.NanosecondsBuckets,
		})

	if err := registerer.Register(m.getAcceptedFrontier); err != nil {
		return fmt.Errorf("failed to register get_accepted_frontier statistics due to %s", err)
	}
	return nil
}

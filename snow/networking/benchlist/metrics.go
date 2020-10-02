// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/timer"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultRequestHelpMsg = "Time spent waiting for a response to this message in milliseconds"
	validatorIDLabel      = "validatorID"
)

func initHistogram(namespace, name string, registerer prometheus.Registerer, errs *wrappers.Errs) prometheus.Histogram {
	histogram := prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: namespace,
			Name:      name,
			Help:      defaultRequestHelpMsg,
			Buckets:   timer.MillisecondsBuckets,
		})

	if err := registerer.Register(histogram); err != nil {
		errs.Add(fmt.Errorf("failed to register %s statistics due to %w", name, err))
	}
	return histogram
}

func initSummary(namespace, name string, registerer prometheus.Registerer, errs *wrappers.Errs) *prometheus.SummaryVec {
	summary := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Namespace: namespace,
			Name:      name,
			Help:      defaultRequestHelpMsg,
		}, []string{validatorIDLabel})

	if err := registerer.Register(summary); err != nil {
		errs.Add(fmt.Errorf("failed to register %s statistics due to %w", name, err))
	}
	return summary
}

type metrics struct {
	ctx *snow.Context

	summaryEnabled bool

	numBenched    prometheus.Gauge
	weightBenched prometheus.Gauge

	getAcceptedFrontierSummary, getAcceptedSummary,
	getAncestorsSummary, getSummary,
	pushQuerySummary, pullQuerySummary *prometheus.SummaryVec

	getAcceptedFrontier, getAccepted,
	getAncestors, get,
	pushQuery, pullQuery prometheus.Histogram
}

// Initialize implements the Engine interface
func (m *metrics) Initialize(ctx *snow.Context, summaryEnabled bool) error {
	m.ctx = ctx
	m.summaryEnabled = summaryEnabled
	errs := wrappers.Errs{}

	benchNamespace := fmt.Sprintf("%s_benchlist", ctx.Namespace)

	m.numBenched = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: benchNamespace,
		Name:      "benched_num",
		Help:      "Number of currently benched validators",
	})
	if err := ctx.Metrics.Register(m.numBenched); err != nil {
		errs.Add(fmt.Errorf("failed to register num benched statistics due to %w", err))
	}

	m.weightBenched = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: benchNamespace,
		Name:      "benched_weight",
		Help:      "Weight of currently benched validators",
	})
	if err := ctx.Metrics.Register(m.weightBenched); err != nil {
		errs.Add(fmt.Errorf("failed to register weight benched statistics due to %w", err))
	}

	querylatencyNamespace := fmt.Sprintf("%s_sender", ctx.Namespace)
	ctx.Log.Info("Registered benchlist metrics with namespace: %s", querylatencyNamespace)

	m.getAcceptedFrontierSummary = initSummary(querylatencyNamespace, "lat_get_accepted_frontier_peer", ctx.Metrics, &errs)
	m.getAcceptedSummary = initSummary(querylatencyNamespace, "lat_get_accepted_peer", ctx.Metrics, &errs)
	m.getAncestorsSummary = initSummary(querylatencyNamespace, "lat_get_ancestors_peer", ctx.Metrics, &errs)
	m.getSummary = initSummary(querylatencyNamespace, "lat_get_peer", ctx.Metrics, &errs)
	m.pushQuerySummary = initSummary(querylatencyNamespace, "lat_push_query_peer", ctx.Metrics, &errs)
	m.pullQuerySummary = initSummary(querylatencyNamespace, "lat_pull_query_peer", ctx.Metrics, &errs)

	m.getAcceptedFrontier = initHistogram(querylatencyNamespace, "lat_get_accepted_frontier", ctx.Metrics, &errs)
	m.getAccepted = initHistogram(querylatencyNamespace, "lat_get_accepted", ctx.Metrics, &errs)
	m.getAncestors = initHistogram(querylatencyNamespace, "lat_get_ancestors", ctx.Metrics, &errs)
	m.get = initHistogram(querylatencyNamespace, "lat_get", ctx.Metrics, &errs)
	m.pushQuery = initHistogram(querylatencyNamespace, "lat_push_query", ctx.Metrics, &errs)
	m.pullQuery = initHistogram(querylatencyNamespace, "lat_pull_query", ctx.Metrics, &errs)

	return errs.Err
}

func (m *metrics) observe(validatorID ids.ShortID, msgType constants.MsgType, latency float64) {
	if !m.summaryEnabled {
		switch msgType {
		case constants.GetAcceptedFrontierMsg:
			m.getAcceptedFrontier.Observe(latency)
		case constants.GetAcceptedMsg:
			m.getAccepted.Observe(latency)
		case constants.GetMsg:
			m.get.Observe(latency)
		case constants.PushQueryMsg:
			m.pushQuery.Observe(latency)
		case constants.PullQueryMsg:
			m.pullQuery.Observe(latency)
		}

		return
	}

	switch msgType {
	case constants.GetAcceptedFrontierMsg:
		observer, err := m.getAcceptedFrontierSummary.GetMetricWith(prometheus.Labels{validatorIDLabel: validatorID.String()})
		if err == nil {
			observer.Observe(latency)
		} else {
			m.ctx.Log.Warn("Failed to get observer with validatorID label")
		}
		m.getAcceptedFrontier.Observe(latency)
	case constants.GetAcceptedMsg:
		observer, err := m.getAcceptedSummary.GetMetricWith(prometheus.Labels{validatorIDLabel: validatorID.String()})
		if err == nil {
			observer.Observe(latency)
		} else {
			m.ctx.Log.Warn("Failed to get observer with validatorID label")
		}
		m.getAccepted.Observe(latency)
	case constants.GetMsg:
		observer, err := m.getSummary.GetMetricWith(prometheus.Labels{validatorIDLabel: validatorID.String()})
		if err == nil {
			observer.Observe(latency)
		} else {
			m.ctx.Log.Warn("Failed to get observer with validatorID label")
		}
		m.get.Observe(latency)
	case constants.PushQueryMsg:
		observer, err := m.pushQuerySummary.GetMetricWith(prometheus.Labels{validatorIDLabel: validatorID.String()})
		if err == nil {
			observer.Observe(latency)
		} else {
			m.ctx.Log.Warn("Failed to get observer with validatorID label")
		}
		m.pushQuery.Observe(latency)
	case constants.PullQueryMsg:
		observer, err := m.pullQuerySummary.GetMetricWith(prometheus.Labels{validatorIDLabel: validatorID.String()})
		if err == nil {
			observer.Observe(latency)
		} else {
			m.ctx.Log.Warn("Failed to get observer with validatorID label")
		}
		m.pullQuery.Observe(latency)
	}
}

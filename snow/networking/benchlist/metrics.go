// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package benchlist

import (
	"fmt"
	"time"

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

func initHistogram(
	namespace,
	name string,
	registerer prometheus.Registerer,
	errs *wrappers.Errs,
) prometheus.Histogram {
	histogram := prometheus.NewHistogram(prometheus.HistogramOpts{
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

func initSummary(
	namespace,
	name string,
	registerer prometheus.Registerer,
	errs *wrappers.Errs,
) *prometheus.SummaryVec {
	summary := prometheus.NewSummaryVec(prometheus.SummaryOpts{
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
func (m *metrics) Initialize(ctx *snow.Context, namespace string, summaryEnabled bool) error {
	m.ctx = ctx
	m.summaryEnabled = summaryEnabled
	errs := wrappers.Errs{}

	benchNamespace := fmt.Sprintf("%s_benchlist", namespace)

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

	queryLatencyNamespace := fmt.Sprintf("%s_sender", namespace)

	m.getAcceptedFrontierSummary = initSummary(queryLatencyNamespace, "lat_get_accepted_frontier_peer", ctx.Metrics, &errs)
	m.getAcceptedSummary = initSummary(queryLatencyNamespace, "lat_get_accepted_peer", ctx.Metrics, &errs)
	m.getAncestorsSummary = initSummary(queryLatencyNamespace, "lat_get_ancestors_peer", ctx.Metrics, &errs)
	m.getSummary = initSummary(queryLatencyNamespace, "lat_get_peer", ctx.Metrics, &errs)
	m.pushQuerySummary = initSummary(queryLatencyNamespace, "lat_push_query_peer", ctx.Metrics, &errs)
	m.pullQuerySummary = initSummary(queryLatencyNamespace, "lat_pull_query_peer", ctx.Metrics, &errs)

	m.getAcceptedFrontier = initHistogram(queryLatencyNamespace, "lat_get_accepted_frontier", ctx.Metrics, &errs)
	m.getAccepted = initHistogram(queryLatencyNamespace, "lat_get_accepted", ctx.Metrics, &errs)
	m.getAncestors = initHistogram(queryLatencyNamespace, "lat_get_ancestors", ctx.Metrics, &errs)
	m.get = initHistogram(queryLatencyNamespace, "lat_get", ctx.Metrics, &errs)
	m.pushQuery = initHistogram(queryLatencyNamespace, "lat_push_query", ctx.Metrics, &errs)
	m.pullQuery = initHistogram(queryLatencyNamespace, "lat_pull_query", ctx.Metrics, &errs)

	return errs.Err
}

func (m *metrics) observe(validatorID ids.ShortID, msgType constants.MsgType, latency time.Duration) {
	latencyMS := float64(latency) / float64(time.Millisecond)

	switch msgType {
	case constants.GetAcceptedFrontierMsg:
		m.getAcceptedFrontier.Observe(latencyMS)
	case constants.GetAcceptedMsg:
		m.getAccepted.Observe(latencyMS)
	case constants.GetMsg:
		m.get.Observe(latencyMS)
	case constants.PushQueryMsg:
		m.pushQuery.Observe(latencyMS)
	case constants.PullQueryMsg:
		m.pullQuery.Observe(latencyMS)
	}

	if !m.summaryEnabled {
		return
	}

	labels := prometheus.Labels{
		validatorIDLabel: validatorID.String(),
	}
	var (
		observer prometheus.Observer
		err      error
	)
	switch msgType {
	case constants.GetAcceptedFrontierMsg:
		observer, err = m.getAcceptedFrontierSummary.GetMetricWith(labels)
	case constants.GetAcceptedMsg:
		observer, err = m.getAcceptedSummary.GetMetricWith(labels)
	case constants.GetMsg:
		observer, err = m.getSummary.GetMetricWith(labels)
	case constants.PushQueryMsg:
		observer, err = m.pushQuerySummary.GetMetricWith(labels)
	case constants.PullQueryMsg:
		observer, err = m.pullQuerySummary.GetMetricWith(labels)
	default:
		return
	}

	if err == nil {
		observer.Observe(latencyMS)
	} else {
		m.ctx.Log.Warn("Failed to get observer with validatorID label due to %s", err)
	}
}

package timeout

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	defaultRequestHelpMsg = "time (in ns) spent waiting for a response to this message"
	validatorIDLabel      = "validatorID"
)

func initAverager(
	namespace,
	name string,
	reg prometheus.Registerer,
	errs *wrappers.Errs,
) metric.Averager {
	return metric.NewAveragerWithErrs(
		namespace,
		name,
		defaultRequestHelpMsg,
		reg,
		errs,
	)
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
		errs.Add(fmt.Errorf("failed to register %s statistics: %w", name, err))
	}
	return summary
}

type metrics struct {
	chainToMetrics map[ids.ID]*chainMetrics
}

func (m *metrics) RegisterChain(ctx *snow.Context, namespace string) error {
	if m.chainToMetrics == nil {
		m.chainToMetrics = map[ids.ID]*chainMetrics{}
	}
	if _, exists := m.chainToMetrics[ctx.ChainID]; exists {
		return fmt.Errorf("chain %s has already been registered", ctx.ChainID)
	}
	cm := &chainMetrics{}
	if err := cm.Initialize(ctx, namespace, false); err != nil {
		return fmt.Errorf("couldn't initialize metrics for chain %s: %w", ctx.ChainID, err)
	}
	m.chainToMetrics[ctx.ChainID] = cm
	return nil
}

// Record that a response to a message of type [msgType] regarding chain [chainID] took [latency]
func (m *metrics) observe(chainID ids.ID, msgType constants.MsgType, latency time.Duration) {
	cm, exists := m.chainToMetrics[chainID]
	if !exists {
		// TODO should this log an error?
		return
	}
	cm.observe(ids.ShortEmpty, msgType, latency)
}

// chainMetrics contains message response time metrics for a chain
type chainMetrics struct {
	ctx *snow.Context

	summaryEnabled bool

	getAcceptedFrontierSummary, getAcceptedSummary,
	getAncestorsSummary, getSummary,
	pushQuerySummary, pullQuerySummary *prometheus.SummaryVec

	getAcceptedFrontier, getAccepted,
	getAncestors, get,
	pushQuery, pullQuery metric.Averager
}

// Initialize implements the Engine interface
func (cm *chainMetrics) Initialize(ctx *snow.Context, namespace string, summaryEnabled bool) error {
	cm.summaryEnabled = summaryEnabled
	errs := wrappers.Errs{}

	queryLatencyNamespace := fmt.Sprintf("%s_lat", namespace)

	cm.getAcceptedFrontierSummary = initSummary(queryLatencyNamespace, "get_accepted_frontier_peer", ctx.Metrics, &errs)
	cm.getAcceptedSummary = initSummary(queryLatencyNamespace, "get_accepted_peer", ctx.Metrics, &errs)
	cm.getAncestorsSummary = initSummary(queryLatencyNamespace, "get_ancestors_peer", ctx.Metrics, &errs)
	cm.getSummary = initSummary(queryLatencyNamespace, "get_peer", ctx.Metrics, &errs)
	cm.pushQuerySummary = initSummary(queryLatencyNamespace, "push_query_peer", ctx.Metrics, &errs)
	cm.pullQuerySummary = initSummary(queryLatencyNamespace, "pull_query_peer", ctx.Metrics, &errs)

	cm.getAcceptedFrontier = initAverager(queryLatencyNamespace, "get_accepted_frontier", ctx.Metrics, &errs)
	cm.getAccepted = initAverager(queryLatencyNamespace, "get_accepted", ctx.Metrics, &errs)
	cm.getAncestors = initAverager(queryLatencyNamespace, "get_ancestors", ctx.Metrics, &errs)
	cm.get = initAverager(queryLatencyNamespace, "get", ctx.Metrics, &errs)
	cm.pushQuery = initAverager(queryLatencyNamespace, "push_query", ctx.Metrics, &errs)
	cm.pullQuery = initAverager(queryLatencyNamespace, "pull_query", ctx.Metrics, &errs)

	return errs.Err
}

func (cm *chainMetrics) observe(validatorID ids.ShortID, msgType constants.MsgType, latency time.Duration) {
	lat := float64(latency)
	switch msgType {
	case constants.GetAcceptedFrontierMsg:
		cm.getAcceptedFrontier.Observe(lat)
	case constants.GetAcceptedMsg:
		cm.getAccepted.Observe(lat)
	case constants.GetMsg:
		cm.get.Observe(lat)
	case constants.PushQueryMsg:
		cm.pushQuery.Observe(lat)
	case constants.PullQueryMsg:
		cm.pullQuery.Observe(lat)
	}

	if !cm.summaryEnabled {
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
		observer, err = cm.getAcceptedFrontierSummary.GetMetricWith(labels)
	case constants.GetAcceptedMsg:
		observer, err = cm.getAcceptedSummary.GetMetricWith(labels)
	case constants.GetMsg:
		observer, err = cm.getSummary.GetMetricWith(labels)
	case constants.PushQueryMsg:
		observer, err = cm.pushQuerySummary.GetMetricWith(labels)
	case constants.PullQueryMsg:
		observer, err = cm.pullQuerySummary.GetMetricWith(labels)
	default:
		return
	}

	if err == nil {
		observer.Observe(lat)
	} else {
		cm.ctx.Log.Warn("Failed to get observer with validatorID label due to %s", err)
	}
}

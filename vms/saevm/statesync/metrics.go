// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package statesync

import (
	"github.com/prometheus/client_golang/prometheus"

	syncnet "github.com/ava-labs/avalanchego/vms/evm/sync/network"
)

// metricsNamespace prefixes every state sync metric, the client-side request
// counts and the server-side handler counts alike. The C-Chain's atomic trie
// counterpart is registered under its own namespace; see
// vms/saevm/cchain/statesync.
const metricsNamespace = "statesync"

// clientMetrics counts the requests this node sends while state syncing, one
// [syncnet.Metrics] per RPC type. The base names mirror coreth's client-side
// sync metrics.
type clientMetrics struct {
	stateTrieLeaves *syncnet.Metrics
	code            *syncnet.Metrics
	blocks          *syncnet.Metrics
}

func newClientMetrics(reg prometheus.Registerer) (*clientMetrics, error) {
	stateTrieLeaves, err := syncnet.NewMetrics(reg, "sync_state_trie_leaves")
	if err != nil {
		return nil, err
	}
	code, err := syncnet.NewMetrics(reg, "sync_code")
	if err != nil {
		return nil, err
	}
	blocks, err := syncnet.NewMetrics(reg, "sync_blocks")
	if err != nil {
		return nil, err
	}
	return &clientMetrics{
		stateTrieLeaves: stateTrieLeaves,
		code:            code,
		blocks:          blocks,
	}, nil
}

// lifecycleMetrics reports the observable lifecycle of the sync driven by the
// C-Chain summary handler (vms/saevm/cchain/statesync), which records it via
// [Handler.MarkSyncStarted] and [Handler.MarkSyncFinished]. It exists so that
// operators and the msync e2e harness can observe a sync — that one started,
// what summary it targets, whether it is still running, and when and how it
// ended — from the metrics API alone.
type lifecycleMetrics struct {
	// inProgress is 1 from [Handler.MarkSyncStarted] until
	// [Handler.MarkSyncFinished].
	inProgress prometheus.Gauge
	// summaryHeight is the accepted summary's block height, 0 until a sync
	// starts.
	summaryHeight prometheus.Gauge
	// startedTimestamp and finishedTimestamp are unix seconds, 0 until the
	// corresponding transition. Their difference is the sync's duration,
	// timed by the VM itself rather than inferred by a poller.
	startedTimestamp  prometheus.Gauge
	finishedTimestamp prometheus.Gauge
	// failed is 1 iff the sync terminated with an error, which is fatal to
	// the chain.
	failed prometheus.Gauge
}

func newLifecycleMetrics(reg prometheus.Registerer) (*lifecycleMetrics, error) {
	m := &lifecycleMetrics{
		inProgress: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "in_progress",
			Help: "1 while a state sync is running, 0 otherwise",
		}),
		summaryHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "summary_height",
			Help: "block height of the accepted state summary being synced; 0 until a sync starts",
		}),
		startedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "started_timestamp",
			Help: "unix seconds at which the accepted summary's sync was launched; 0 until then",
		}),
		finishedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "finished_timestamp",
			Help: "unix seconds at which the sync terminated, in success or failure; 0 until then",
		}),
		failed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "failed",
			Help: "1 if the state sync terminated with an error, 0 otherwise",
		}),
	}
	for _, c := range []prometheus.Collector{
		m.inProgress,
		m.summaryHeight,
		m.startedTimestamp,
		m.finishedTimestamp,
		m.failed,
	} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// MarkSyncStarted records that a sync to summary has been launched. It MUST be
// called before the sync mutates anything, so that a sync is never observable
// through its side effects without also being observable here.
func (h *Handler) MarkSyncStarted(summary *Summary) {
	h.lifecycle.summaryHeight.Set(float64(summary.AcceptedHeight))
	h.lifecycle.startedTimestamp.SetToCurrentTime()
	h.lifecycle.inProgress.Set(1)
}

// MarkSyncFinished records that the sync recorded by [Handler.MarkSyncStarted]
// terminated; a non-nil err marks it failed. It MUST be called exactly once,
// after the sync's final write.
func (h *Handler) MarkSyncFinished(err error) {
	if err != nil {
		h.lifecycle.failed.Set(1)
	}
	h.lifecycle.finishedTimestamp.SetToCurrentTime()
	h.lifecycle.inProgress.Set(0)
}

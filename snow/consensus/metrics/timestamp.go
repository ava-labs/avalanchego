// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var _ Timestamp = (*timestamp)(nil)

// Timestamp reports the last accepted block time,
// to track it in unix seconds.
type Timestamp interface {
	Accepted(ts time.Time)
}

type timestamp struct {
	// lastAcceptedTimestamp keeps track of the last accepted timestamp
	lastAcceptedTimestamp prometheus.Gauge
}

func NewTimestamp(namespace string, reg prometheus.Registerer) (Timestamp, error) {
	t := &timestamp{
		lastAcceptedTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_accepted_timestamp",
			Help:      "Last accepted block timestamp in unix seconds",
		}),
	}
	return t, reg.Register(t.lastAcceptedTimestamp)
}

func (t *timestamp) Accepted(ts time.Time) {
	t.lastAcceptedTimestamp.Set(float64(ts.Unix()))
}

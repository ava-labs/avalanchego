// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var _ Height = &height{}

// Height reports the last accepted height
type Height interface {
	Accepted(height uint64)
}

type height struct {
	// lastAcceptedHeight keeps track of the last accepted height
	lastAcceptedHeight prometheus.Gauge
}

func NewHeight(namespace string, reg prometheus.Registerer) (Height, error) {
	h := &height{
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_accepted_height",
			Help:      "Last height accepted",
		}),
	}
	return h, reg.Register(h.lastAcceptedHeight)
}

func (h *height) Accepted(height uint64) {
	h.lastAcceptedHeight.Set(float64(height))
}

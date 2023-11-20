// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/math"
)

var _ Height = (*height)(nil)

type Height interface {
	Verified(height uint64)
	Accepted(height uint64)
}

type height struct {
	currentMaxVerifiedHeight uint64
	maxVerifiedHeight        prometheus.Gauge

	lastAcceptedHeight prometheus.Gauge
}

func NewHeight(namespace string, reg prometheus.Registerer) (Height, error) {
	h := &height{
		maxVerifiedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "max_verified_height",
			Help:      "Highest verified height",
		}),
		lastAcceptedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "last_accepted_height",
			Help:      "Last height accepted",
		}),
	}
	err := utils.Err(
		reg.Register(h.lastAcceptedHeight),
		reg.Register(h.maxVerifiedHeight),
	)
	return h, err
}

func (h *height) Verified(height uint64) {
	h.currentMaxVerifiedHeight = math.Max(h.currentMaxVerifiedHeight, height)
	h.maxVerifiedHeight.Set(float64(h.currentMaxVerifiedHeight))
}

func (h *height) Accepted(height uint64) {
	h.lastAcceptedHeight.Set(float64(height))
}

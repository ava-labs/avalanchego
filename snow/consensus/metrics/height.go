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
	currentHighestVerifiedHeight uint64
	highestVerifiedHeight        prometheus.Gauge

	lastAcceptedHeight prometheus.Gauge
}

func NewHeight(namespace string, reg prometheus.Registerer) (Height, error) {
	h := &height{
		highestVerifiedHeight: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "highest_verified_height",
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
		reg.Register(h.highestVerifiedHeight),
	)
	return h, err
}

func (h *height) Verified(height uint64) {
	h.currentHighestVerifiedHeight = math.Max(h.currentHighestVerifiedHeight, height)
	h.highestVerifiedHeight.Set(float64(h.currentHighestVerifiedHeight))
}

func (h *height) Accepted(height uint64) {
	h.lastAcceptedHeight.Set(float64(height))
}

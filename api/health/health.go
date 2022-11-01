// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

var _ Health = (*health)(nil)

// Health defines the full health service interface for registering, reporting
// and refreshing health checks.
type Health interface {
	Registerer
	Reporter

	Start(freq time.Duration)
	Stop()
}

// Registerer defines how to register new components to check the health of.
type Registerer interface {
	RegisterReadinessCheck(name string, checker Checker) error
	RegisterHealthCheck(name string, checker Checker) error
	RegisterLivenessCheck(name string, checker Checker) error
}

// Reporter returns the current health status.
type Reporter interface {
	Readiness() (map[string]Result, bool)
	Health() (map[string]Result, bool)
	Liveness() (map[string]Result, bool)
}

type health struct {
	log       logging.Logger
	readiness *worker
	health    *worker
	liveness  *worker
}

func New(log logging.Logger, registerer prometheus.Registerer) (Health, error) {
	readinessWorker, err := newWorker("readiness", registerer)
	if err != nil {
		return nil, err
	}

	healthWorker, err := newWorker("health", registerer)
	if err != nil {
		return nil, err
	}

	livenessWorker, err := newWorker("liveness", registerer)
	return &health{
		log:       log,
		readiness: readinessWorker,
		health:    healthWorker,
		liveness:  livenessWorker,
	}, err
}

func (h *health) RegisterReadinessCheck(name string, checker Checker) error {
	return h.readiness.RegisterMonotonicCheck(name, checker)
}

func (h *health) RegisterHealthCheck(name string, checker Checker) error {
	return h.health.RegisterCheck(name, checker)
}

func (h *health) RegisterLivenessCheck(name string, checker Checker) error {
	return h.liveness.RegisterCheck(name, checker)
}

func (h *health) Readiness() (map[string]Result, bool) {
	results, healthy := h.readiness.Results()
	if !healthy {
		h.log.Warn("failing readiness check",
			zap.Reflect("reason", results),
		)
	}
	return results, healthy
}

func (h *health) Health() (map[string]Result, bool) {
	results, healthy := h.health.Results()
	if !healthy {
		h.log.Warn("failing health check",
			zap.Reflect("reason", results),
		)
	}
	return results, healthy
}

func (h *health) Liveness() (map[string]Result, bool) {
	results, healthy := h.liveness.Results()
	if !healthy {
		h.log.Warn("failing liveness check",
			zap.Reflect("reason", results),
		)
	}
	return results, healthy
}

func (h *health) Start(freq time.Duration) {
	h.readiness.Start(freq)
	h.health.Start(freq)
	h.liveness.Start(freq)
}

func (h *health) Stop() {
	h.readiness.Stop()
	h.health.Stop()
	h.liveness.Stop()
}

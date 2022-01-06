// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var _ Health = &health{}

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
	readiness *worker
	health    *worker
	liveness  *worker
}

func New(registerer prometheus.Registerer) (Health, error) {
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
	return h.readiness.Results()
}

func (h *health) Health() (map[string]Result, bool) {
	return h.health.Results()
}

func (h *health) Liveness() (map[string]Result, bool) {
	return h.liveness.Results()
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

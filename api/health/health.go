// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/logging"
)

const (
	// CheckLabel is the label used to differentiate between health checks.
	CheckLabel = "check"
	// TagLabel is the label used to differentiate between health check tags.
	TagLabel = "tag"
	// AllTag is automatically added to every registered check.
	AllTag = "all"
	// ApplicationTag checks will act as if they specified every tag that has
	// been registered.
	// Registering a health check with this tag will ensure that it is always
	// included in all health query results.
	ApplicationTag = "application"
)

var _ Health = (*health)(nil)

// Health defines the full health service interface for registering, reporting
// and refreshing health checks.
type Health interface {
	Registerer
	Reporter

	// Start running periodic health checks at the specified frequency.
	// Repeated calls to Start will be no-ops.
	Start(ctx context.Context, freq time.Duration)

	// Stop running periodic health checks. Stop should only be called after
	// Start. Once Stop returns, no more health checks will be executed.
	Stop()
}

// Registerer defines how to register new components to check the health of.
type Registerer interface {
	RegisterReadinessCheck(name string, checker Checker, tags ...string) error
	RegisterHealthCheck(name string, checker Checker, tags ...string) error
	RegisterLivenessCheck(name string, checker Checker, tags ...string) error
}

// Reporter returns the current health status.
type Reporter interface {
	Readiness(tags ...string) (map[string]Result, bool)
	Health(tags ...string) (map[string]Result, bool)
	Liveness(tags ...string) (map[string]Result, bool)
}

type health struct {
	log       logging.Logger
	readiness *worker
	health    *worker
	liveness  *worker
}

func New(log logging.Logger, registerer prometheus.Registerer) (Health, error) {
	failingChecks := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "checks_failing",
			Help: "number of currently failing health checks",
		},
		[]string{CheckLabel, TagLabel},
	)
	return &health{
		log:       log,
		readiness: newWorker(log, "readiness", failingChecks),
		health:    newWorker(log, "health", failingChecks),
		liveness:  newWorker(log, "liveness", failingChecks),
	}, registerer.Register(failingChecks)
}

func (h *health) RegisterReadinessCheck(name string, checker Checker, tags ...string) error {
	return h.readiness.RegisterMonotonicCheck(name, checker, tags...)
}

func (h *health) RegisterHealthCheck(name string, checker Checker, tags ...string) error {
	return h.health.RegisterCheck(name, checker, tags...)
}

func (h *health) RegisterLivenessCheck(name string, checker Checker, tags ...string) error {
	return h.liveness.RegisterCheck(name, checker, tags...)
}

func (h *health) Readiness(tags ...string) (map[string]Result, bool) {
	results, healthy := h.readiness.Results(tags...)
	if !healthy {
		h.log.Warn("failing check",
			zap.String("namespace", "readiness"),
			zap.Reflect("reason", results),
		)
	}
	return results, healthy
}

func (h *health) Health(tags ...string) (map[string]Result, bool) {
	results, healthy := h.health.Results(tags...)
	if !healthy {
		h.log.Warn("failing check",
			zap.String("namespace", "health"),
			zap.Reflect("reason", results),
		)
	}
	return results, healthy
}

func (h *health) Liveness(tags ...string) (map[string]Result, bool) {
	results, healthy := h.liveness.Results(tags...)
	if !healthy {
		h.log.Warn("failing check",
			zap.String("namespace", "liveness"),
			zap.Reflect("reason", results),
		)
	}
	return results, healthy
}

func (h *health) Start(ctx context.Context, freq time.Duration) {
	h.readiness.Start(ctx, freq)
	h.health.Start(ctx, freq)
	h.liveness.Start(ctx, freq)
}

func (h *health) Stop() {
	h.readiness.Stop()
	h.health.Stop()
	h.liveness.Stop()
}

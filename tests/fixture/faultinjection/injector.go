// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package faultinjection

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/dynamic"

	"github.com/ava-labs/avalanchego/tests/fixture/stacktrace"
	"github.com/ava-labs/avalanchego/utils/logging"

	restclient "k8s.io/client-go/rest"
)

// Injector manages chaos experiment injection.
type Injector struct {
	log             logging.Logger
	client          dynamic.Interface
	config          *Config
	namespace       string
	targetNamespace string
	networkUUID     string

	mu          sync.Mutex
	experiments []*Experiment
	stopCh      chan struct{}
	stopped     bool
}

// NewInjector creates a new Injector.
func NewInjector(
	log logging.Logger,
	kubeConfig *restclient.Config,
	config *Config,
	namespace string,
	targetNamespace string,
	networkUUID string,
) (*Injector, error) {
	client, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, stacktrace.Errorf("failed to create dynamic client: %w", err)
	}

	return &Injector{
		log:             log,
		client:          client,
		config:          config,
		namespace:       namespace,
		targetNamespace: targetNamespace,
		networkUUID:     networkUUID,
		stopCh:          make(chan struct{}),
	}, nil
}

// Start begins the chaos injection loop in a background goroutine.
func (i *Injector) Start(ctx context.Context) {
	go i.run(ctx)
}

// Stop cleans up all active experiments and stops the injection loop.
func (i *Injector) Stop() {
	i.mu.Lock()
	if i.stopped {
		i.mu.Unlock()
		return
	}
	i.stopped = true
	close(i.stopCh)
	experiments := i.experiments
	i.experiments = nil
	i.mu.Unlock()

	// Clean up all active experiments
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, exp := range experiments {
		i.log.Info("cleaning up experiment",
			zap.String("name", exp.Name),
			zap.String("type", string(exp.Type)),
		)
		if err := deleteExperiment(ctx, i.client, exp); err != nil {
			i.log.Warn("failed to delete experiment during cleanup",
				zap.String("name", exp.Name),
				zap.Error(err),
			)
		}
	}
}

// InjectOnce injects a single chaos experiment and returns it.
// This is useful for testing individual fault types.
func (i *Injector) InjectOnce(ctx context.Context, experimentType ExperimentType, duration time.Duration) (*Experiment, error) {
	return i.injectExperiment(ctx, experimentType, duration)
}

func (i *Injector) run(ctx context.Context) {
	ticker := time.NewTicker(i.config.Interval)
	defer ticker.Stop()

	i.log.Info("starting chaos injection loop",
		zap.Duration("interval", i.config.Interval),
		zap.String("namespace", i.namespace),
		zap.String("targetNamespace", i.targetNamespace),
		zap.String("networkUUID", i.networkUUID),
	)

	for {
		select {
		case <-ctx.Done():
			i.log.Info("chaos injection loop stopped due to context cancellation")
			return
		case <-i.stopCh:
			i.log.Info("chaos injection loop stopped")
			return
		case <-ticker.C:
			i.cleanupExpiredExperiments(ctx)

			experimentType := i.selectExperimentType()
			duration := i.randomDuration()

			exp, err := i.injectExperiment(ctx, experimentType, duration)
			if err != nil {
				i.log.Warn("failed to inject experiment",
					zap.String("type", string(experimentType)),
					zap.Error(err),
				)
				continue
			}

			i.mu.Lock()
			i.experiments = append(i.experiments, exp)
			i.mu.Unlock()

			i.log.Info("injected chaos experiment",
				zap.String("name", exp.Name),
				zap.String("type", string(exp.Type)),
				zap.Duration("duration", exp.Duration),
			)
		}
	}
}

func (i *Injector) injectExperiment(ctx context.Context, experimentType ExperimentType, duration time.Duration) (*Experiment, error) {
	name := i.generateExperimentName(experimentType)
	labelSelectors := map[string]string{
		"network_uuid": i.networkUUID,
	}

	switch experimentType {
	case ExperimentTypePodKill:
		return createPodChaos(ctx, i.client, name, i.namespace, i.targetNamespace, labelSelectors, duration)
	case ExperimentTypeNetworkDelay:
		return createNetworkDelay(ctx, i.client, name, i.namespace, i.targetNamespace, labelSelectors,
			i.config.NetworkLatency, i.config.NetworkJitter, duration)
	case ExperimentTypeNetworkLoss:
		return createNetworkLoss(ctx, i.client, name, i.namespace, i.targetNamespace, labelSelectors,
			i.config.NetworkLossPercent, duration)
	default:
		return nil, stacktrace.Errorf("unknown experiment type: %s", experimentType)
	}
}

func (i *Injector) selectExperimentType() ExperimentType {
	totalWeight := i.config.TotalWeight()
	if totalWeight == 0 {
		return ExperimentTypePodKill
	}

	r := rand.Intn(totalWeight) //nolint:gosec
	if r < i.config.PodKillWeight {
		return ExperimentTypePodKill
	}
	r -= i.config.PodKillWeight
	if r < i.config.NetworkDelayWeight {
		return ExperimentTypeNetworkDelay
	}
	return ExperimentTypeNetworkLoss
}

func (i *Injector) randomDuration() time.Duration {
	minNanos := i.config.MinDuration.Nanoseconds()
	maxNanos := i.config.MaxDuration.Nanoseconds()
	if maxNanos <= minNanos {
		return i.config.MinDuration
	}
	return time.Duration(minNanos + rand.Int63n(maxNanos-minNanos)) //nolint:gosec
}

func (i *Injector) generateExperimentName(experimentType ExperimentType) string {
	return string(experimentType) + "-" + randomSuffix()
}

func (i *Injector) cleanupExpiredExperiments(ctx context.Context) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := time.Now()
	var active []*Experiment
	for _, exp := range i.experiments {
		if now.Sub(exp.StartTime) > exp.Duration+10*time.Second {
			// Experiment should have expired, try to delete it
			if err := deleteExperiment(ctx, i.client, exp); err != nil {
				i.log.Warn("failed to delete expired experiment",
					zap.String("name", exp.Name),
					zap.Error(err),
				)
				// Keep it in the list to retry later
				active = append(active, exp)
			} else {
				i.log.Debug("cleaned up expired experiment",
					zap.String("name", exp.Name),
					zap.String("type", string(exp.Type)),
				)
			}
		} else {
			active = append(active, exp)
		}
	}
	i.experiments = active
}

func randomSuffix() string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))] //nolint:gosec
	}
	return string(b)
}

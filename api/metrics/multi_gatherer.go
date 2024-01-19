// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/metric"
)

var (
	_ MultiGatherer = (*multiGatherer)(nil)

	errReregisterGatherer = errors.New("attempt to register existing gatherer")
)

// MultiGatherer extends the Gatherer interface by allowing additional gatherers
// to be registered.
type MultiGatherer interface {
	prometheus.Gatherer

	// Register adds the outputs of [gatherer] to the results of future calls to
	// Gather with the provided [namespace] added to the metrics.
	Register(namespace string, gatherer prometheus.Gatherer) error
}

type multiGatherer struct {
	lock      sync.RWMutex
	gatherers map[string]prometheus.Gatherer
}

func NewMultiGatherer() MultiGatherer {
	return &multiGatherer{
		gatherers: make(map[string]prometheus.Gatherer),
	}
}

func (g *multiGatherer) Gather() ([]*dto.MetricFamily, error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	var results []*dto.MetricFamily
	for namespace, gatherer := range g.gatherers {
		gatheredMetrics, err := gatherer.Gather()
		if err != nil {
			return nil, err
		}
		for _, gatheredMetric := range gatheredMetrics {
			var name string
			if gatheredMetric.Name != nil {
				name = metric.AppendNamespace(namespace, *gatheredMetric.Name)
			} else {
				name = namespace
			}
			gatheredMetric.Name = &name
			results = append(results, gatheredMetric)
		}
	}
	// Because we overwrite every metric's name, we are guaranteed that there
	// are no metrics with nil names.
	sortMetrics(results)
	return results, nil
}

func (g *multiGatherer) Register(namespace string, gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	if existingGatherer, exists := g.gatherers[namespace]; exists {
		return fmt.Errorf("%w for namespace %q; existing: %#v; new: %#v",
			errReregisterGatherer,
			namespace,
			existingGatherer,
			gatherer,
		)
	}

	g.gatherers[namespace] = gatherer
	return nil
}

func sortMetrics(m []*dto.MetricFamily) {
	slices.SortFunc(m, func(i, j *dto.MetricFamily) int {
		return utils.Compare(*i.Name, *j.Name)
	})
}

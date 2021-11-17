// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

var (
	errDuplicatedPrefix = errors.New("duplicated prefix")

	_ MultiGatherer = &multiGatherer{}
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
		metrics, err := gatherer.Gather()
		if err != nil {
			return nil, err
		}
		for _, metric := range metrics {
			var name string
			if metric.Name != nil {
				if len(namespace) > 0 {
					name = fmt.Sprintf("%s_%s", namespace, *metric.Name)
				} else {
					name = *metric.Name
				}
			} else {
				name = namespace
			}
			metric.Name = &name
			results = append(results, metric)
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

	if _, exists := g.gatherers[namespace]; exists {
		return errDuplicatedPrefix
	}

	g.gatherers[namespace] = gatherer
	return nil
}

type sortMetricsData []*dto.MetricFamily

func (m sortMetricsData) Less(i, j int) bool { return *m[i].Name < *m[j].Name }
func (m sortMetricsData) Len() int           { return len(m) }
func (m sortMetricsData) Swap(i, j int)      { m[j], m[i] = m[i], m[j] }

func sortMetrics(m []*dto.MetricFamily) { sort.Sort(sortMetricsData(m)) }

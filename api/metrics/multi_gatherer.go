// (c) 2021, Ava Labs, Inc. All rights reserved.
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

type MultiGatherer interface {
	prometheus.Gatherer

	Register(prefix string, gatherer prometheus.Gatherer) error
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
	for prefix, gatherer := range g.gatherers {
		metrics, err := gatherer.Gather()
		if err != nil {
			return nil, err
		}
		for _, metric := range metrics {
			var name string
			if metric.Name != nil {
				if len(prefix) > 0 {
					name = fmt.Sprintf("%s_%s", prefix, *metric.Name)
				} else {
					name = *metric.Name
				}
			} else {
				name = prefix
			}
			metric.Name = &name
			results = append(results, metric)
		}
	}
	sortMetrics(results)
	return results, nil
}

func (g *multiGatherer) Register(prefix string, gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	if _, exists := g.gatherers[prefix]; exists {
		return errDuplicatedPrefix
	}

	g.gatherers[prefix] = gatherer
	return nil
}

type sortMetricsData []*dto.MetricFamily

func (m sortMetricsData) Less(i, j int) bool {
	iName := m[i].Name
	jName := m[j].Name
	if iName == jName {
		return false
	}
	if iName == nil {
		return true
	}
	if jName == nil {
		return false
	}
	return *iName < *jName
}
func (m sortMetricsData) Len() int      { return len(m) }
func (m sortMetricsData) Swap(i, j int) { m[j], m[i] = m[i], m[j] }

func sortMetrics(m []*dto.MetricFamily) { sort.Sort(sortMetricsData(m)) }

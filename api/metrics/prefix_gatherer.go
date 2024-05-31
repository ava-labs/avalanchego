// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"slices"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/utils/metric"

	dto "github.com/prometheus/client_model/go"
)

var _ MultiGatherer = (*prefixGatherer)(nil)

// NewPrefixGatherer returns a new MultiGatherer that merges metrics by adding a
// prefix to their names.
func NewPrefixGatherer() MultiGatherer {
	return &prefixGatherer{}
}

type prefixGatherer struct {
	multiGatherer
}

func (g *prefixGatherer) Register(prefix string, gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	// TODO: Restrict prefixes to avoid potential conflicts
	if slices.Contains(g.names, prefix) {
		return fmt.Errorf("%w: %q",
			errDuplicateGatherer,
			prefix,
		)
	}

	g.names = append(g.names, prefix)
	g.gatherers = append(g.gatherers, &prefixedGatherer{
		prefix:   prefix,
		gatherer: gatherer,
	})
	return nil
}

type prefixedGatherer struct {
	prefix   string
	gatherer prometheus.Gatherer
}

func (g *prefixedGatherer) Gather() ([]*dto.MetricFamily, error) {
	gatheredMetricFamilies, err := g.gatherer.Gather()
	if err != nil {
		return nil, err
	}

	metricFamilies := gatheredMetricFamilies[:0]
	for _, gatheredMetricFamily := range gatheredMetricFamilies {
		if gatheredMetricFamily == nil {
			continue
		}

		name := metric.AppendNamespace(
			g.prefix,
			gatheredMetricFamily.GetName(),
		)
		gatheredMetricFamily.Name = &name
		metricFamilies = append(metricFamilies, gatheredMetricFamily)
	}
	return metricFamilies, nil
}

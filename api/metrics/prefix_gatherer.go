// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"
	"slices"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

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
	// Gather returns partially filled metrics in the case of an error. So, it
	// is expected to still return the metrics in the case an error is returned.
	metricFamilies, err := g.gatherer.Gather()
	for _, metricFamily := range metricFamilies {
		metricFamily.Name = proto.String(metric.AppendNamespace(
			g.prefix,
			metricFamily.GetName(),
		))
	}
	return metricFamilies, err
}

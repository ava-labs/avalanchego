// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"errors"
	"fmt"
	"slices"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

var (
	_ MultiGatherer = (*prefixGatherer)(nil)

	errDuplicateGatherer = errors.New("attempt to register duplicate gatherer")
)

// NewLabelGatherer returns a new MultiGatherer that merges metrics by adding a
// new label.
func NewLabelGatherer(labelName string) MultiGatherer {
	return &labelGatherer{
		labelName: labelName,
	}
}

type labelGatherer struct {
	multiGatherer

	labelName string
}

func (g *labelGatherer) Register(labelValue string, gatherer prometheus.Gatherer) error {
	g.lock.Lock()
	defer g.lock.Unlock()

	if slices.Contains(g.names, labelValue) {
		return fmt.Errorf("%w: for %q with label %q",
			errDuplicateGatherer,
			g.labelName,
			labelValue,
		)
	}

	g.register(
		labelValue,
		&labeledGatherer{
			labelName:  g.labelName,
			labelValue: labelValue,
			gatherer:   gatherer,
		},
	)
	return nil
}

type labeledGatherer struct {
	labelName  string
	labelValue string
	gatherer   prometheus.Gatherer
}

func (g *labeledGatherer) Gather() ([]*dto.MetricFamily, error) {
	// Gather returns partially filled metrics in the case of an error. So, it
	// is expected to still return the metrics in the case an error is returned.
	metricFamilies, err := g.gatherer.Gather()
	for _, metricFamily := range metricFamilies {
		for _, metric := range metricFamily.Metric {
			metric.Label = append(metric.Label, &dto.LabelPair{
				Name:  &g.labelName,
				Value: &g.labelValue,
			})
		}
	}
	return metricFamilies, err
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"cmp"
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

	g.names = append(g.names, labelValue)
	g.gatherers = append(g.gatherers, &labeledGatherer{
		labelName:  g.labelName,
		labelValue: labelValue,
		gatherer:   gatherer,
	})
	return nil
}

type labeledGatherer struct {
	labelName  string
	labelValue string
	gatherer   prometheus.Gatherer
}

func (g *labeledGatherer) Gather() ([]*dto.MetricFamily, error) {
	gatheredMetricFamilies, err := g.gatherer.Gather()
	if err != nil {
		return nil, err
	}

	for _, gatheredMetricFamily := range gatheredMetricFamilies {
		if gatheredMetricFamily == nil {
			continue
		}

		metrics := gatheredMetricFamily.Metric[:0]
		for _, gatheredMetric := range gatheredMetricFamily.Metric {
			if gatheredMetric == nil {
				continue
			}

			gatheredMetric.Label = append(gatheredMetric.Label, &dto.LabelPair{
				Name:  &g.labelName,
				Value: &g.labelValue,
			})
			slices.SortFunc(gatheredMetric.Label, func(i, j *dto.LabelPair) int {
				return cmp.Compare(i.GetName(), j.GetName())
			})
			metrics = append(metrics, gatheredMetric)
		}
		gatheredMetricFamily.Metric = metrics
	}
	return gatheredMetricFamilies, nil
}

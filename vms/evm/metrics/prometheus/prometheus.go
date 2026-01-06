// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ava-labs/libevm/metrics"
	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

var (
	_ prometheus.Gatherer = (*Gatherer)(nil)

	errMetricSkip             = errors.New("metric skipped")
	errMetricTypeNotSupported = errors.New("metric type is not supported")
	quantiles                 = []float64{.5, .75, .95, .99, .999, .9999}
	pvShortPercent            = []float64{50, 95, 99}
	helpText                  = ""
)

// Gatherer implements the [prometheus.Gatherer] interface by gathering all
// metrics from a [Registry].
type Gatherer struct {
	registry Registry
}

// Gather gathers metrics from the registry and converts them to
// a slice of metric families.
func (g *Gatherer) Gather() ([]*dto.MetricFamily, error) {
	// Gather and pre-sort the metrics to avoid random listings
	var names []string
	g.registry.Each(func(name string, _ any) {
		names = append(names, name)
	})
	slices.Sort(names)

	var (
		mfs  = make([]*dto.MetricFamily, 0, len(names))
		errs []error
	)
	for _, name := range names {
		mf, err := metricFamily(g.registry, name)
		switch {
		case err == nil:
			mfs = append(mfs, mf)
		case !errors.Is(err, errMetricSkip):
			errs = append(errs, err)
		}
	}

	return mfs, errors.Join(errs...)
}

// NewGatherer returns a [Gatherer] using the given registry.
func NewGatherer(registry Registry) *Gatherer {
	return &Gatherer{
		registry: registry,
	}
}

func metricFamily(registry Registry, name string) (mf *dto.MetricFamily, err error) {
	metric := registry.Get(name)
	name = strings.ReplaceAll(name, "/", "_")

	switch m := metric.(type) {
	case metrics.NilCounter, metrics.NilCounterFloat64, metrics.NilEWMA,
		metrics.NilGauge, metrics.NilGaugeFloat64, metrics.NilGaugeInfo,
		metrics.NilHealthcheck, metrics.NilHistogram, metrics.NilMeter,
		metrics.NilResettingTimer, metrics.NilSample, metrics.NilTimer:
		return nil, fmt.Errorf("%w: %q metric is nil", errMetricSkip, name)
	case metrics.Counter:
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_COUNTER.Enum(),
			Metric: []*dto.Metric{{
				Counter: &dto.Counter{
					Value: ptrTo(float64(m.Snapshot().Count())),
				},
			}},
		}, nil
	case metrics.CounterFloat64:
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_COUNTER.Enum(),
			Metric: []*dto.Metric{{
				Counter: &dto.Counter{
					Value: ptrTo(m.Snapshot().Count()),
				},
			}},
		}, nil
	case metrics.Gauge:
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{
					Value: ptrTo(float64(m.Snapshot().Value())),
				},
			}},
		}, nil
	case metrics.GaugeFloat64:
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{
					Value: ptrTo(m.Snapshot().Value()),
				},
			}},
		}, nil
	case metrics.GaugeInfo:
		return nil, fmt.Errorf("%w: %q is a %T", errMetricSkip, name, m)
	case metrics.Histogram:
		snapshot := m.Snapshot()
		thresholds := snapshot.Percentiles(quantiles)
		dtoQuantiles := make([]*dto.Quantile, len(quantiles))
		for i := range thresholds {
			dtoQuantiles[i] = &dto.Quantile{
				Quantile: ptrTo(quantiles[i]),
				Value:    ptrTo(thresholds[i]),
			}
		}
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_SUMMARY.Enum(),
			Metric: []*dto.Metric{{
				Summary: &dto.Summary{
					SampleCount: ptrTo(uint64(snapshot.Count())),
					SampleSum:   ptrTo(float64(snapshot.Sum())),
					Quantile:    dtoQuantiles,
				},
			}},
		}, nil
	case metrics.Meter:
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{
					Value: ptrTo(float64(m.Snapshot().Count())),
				},
			}},
		}, nil
	case metrics.Timer:
		snapshot := m.Snapshot()
		thresholds := snapshot.Percentiles(quantiles)
		dtoQuantiles := make([]*dto.Quantile, len(quantiles))
		for i := range thresholds {
			dtoQuantiles[i] = &dto.Quantile{
				Quantile: ptrTo(quantiles[i]),
				Value:    ptrTo(thresholds[i]),
			}
		}
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_SUMMARY.Enum(),
			Metric: []*dto.Metric{{
				Summary: &dto.Summary{
					SampleCount: ptrTo(uint64(snapshot.Count())),
					SampleSum:   ptrTo(float64(snapshot.Sum())),
					Quantile:    dtoQuantiles,
				},
			}},
		}, nil
	case metrics.ResettingTimer:
		snapshot := m.Snapshot()
		thresholds := snapshot.Percentiles(pvShortPercent)
		dtoQuantiles := make([]*dto.Quantile, len(pvShortPercent))
		for i := range pvShortPercent {
			dtoQuantiles[i] = &dto.Quantile{
				Quantile: ptrTo(pvShortPercent[i]),
				Value:    ptrTo(thresholds[i]),
			}
		}
		count := snapshot.Count()
		return &dto.MetricFamily{
			Name: &name,
			Help: &helpText,
			Type: dto.MetricType_SUMMARY.Enum(),
			Metric: []*dto.Metric{{
				Summary: &dto.Summary{
					SampleCount: ptrTo(uint64(count)),
					SampleSum:   ptrTo(float64(count) * snapshot.Mean()),
					Quantile:    dtoQuantiles,
				},
			}},
		}, nil
	default:
		return nil, fmt.Errorf("%w: metric %q type %T", errMetricTypeNotSupported, name, metric)
	}
}

func ptrTo[T any](x T) *T { return &x }

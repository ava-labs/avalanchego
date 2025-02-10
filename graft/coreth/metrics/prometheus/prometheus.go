// (c) 2021-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ethereum/go-ethereum/metrics"

	dto "github.com/prometheus/client_model/go"
)

// Gatherer implements [prometheus.Gatherer] interface by
// gathering all metrics from the given Prometheus registry.
type Gatherer struct {
	registry Registry
}

var _ prometheus.Gatherer = (*Gatherer)(nil)

// NewGatherer returns a [Gatherer] using the given registry.
func NewGatherer(registry Registry) *Gatherer {
	return &Gatherer{
		registry: registry,
	}
}

// Gather gathers metrics from the registry and converts them to
// a slice of metric families.
func (g *Gatherer) Gather() (mfs []*dto.MetricFamily, err error) {
	// Gather and pre-sort the metrics to avoid random listings
	var names []string
	g.registry.Each(func(name string, i any) {
		names = append(names, name)
	})
	sort.Strings(names)

	mfs = make([]*dto.MetricFamily, 0, len(names))
	for _, name := range names {
		mf, err := metricFamily(g.registry, name)
		if err != nil {
			if errors.Is(err, errMetricSkip) {
				continue
			}
			return nil, err
		}
		mfs = append(mfs, mf)
	}

	return mfs, nil
}

var (
	errMetricSkip = errors.New("metric skipped")
)

func ptrTo[T any](x T) *T { return &x }

func metricFamily(registry Registry, name string) (mf *dto.MetricFamily, err error) {
	metric := registry.Get(name)
	name = strings.ReplaceAll(name, "/", "_")

	switch m := metric.(type) {
	case metrics.Counter:
		return &dto.MetricFamily{
			Name: &name,
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
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{
					Value: ptrTo(m.Snapshot().Value()),
				},
			}},
		}, nil
	case metrics.Histogram:
		snapshot := m.Snapshot()

		quantiles := []float64{.5, .75, .95, .99, .999, .9999}
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
			Type: dto.MetricType_SUMMARY.Enum(),
			Metric: []*dto.Metric{{
				Summary: &dto.Summary{
					SampleCount: ptrTo(uint64(snapshot.Count())), //nolint:gosec
					SampleSum:   ptrTo(float64(snapshot.Sum())),
					Quantile:    dtoQuantiles,
				},
			}},
		}, nil
	case metrics.Meter:
		return &dto.MetricFamily{
			Name: &name,
			Type: dto.MetricType_GAUGE.Enum(),
			Metric: []*dto.Metric{{
				Gauge: &dto.Gauge{
					Value: ptrTo(float64(m.Snapshot().Count())),
				},
			}},
		}, nil
	case metrics.Timer:
		snapshot := m.Snapshot()

		quantiles := []float64{.5, .75, .95, .99, .999, .9999}
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
			Type: dto.MetricType_SUMMARY.Enum(),
			Metric: []*dto.Metric{{
				Summary: &dto.Summary{
					SampleCount: ptrTo(uint64(snapshot.Count())), //nolint:gosec
					SampleSum:   ptrTo(float64(snapshot.Sum())),
					Quantile:    dtoQuantiles,
				},
			}},
		}, nil
	case metrics.ResettingTimer:
		snapshot := m.Snapshot()
		if snapshot.Count() == 0 {
			return nil, fmt.Errorf("%w: %q resetting timer metric count is zero", errMetricSkip, name)
		}

		pvShortPercent := []float64{50, 95, 99}
		thresholds := snapshot.Percentiles(pvShortPercent)
		dtoQuantiles := make([]*dto.Quantile, len(pvShortPercent))
		for i := range pvShortPercent {
			dtoQuantiles[i] = &dto.Quantile{
				Quantile: ptrTo(pvShortPercent[i]),
				Value:    ptrTo(thresholds[i]),
			}
		}

		return &dto.MetricFamily{
			Name: &name,
			Type: dto.MetricType_SUMMARY.Enum(),
			Metric: []*dto.Metric{{
				Summary: &dto.Summary{
					SampleCount: ptrTo(uint64(snapshot.Count())), //nolint:gosec
					Quantile:    dtoQuantiles,
				},
			}},
		}, nil
	default:
		return nil, fmt.Errorf("metric %q: type is not supported: %T", name, metric)
	}
}

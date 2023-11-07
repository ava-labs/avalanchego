// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import (
	"sort"
	"strings"

	"github.com/ava-labs/subnet-evm/metrics"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"
)

var (
	pv             = []float64{.5, .75, .95, .99, .999, .9999}
	pvShortPercent = []float64{50, 95, 99}
	pvShort        = []float64{.50, .95, .99}
)

type gatherer struct {
	reg metrics.Registry
}

func (g gatherer) Gather() ([]*dto.MetricFamily, error) {
	// Gather and pre-sort the metrics to avoid random listings
	var names []string
	g.reg.Each(func(name string, i interface{}) {
		names = append(names, name)
	})
	sort.Strings(names)

	mfs := make([]*dto.MetricFamily, 0, len(names))
	for _, name := range names {
		mIntf := g.reg.Get(name)
		name := strings.Replace(name, "/", "_", -1)

		switch m := mIntf.(type) {
		case metrics.Counter:
			val := m.Snapshot().Count()
			valFloat := float64(val)
			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_COUNTER.Enum(),
				Metric: []*dto.Metric{{
					Counter: &dto.Counter{
						Value: &valFloat,
					},
				}},
			})
		case metrics.CounterFloat64:
			val := m.Snapshot().Count()
			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_COUNTER.Enum(),
				Metric: []*dto.Metric{{
					Counter: &dto.Counter{
						Value: &val,
					},
				}},
			})
		case metrics.Gauge:
			val := m.Snapshot().Value()
			valFloat := float64(val)
			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{
					Gauge: &dto.Gauge{
						Value: &valFloat,
					},
				}},
			})
		case metrics.GaugeFloat64:
			val := m.Snapshot().Value()
			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{
					Gauge: &dto.Gauge{
						Value: &val,
					},
				}},
			})
		case metrics.Histogram:
			snapshot := m.Snapshot()
			count := snapshot.Count()
			countUint := uint64(count)
			sum := snapshot.Sum()
			sumFloat := float64(sum)

			ps := snapshot.Percentiles(pv)
			qs := make([]*dto.Quantile, len(pv))
			for i := range ps {
				v := pv[i]
				s := ps[i]
				qs[i] = &dto.Quantile{
					Quantile: &v,
					Value:    &s,
				}
			}

			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_SUMMARY.Enum(),
				Metric: []*dto.Metric{{
					Summary: &dto.Summary{
						SampleCount: &countUint,
						SampleSum:   &sumFloat,
						Quantile:    qs,
					},
				}},
			})
		case metrics.Meter:
			val := m.Snapshot().Count()
			valFloat := float64(val)
			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_GAUGE.Enum(),
				Metric: []*dto.Metric{{
					Gauge: &dto.Gauge{
						Value: &valFloat,
					},
				}},
			})
		case metrics.Timer:
			snapshot := m.Snapshot()
			count := snapshot.Count()
			countUint := uint64(count)
			sum := snapshot.Sum()
			sumFloat := float64(sum)

			ps := snapshot.Percentiles(pv)
			qs := make([]*dto.Quantile, len(pv))
			for i := range ps {
				v := pv[i]
				s := ps[i]
				qs[i] = &dto.Quantile{
					Quantile: &v,
					Value:    &s,
				}
			}

			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_SUMMARY.Enum(),
				Metric: []*dto.Metric{{
					Summary: &dto.Summary{
						SampleCount: &countUint,
						SampleSum:   &sumFloat,
						Quantile:    qs,
					},
				}},
			})
		case metrics.ResettingTimer:
			snapshot := m.Snapshot()

			vals := snapshot.Values()
			count := uint64(len(vals))
			if count == 0 {
				continue
			}

			ps := snapshot.Percentiles(pvShortPercent)
			qs := make([]*dto.Quantile, len(pv))
			for i := range pvShort {
				v := pv[i]
				s := float64(ps[i])
				qs[i] = &dto.Quantile{
					Quantile: &v,
					Value:    &s,
				}
			}

			mfs = append(mfs, &dto.MetricFamily{
				Name: &name,
				Type: dto.MetricType_SUMMARY.Enum(),
				Metric: []*dto.Metric{{
					Summary: &dto.Summary{
						SampleCount: &count,
						// TODO: do we need to specify SampleSum here? and if so
						// what should that be?
						Quantile: qs,
					},
				}},
			})
		}
	}

	return mfs, nil
}

func Gatherer(reg metrics.Registry) prometheus.Gatherer {
	return gatherer{reg: reg}
}

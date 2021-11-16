// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	dto "github.com/prometheus/client_model/go"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/vmproto"
)

func (vm *VMServer) Gather(
	context.Context,
	*emptypb.Empty,
) (*vmproto.GatherResponse, error) {
	mfs, err := vm.ctx.Metrics.Gather()
	if err != nil {
		return nil, err
	}
	resp := &vmproto.GatherResponse{
		MetricFamilies: make([]*vmproto.MetricFamily, len(mfs)),
	}
	for i, mf := range mfs {
		resp.MetricFamilies[i] = convertFromMetricFamily(mf)
	}
	return resp, nil
}

func convertFromMetricFamily(mf *dto.MetricFamily) *vmproto.MetricFamily {
	if mf == nil {
		return nil
	}
	return &vmproto.MetricFamily{
		Name:   mf.Name,
		Help:   mf.Help,
		Type:   (*vmproto.MetricType)(mf.Type),
		Metric: convertFromMetrics(mf.Metric),
	}
}

func convertFromMetrics(ms []*dto.Metric) []*vmproto.Metric {
	newMs := make([]*vmproto.Metric, len(ms))
	for i, m := range ms {
		newMs[i] = convertFromMetric(m)
	}
	return newMs
}

func convertFromMetric(m *dto.Metric) *vmproto.Metric {
	if m == nil {
		return nil
	}
	return &vmproto.Metric{
		Label:       convertFromLabelPairs(m.Label),
		Gauge:       convertFromGauge(m.Gauge),
		Counter:     convertFromCounter(m.Counter),
		Summary:     convertFromSummary(m.Summary),
		Untyped:     convertFromUntyped(m.Untyped),
		Histogram:   convertFromHistogram(m.Histogram),
		TimestampMs: m.TimestampMs,
	}
}

func convertFromLabelPairs(lps []*dto.LabelPair) []*vmproto.LabelPair {
	newLps := make([]*vmproto.LabelPair, len(lps))
	for i, lp := range lps {
		newLps[i] = convertFromLabelPair(lp)
	}
	return newLps
}

func convertFromLabelPair(lp *dto.LabelPair) *vmproto.LabelPair {
	if lp == nil {
		return nil
	}
	return &vmproto.LabelPair{
		Name:  lp.Name,
		Value: lp.Value,
	}
}

func convertFromGauge(g *dto.Gauge) *vmproto.Gauge {
	if g == nil {
		return nil
	}
	return &vmproto.Gauge{
		Value: g.Value,
	}
}

func convertFromCounter(c *dto.Counter) *vmproto.Counter {
	if c == nil {
		return nil
	}
	return &vmproto.Counter{
		Value:    c.Value,
		Exemplar: convertFromExemplar(c.Exemplar),
	}
}

func convertFromExemplar(e *dto.Exemplar) *vmproto.Exemplar {
	if e == nil {
		return nil
	}
	return &vmproto.Exemplar{
		Label:     convertFromLabelPairs(e.Label),
		Value:     e.Value,
		Timestamp: e.Timestamp,
	}
}

func convertFromSummary(s *dto.Summary) *vmproto.Summary {
	if s == nil {
		return nil
	}
	return &vmproto.Summary{
		SampleCount: s.SampleCount,
		SampleSum:   s.SampleSum,
		Quantile:    convertFromQuantiles(s.Quantile),
	}
}

func convertFromQuantiles(qs []*dto.Quantile) []*vmproto.Quantile {
	newQs := make([]*vmproto.Quantile, len(qs))
	for i, q := range qs {
		newQs[i] = convertFromQuantile(q)
	}
	return newQs
}

func convertFromQuantile(q *dto.Quantile) *vmproto.Quantile {
	if q == nil {
		return nil
	}
	return &vmproto.Quantile{
		Quantile: q.Quantile,
		Value:    q.Value,
	}
}

func convertFromUntyped(u *dto.Untyped) *vmproto.Untyped {
	if u == nil {
		return nil
	}
	return &vmproto.Untyped{
		Value: u.Value,
	}
}

func convertFromHistogram(h *dto.Histogram) *vmproto.Histogram {
	if h == nil {
		return nil
	}
	return &vmproto.Histogram{
		SampleCount: h.SampleCount,
		SampleSum:   h.SampleSum,
		Bucket:      convertFromBuckets(h.Bucket),
	}
}

func convertFromBuckets(bs []*dto.Bucket) []*vmproto.Bucket {
	newBs := make([]*vmproto.Bucket, len(bs))
	for i, q := range bs {
		newBs[i] = convertFromBucket(q)
	}
	return newBs
}

func convertFromBucket(b *dto.Bucket) *vmproto.Bucket {
	if b == nil {
		return nil
	}
	return &vmproto.Bucket{
		CumulativeCount: b.CumulativeCount,
		UpperBound:      b.UpperBound,
		Exemplar:        convertFromExemplar(b.Exemplar),
	}
}

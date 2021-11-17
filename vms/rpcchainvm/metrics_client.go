// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/vms/rpcchainvm/vmproto"
)

var _ prometheus.Gatherer = &VMClient{}

func (vm *VMClient) Gather() ([]*dto.MetricFamily, error) {
	resp, err := vm.client.Gather(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	newResp := make([]*dto.MetricFamily, len(resp.MetricFamilies))
	for i, mf := range resp.MetricFamilies {
		newResp[i] = convertToMetricFamily(mf)
	}
	return newResp, nil
}

func convertToMetricFamily(mf *vmproto.MetricFamily) *dto.MetricFamily {
	if mf == nil {
		return nil
	}
	return &dto.MetricFamily{
		Name:   mf.Name,
		Help:   mf.Help,
		Type:   (*dto.MetricType)(mf.Type),
		Metric: convertToMetrics(mf.Metric),
	}
}

func convertToMetrics(ms []*vmproto.Metric) []*dto.Metric {
	newMs := make([]*dto.Metric, len(ms))
	for i, m := range ms {
		newMs[i] = convertToMetric(m)
	}
	return newMs
}

func convertToMetric(m *vmproto.Metric) *dto.Metric {
	if m == nil {
		return nil
	}
	return &dto.Metric{
		Label:       convertToLabelPairs(m.Label),
		Gauge:       convertToGauge(m.Gauge),
		Counter:     convertToCounter(m.Counter),
		Summary:     convertToSummary(m.Summary),
		Untyped:     convertToUntyped(m.Untyped),
		Histogram:   convertToHistogram(m.Histogram),
		TimestampMs: m.TimestampMs,
	}
}

func convertToLabelPairs(lps []*vmproto.LabelPair) []*dto.LabelPair {
	newLps := make([]*dto.LabelPair, len(lps))
	for i, lp := range lps {
		newLps[i] = convertToLabelPair(lp)
	}
	return newLps
}

func convertToLabelPair(lp *vmproto.LabelPair) *dto.LabelPair {
	if lp == nil {
		return nil
	}
	return &dto.LabelPair{
		Name:  lp.Name,
		Value: lp.Value,
	}
}

func convertToGauge(g *vmproto.Gauge) *dto.Gauge {
	if g == nil {
		return nil
	}
	return &dto.Gauge{
		Value: g.Value,
	}
}

func convertToCounter(c *vmproto.Counter) *dto.Counter {
	if c == nil {
		return nil
	}
	return &dto.Counter{
		Value:    c.Value,
		Exemplar: convertToExemplar(c.Exemplar),
	}
}

func convertToExemplar(e *vmproto.Exemplar) *dto.Exemplar {
	if e == nil {
		return nil
	}
	return &dto.Exemplar{
		Label:     convertToLabelPairs(e.Label),
		Value:     e.Value,
		Timestamp: e.Timestamp,
	}
}

func convertToSummary(s *vmproto.Summary) *dto.Summary {
	if s == nil {
		return nil
	}
	return &dto.Summary{
		SampleCount: s.SampleCount,
		SampleSum:   s.SampleSum,
		Quantile:    convertToQuantiles(s.Quantile),
	}
}

func convertToQuantiles(qs []*vmproto.Quantile) []*dto.Quantile {
	newQs := make([]*dto.Quantile, len(qs))
	for i, q := range qs {
		newQs[i] = convertToQuantile(q)
	}
	return newQs
}

func convertToQuantile(q *vmproto.Quantile) *dto.Quantile {
	if q == nil {
		return nil
	}
	return &dto.Quantile{
		Quantile: q.Quantile,
		Value:    q.Value,
	}
}

func convertToUntyped(u *vmproto.Untyped) *dto.Untyped {
	if u == nil {
		return nil
	}
	return &dto.Untyped{
		Value: u.Value,
	}
}

func convertToHistogram(h *vmproto.Histogram) *dto.Histogram {
	if h == nil {
		return nil
	}
	return &dto.Histogram{
		SampleCount: h.SampleCount,
		SampleSum:   h.SampleSum,
		Bucket:      convertToBuckets(h.Bucket),
	}
}

func convertToBuckets(bs []*vmproto.Bucket) []*dto.Bucket {
	newBs := make([]*dto.Bucket, len(bs))
	for i, q := range bs {
		newBs[i] = convertToBucket(q)
	}
	return newBs
}

func convertToBucket(b *vmproto.Bucket) *dto.Bucket {
	if b == nil {
		return nil
	}
	return &dto.Bucket{
		CumulativeCount: b.CumulativeCount,
		UpperBound:      b.UpperBound,
		Exemplar:        convertToExemplar(b.Exemplar),
	}
}

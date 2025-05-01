// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestMetrics(t *testing.T) {
	require := require.New(t)
	metrics, err := NewMetrics(prometheus.NewRegistry(), "gossip_metrics")
	require.NoError(err)
	gossipableIds := []ids.ID{
		ids.GenerateTestID(), // success, not dropped
		ids.GenerateTestID(), // dropped, duplicate
		ids.GenerateTestID(), // dropped, custom label
		ids.GenerateTestID(), // parsing error, malformed
	}
	gossipableSizes := map[ids.ID]int{
		gossipableIds[0]: 1,
		gossipableIds[1]: 5,
		gossipableIds[2]: 7,
	}
	const customLabel = "custom"
	gossipableLabels := []string{
		droppedNot,
		DroppedDuplicate,
		customLabel,
	}
	for i, label := range gossipableLabels {
		metrics.AddDropMetric(gossipableIds[i], label)
	}
	const (
		totalMalformedSize  = 15
		totalMalformedCount = 23
	)
	require.NoError(metrics.updateReceivedMetrics(pullType, gossipableSizes, totalMalformedSize, totalMalformedCount))

	// verify that the metrics have been added correctly.
	for i, label := range gossipableLabels {
		bytesCounter, err := metrics.receivedBytes.GetMetricWith(prometheus.Labels{
			ioLabel:      receivedIO,
			typeLabel:    pullType,
			droppedLabel: label,
		})
		require.NoError(err)

		require.Equal(float64(gossipableSizes[gossipableIds[i]]), testutil.ToFloat64(bytesCounter))

		gossipableCount, err := metrics.receivedCount.GetMetricWith(prometheus.Labels{
			ioLabel:      receivedIO,
			typeLabel:    pullType,
			droppedLabel: label,
		})
		require.NoError(err)

		require.Equal(float64(1), testutil.ToFloat64(gossipableCount))
	}
	// ensure correctness of malformed count ( since we don't have gossipableID for that )
	bytesCounter, err := metrics.receivedBytes.GetMetricWith(prometheus.Labels{
		ioLabel:      receivedIO,
		typeLabel:    pullType,
		droppedLabel: droppedMalformed,
	})
	require.NoError(err)

	require.Equal(float64(totalMalformedSize), testutil.ToFloat64(bytesCounter))

	gossipableCount, err := metrics.receivedCount.GetMetricWith(prometheus.Labels{
		ioLabel:      receivedIO,
		typeLabel:    pullType,
		droppedLabel: droppedMalformed,
	})
	require.NoError(err)

	require.Equal(float64(totalMalformedCount), testutil.ToFloat64(gossipableCount))
}

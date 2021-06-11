// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metrics

import (
	"fmt"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/prometheus/client_golang/prometheus"
)

func NewNanosecnodsLatencyMetric(namespace, name string) prometheus.Histogram {
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      name,
		Help:      fmt.Sprintf("Latency of a %s call in nanoseconds", name),
		Buckets:   utils.NanosecondsBuckets,
	})
}

// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package metric

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
)

func NewNanosecondsLatencyMetric(namespace, name string) prometheus.Histogram {
	return prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      name,
		Help:      fmt.Sprintf("Latency of a %s call in nanoseconds", name),
		Buckets:   NanosecondsBuckets,
	})
}

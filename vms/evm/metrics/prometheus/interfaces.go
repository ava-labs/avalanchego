// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package prometheus

import "github.com/ava-labs/libevm/metrics"

var _ Registry = metrics.Registry(nil)

type Registry interface {
	// Call the given function for each registered metric.
	Each(func(name string, metric any))
	// Get the metric by the given name or nil if none is registered.
	Get(name string) any
}

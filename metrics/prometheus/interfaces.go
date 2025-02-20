// (c) 2025 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package prometheus

// Registry is a narrower interface of [prometheus.Registry] containing
// only the required functions for the [Gatherer].
type Registry interface {
	// Call the given function for each registered metric.
	Each(func(string, any))
	// Get the metric by the given name or nil if none is registered.
	Get(string) any
}

// (c) 2024 Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package core

import "github.com/ava-labs/libevm/metrics"

// getOrOverrideAsRegisteredCounter searches for a metric already registered
// with`name`. If a metric is found and it is a Counter, it is returned. If a
// metric is found and it is not a Counter, it is unregistered and replaced with
// a new registered Counter. If no metric is found, a new Counter is constructed
// and registered.
//
// This is necessary for a metric defined in libevm with the same name but a
// different type to what we expect.
func getOrOverrideAsRegisteredCounter(name string, r metrics.Registry) metrics.Counter {
	if r == nil {
		r = metrics.DefaultRegistry
	}

	switch c := r.GetOrRegister(name, metrics.NewCounter).(type) {
	case metrics.Counter:
		return c
	default: // `name` must have already been registered to be any other type
		r.Unregister(name)
		return metrics.NewRegisteredCounter(name, r)
	}
}

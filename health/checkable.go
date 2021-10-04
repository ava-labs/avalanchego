// (c) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

// Checkable can have its health checked
type Checkable interface {
	// HealthCheck returns health check results and,
	// if not healthy, a non-nil error
	HealthCheck() (interface{}, error)
}

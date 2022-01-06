// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

// Checker can have its health checked
type Checker interface {
	// HealthCheck returns health check results and, if not healthy, a non-nil
	// error
	HealthCheck() (interface{}, error)
}

type CheckerFunc func() (interface{}, error)

func (f CheckerFunc) HealthCheck() (interface{}, error) { return f() }

// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import "context"

var _ Checker = CheckerFunc(nil)

// Checker can have its health checked
type Checker interface {
	// HealthCheck returns health check results and, if not healthy, a non-nil
	// error
	//
	// It is expected that the results are json marshallable.
	HealthCheck(context.Context) (any, error)
}

type CheckerFunc func(context.Context) (any, error)

func (f CheckerFunc) HealthCheck(ctx context.Context) (any, error) {
	return f(ctx)
}

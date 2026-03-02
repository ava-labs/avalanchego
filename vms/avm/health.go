// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import "context"

// TODO: add health checks
func (*VM) HealthCheck(context.Context) (interface{}, error) {
	return nil, nil
}

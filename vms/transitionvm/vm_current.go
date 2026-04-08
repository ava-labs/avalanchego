// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package transitionvm

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
)

func (v *VM) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	return v.current().AppGossip(ctx, nodeID, msg)
}

func (v *VM) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	return v.current().AppRequest(ctx, nodeID, requestID, deadline, request)
}

func (v *VM) HealthCheck(ctx context.Context) (interface{}, error) {
	return v.current().HealthCheck(ctx)
}

func (v *VM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return v.current().LastAccepted(ctx)
}

func (v *VM) Version(ctx context.Context) (string, error) {
	return v.current().Version(ctx)
}

func (v *VM) current() chain {
	panic(errUnimplemented)
}

// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var ErrNotConnectedEnoughStake = errors.New("not connected to enough stake")

func (h *handler) HealthCheck(ctx context.Context) (interface{}, error) {
	state := h.ctx.State.Get()
	engine, ok := h.engineManager.Get(state.Type).Get(state.State)
	if !ok {
		return nil, fmt.Errorf(
			"%w %s running %s",
			errMissingEngine,
			state.State,
			state.Type,
		)
	}
	engineIntf, engineErr := engine.HealthCheck(ctx)
	networkingIntf, networkingErr := h.networkHealthCheck()
	intf := map[string]interface{}{
		"engine":     engineIntf,
		"networking": networkingIntf,
	}
	if engineErr == nil {
		return intf, networkingErr
	}
	if networkingErr == nil {
		return intf, engineErr
	}
	return intf, fmt.Errorf("engine: %w; networking: %w", engineErr, networkingErr)
}

func (h *handler) networkHealthCheck() (interface{}, error) {
	percentConnected := h.peerTracker.ConnectedPercent()
	details := map[string]interface{}{
		"percentConnected":       percentConnected,
		"disconnectedValidators": h.getDisconnectedValidators(),
	}

	subnetConfig := h.subnet.Config()

	if subnetConfig.ConsensusParameters.SimplexParams != nil {
		return details, nil
	}

	var err error
	minPercentConnected := subnetConfig.ConsensusParameters.SnowballParams.MinPercentConnectedHealthy()
	if percentConnected < minPercentConnected {
		err = fmt.Errorf("%w: connected to %f%%; required at least %f%%",
			ErrNotConnectedEnoughStake,
			percentConnected*100,
			minPercentConnected*100,
		)
	}

	return details, err
}

func (h *handler) getDisconnectedValidators() set.Set[ids.NodeID] {
	vdrs := h.peerTracker.GetValidators()
	connectedVdrs := h.peerTracker.ConnectedValidators()
	// vdrs - connectedVdrs is equal to the disconnectedVdrs
	vdrs.Difference(connectedVdrs)
	return vdrs
}

// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"fmt"
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
	details := map[string]float64{
		"percentConnected": percentConnected,
	}

	var err error
	subnetConfig := h.subnet.Config()
	minPercentConnected := subnetConfig.ConsensusParameters.MinPercentConnectedHealthy()
	if percentConnected < minPercentConnected {
		err = fmt.Errorf("%w: connected to %f%%; required at least %f%%",
			ErrNotConnectedEnoughStake,
			percentConnected*100,
			minPercentConnected*100,
		)
	}

	return details, err
}

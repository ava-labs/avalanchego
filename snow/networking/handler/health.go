// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"fmt"
)

func (h *handler) HealthCheck(ctx context.Context) (interface{}, error) {
	h.ctx.Lock.Lock()
	defer h.ctx.Lock.Unlock()

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
	return intf, fmt.Errorf("engine: %w ; networking: %v", engineErr, networkingErr)
}

func (h *handler) networkHealthCheck() (interface{}, error) {
	percentConnected := h.getPercentConnected()
	details := map[string]float64{
		"percentConnected": percentConnected,
	}
	h.metrics.SetPercentConnected(percentConnected)

	var err error
	if percentConnected < h.ctx.MinPercentConnectedStakeHealthy {
		err = getPercentErr(percentConnected, h.ctx.MinPercentConnectedStakeHealthy)
	}

	return details, err
}

func (h *handler) getPercentConnected() float64 {
	vdrSetWeight := h.validators.Weight()
	if vdrSetWeight == 0 {
		return 1
	}

	connectedStake := h.peerTracker.ConnectedWeight()
	return float64(connectedStake) / float64(vdrSetWeight)
}

func getPercentErr(percentConnected float64, minPercentConnected float64) error {
	if percentConnected < minPercentConnected {
		return fmt.Errorf("connected to %f%% of network stake; should be connected to at least %f%%",
			percentConnected*100,
			minPercentConnected*100,
		)
	}
	return nil
}

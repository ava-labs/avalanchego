// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package handler

import (
	"context"
	"errors"
	"fmt"
)

var ErrNotConnectedEnoughStake = errors.New("not connected to enough stake")

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
	// TODO: Update this to return both errors with %w once we upgrade to Go 1.20
	return intf, fmt.Errorf("engine: %v; networking: %v", engineErr, networkingErr)
}

func (h *handler) networkHealthCheck() (interface{}, error) {
	percentConnected := h.getPercentConnected()
	details := map[string]float64{
		"percentConnected": percentConnected,
	}
	h.metrics.SetPercentConnected(percentConnected)

	var err error
	subnetConfig := h.subnet.Config()
	minPercentStake := subnetConfig.ConsensusParameters.MinPercentConnectedStakeHealthy()
	if percentConnected < minPercentStake {
		err = fmt.Errorf("%w: connected to %f%%; required at least %f%%",
			ErrNotConnectedEnoughStake,
			percentConnected*100,
			minPercentStake*100,
		)
	}

	return details, err
}

func (h *handler) getPercentConnected() float64 {
	vdrSetWeight := h.peerTracker.Weight()
	if vdrSetWeight == 0 {
		return 1
	}

	connectedStake := h.peerTracker.ConnectedWeight()
	return float64(connectedStake) / float64(vdrSetWeight)
}

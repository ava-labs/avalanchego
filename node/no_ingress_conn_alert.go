// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// ErrNoIngressConnections denotes that no node is connected to this validator.
var ErrNoIngressConnections = errors.New("no ingress connections")

type ingressConnectionCounter interface {
	IngressConnCount() int
}

type validatorRetriever interface {
	GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool)
}

type bootstrappingTracker interface {
	Bootstrapping() []ids.ID
}

type noIngressConnAlert struct {
	lock               sync.Mutex
	lastError          error
	lastResult         interface{}
	lastCheck          time.Time
	minCheckInterval   time.Duration
	now                func() time.Time
	selfID             ids.NodeID
	ingressConnections ingressConnectionCounter
	validators         validatorRetriever
	bootstrapping      bootstrappingTracker
}

func ingressConnResult(n int, areWeValidator bool) map[string]interface{} {
	return map[string]interface{}{"ingressConnectionCount": n, "primary network validator": areWeValidator}
}

func (nica *noIngressConnAlert) HealthCheck(context.Context) (interface{}, error) {
	nica.lock.Lock()
	defer nica.lock.Unlock()

	if len(nica.bootstrapping.Bootstrapping()) > 0 {
		return nil, nil
	}

	now := nica.now()

	if nica.lastCheck.IsZero() {
		nica.lastCheck = now
		return nica.lastResult, nica.lastError
	}

	if now.Sub(nica.lastCheck) < nica.minCheckInterval && nica.lastResult == nil {
		return nica.lastResult, nica.lastError
	}

	nica.lastCheck = now

	connCount := nica.ingressConnections.IngressConnCount()
	_, areWeValidator := nica.validators.GetValidator(constants.PrimaryNetworkID, nica.selfID)

	if connCount > 0 {
		nica.clearError(connCount, areWeValidator)
		return nica.lastResult, nica.lastError
	}

	if !areWeValidator {
		nica.clearError(connCount, areWeValidator)
		return nica.lastResult, nica.lastError
	}

	nica.setError(connCount, areWeValidator)

	return nica.lastResult, nica.lastError
}

func (nica *noIngressConnAlert) setError(n int, areWeValidator bool) {
	nica.lastResult, nica.lastError = ingressConnResult(n, areWeValidator), ErrNoIngressConnections
}

func (nica *noIngressConnAlert) clearError(n int, areWeValidator bool) {
	nica.lastResult, nica.lastError = ingressConnResult(n, areWeValidator), nil
}

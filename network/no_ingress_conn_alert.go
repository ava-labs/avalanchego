// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"

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

type noIngressConnAlert struct {
	selfID             ids.NodeID
	ingressConnections ingressConnectionCounter
	validators         validatorRetriever
}

func ingressConnResult(n int, areWeValidator bool) map[string]interface{} {
	return map[string]interface{}{"ingressConnectionCount": n, "primary network validator": areWeValidator}
}

func (nica *noIngressConnAlert) checkHealth() (interface{}, error) {
	connCount := nica.ingressConnections.IngressConnCount()
	_, areWeValidator := nica.validators.GetValidator(constants.PrimaryNetworkID, nica.selfID)

	if connCount > 0 {
		return ingressConnResult(connCount, areWeValidator), nil
	}

	if !areWeValidator {
		return ingressConnResult(connCount, areWeValidator), nil
	}

	return ingressConnResult(connCount, areWeValidator), ErrNoIngressConnections
}

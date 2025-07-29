// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
)

// ErrNoIngressConnections denotes that no node is connected to this validator.
var ErrNoIngressConnections = errors.New("primary network validator has no inbound connections")

type ingressConnectionCounter interface {
	IngressConnCount() int
}

type validatorRetriever interface {
	GetValidator(subnetID ids.ID, nodeID ids.NodeID) (*validators.Validator, bool)
}

func checkNoIngressConnections(selfID ids.NodeID, ingressConnections ingressConnectionCounter, validators validatorRetriever) (interface{}, error) {
	connCount := ingressConnections.IngressConnCount()
	_, areWeValidator := validators.GetValidator(constants.PrimaryNetworkID, selfID)

	result := map[string]interface{}{
		"ingressConnectionCount":  connCount,
		"primaryNetworkValidator": areWeValidator,
	}

	if connCount > 0 || !areWeValidator {
		return result, nil
	}

	return result, ErrNoIngressConnections
}

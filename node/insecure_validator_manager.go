// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

type insecureValidatorManager struct {
	router.Router
	vdrs   validators.Set
	weight uint64
}

func (i *insecureValidatorManager) Connected(vdrID ids.GenericNodeID, nodeVersion *version.Application, subnetID ids.ID) {
	if constants.PrimaryNetworkID == subnetID {
		// Sybil protection is disabled so we don't have a txID that added the
		// peer as a validator. Because each validator needs a txID associated
		// with it, we hack one together by padding the nodeID with zeroes.
		dummyTxID := ids.Empty
		copy(ids.Writable(&dummyTxID), vdrID.Bytes())

		// Add will only error here if the total weight of the set would go over
		// [math.MaxUint64]. In this case, we will just not mark this new peer
		// as a validator.
		_ = i.vdrs.Add(vdrID, nil, dummyTxID, i.weight)
	}
	i.Router.Connected(vdrID, nodeVersion, subnetID)
}

func (i *insecureValidatorManager) Disconnected(vdrID ids.GenericNodeID) {
	// RemoveWeight will only error here if there was an error reported during
	// Add.
	_ = i.vdrs.RemoveWeight(vdrID, i.weight)
	i.Router.Disconnected(vdrID)
}

// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/networking/router"
	"github.com/ava-labs/avalanchego/version"
)

var _ router.ExternalHandler = (*testHandler)(nil)

type testHandler struct {
	router.InboundHandler
	ConnectedF    func(nodeID ids.GenericNodeID, nodeVersion *version.Application, subnetID ids.ID)
	DisconnectedF func(nodeID ids.GenericNodeID)
}

func (h *testHandler) Connected(id ids.GenericNodeID, nodeVersion *version.Application, subnetID ids.ID) {
	if h.ConnectedF != nil {
		h.ConnectedF(id, nodeVersion, subnetID)
	}
}

func (h *testHandler) Disconnected(id ids.GenericNodeID) {
	if h.DisconnectedF != nil {
		h.DisconnectedF(id)
	}
}

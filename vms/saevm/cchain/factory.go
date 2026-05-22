// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package cchain

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/evm/acp226"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct{}

func (*Factory) New(logger logging.Logger) (interface{}, error) {
	logger.Info("Creating new SAE VM")
	return adaptor.Convert(&VM{
		pullGossipPeriod:      time.Second,
		pushGossipPeriod:      100 * time.Millisecond,
		initialMinDelayExcess: acp226.InitialDelayExcess,
	}), nil
}

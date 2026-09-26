// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package l1s

import (
	"time"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/saevm/adaptor"
)

var _ vms.Factory = (*Factory)(nil)

// Factory creates new L1 VMs.
type Factory struct{}

// New creates a new L1 VM.
func (*Factory) New(log logging.Logger) (any, error) {
	log.Info("Creating new L1 SAE VM")
	return adaptor.Convert(&VM{
		now: time.Now,
	}), nil
}

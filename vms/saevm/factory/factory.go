// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package factory

import (
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/saevm/evm"
	"github.com/ava-labs/strevm/adaptor"
)

var _ vms.Factory = (*Factory)(nil)

type Factory struct{}

func (*Factory) New(logging.Logger) (interface{}, error) {
	return adaptor.Convert(&evm.VM{}), nil
}

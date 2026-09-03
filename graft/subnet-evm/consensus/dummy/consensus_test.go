// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customheader"
	"github.com/ava-labs/avalanchego/graft/subnet-evm/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

func TestVerifyHeaderRejectsSAEFields(t *testing.T) {
	customtypes.Register()
	header := customtypes.WithHeaderExtra(
		&types.Header{},
		&customtypes.HeaderExtra{TargetExcess: utils.PointerTo(gas.Gas(1))},
	)

	err := (&DummyEngine{}).verifyHeader(nil, header, nil, false)
	require.ErrorIs(t, err, customheader.ErrSAEHeaderFieldsUnsupported, "DummyEngine.verifyHeader()")
}

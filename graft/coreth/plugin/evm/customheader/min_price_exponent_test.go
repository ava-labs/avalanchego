// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/vms/saevm/cchain/dynamic"
)

func TestVerifyMinPriceExponent(t *testing.T) {
	require.NoError(t, VerifyMinPriceExponent(&types.Header{Time: 1001}))

	withExponent := customtypes.WithHeaderExtra(
		&types.Header{Time: 1001},
		&customtypes.HeaderExtra{MinPriceExponent: utils.PointerTo(dynamic.PriceExponent(1000))},
	)
	require.ErrorIs(t, VerifyMinPriceExponent(withExponent), errRemoteMinPriceExponentSet)
}

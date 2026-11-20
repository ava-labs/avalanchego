// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warp

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/payload"

	avalanchewarp "github.com/ava-labs/avalanchego/vms/platformvm/warp"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m, goleak.IgnoreCurrent())
}

func newHash(tb testing.TB) (*avalanchewarp.UnsignedMessage, *payload.Hash) {
	p, err := payload.NewHash(
		ids.GenerateTestID(),
	)
	require.NoError(tb, err, "payload.NewHash()")

	m, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, p.Bytes())
	require.NoError(tb, err, "warp.NewUnsignedMessage()")
	return m, p
}

func newAddressedCall(tb testing.TB) (*avalanchewarp.UnsignedMessage, *payload.AddressedCall) {
	p, err := payload.NewAddressedCall(
		utils.RandomBytes(20),
		[]byte("test"),
	)
	require.NoError(tb, err, "payload.NewAddressedCall()")

	m, err := avalanchewarp.NewUnsignedMessage(constants.UnitTestID, snowtest.XChainID, p.Bytes())
	require.NoError(tb, err, "warp.NewUnsignedMessage()")
	return m, p
}

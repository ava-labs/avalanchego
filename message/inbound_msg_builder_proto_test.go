// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

func Test_newInboundBuilderWithProto(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	mb, err := newMsgBuilderProtobuf("test", prometheus.NewRegistry(), int64(constants.DefaultMaxMessageSize), 5*time.Second)
	require.NoError(err)

	builder := newInboundBuilderWithProto(mb)

	inMsg := builder.InboundAccepted(
		ids.GenerateTestID(),
		uint32(12345),
		[]ids.ID{ids.GenerateTestID()},
		ids.GenerateTestNodeID(),
	)

	t.Logf("outbound message built %q", inMsg.Op().String())
}

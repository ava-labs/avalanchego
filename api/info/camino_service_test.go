// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
)

func TestGetGenesisBytes(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockLog := logging.NewMockLogger(ctrl)
	service := Info{log: mockLog}

	mockLog.EXPECT().Debug(gomock.Any()).Times(1)

	service.GenesisBytes = []byte("some random bytes")

	reply := GetGenesisBytesReply{}
	err := service.GetGenesisBytes(nil, nil, &reply)
	require.NoError(t, err)
	require.Equal(t, GetGenesisBytesReply{GenesisBytes: service.GenesisBytes}, reply)
}

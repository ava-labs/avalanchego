// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package simplex

import (
	"testing"

	"github.com/ava-labs/avalanchego/message/messagemock"
	"github.com/ava-labs/avalanchego/snow/networking/sender/sendermock"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestComm(t *testing.T) {	
	require := require.New(t)
	ctrl := gomock.NewController(t)

	msgCreator     := messagemock.NewOutboundMsgBuilder(ctrl)
	sender         := sendermock.NewExternalSender(ctrl)

	comm, err := NewComm(&Config{
		{
			
		})

}

func TestCommBroadcast(){

}

func TestCommFailsWithoutCurrentNode() {

}
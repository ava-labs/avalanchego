// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	assert         *assert.Assertions
	expectedInData interface{}
}

func (mc *mockClient) SendRequest(
	ctx context.Context,
	method string,
	inData interface{},
	reply interface{},
	options ...rpc.Option,
) error {
	mc.assert.Equal(inData, mc.expectedInData)
	return nil
}

func TestCreateAsset(t *testing.T) {
	assert := assert.New(t)
	client := client{}
	clientHolders := []*ClientHolder{
		{
			Amount:  11,
			Address: ids.GenerateTestShortID(),
		},
	}
	clientMinters := []ClientOwners{
		{
			Threshold: 22,
			Minters:   []ids.ShortID{ids.GenerateTestShortID()},
		},
	}
	clientFrom := []ids.ShortID{ids.GenerateTestShortID()}
	clientChangeAddr := ids.GenerateTestShortID()
	serviceHolders := []*Holder{
		{
			Amount:  json.Uint64(clientHolders[0].Amount),
			Address: clientHolders[0].Address.String(),
		},
	}
	serviceMinters := []Owners{
		{
			Threshold: json.Uint32(clientMinters[0].Threshold),
			Minters:   []string{clientMinters[0].Minters[0].String()},
		},
	}
	serviceFrom := []string{clientFrom[0].String()}
	serviceChangeAddr := clientChangeAddr.String()
	expectedInData := &CreateAssetArgs{
		JSONSpendHeader: api.JSONSpendHeader{
			JSONFromAddrs:  api.JSONFromAddrs{From: serviceFrom},
			JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: serviceChangeAddr},
		},
		InitialHolders: serviceHolders,
		MinterSets:     serviceMinters,
	}
	client.requester = &mockClient{
		assert:         assert,
		expectedInData: expectedInData,
	}
	client.CreateAsset(
		context.Background(),
		api.UserPass{},
		clientFrom,
		clientChangeAddr,
		"",
		"",
		0,
		clientHolders,
		clientMinters,
	)
}

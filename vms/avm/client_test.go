// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package avm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

type mockClient struct {
	require        *require.Assertions
	expectedInData interface{}
}

func (mc *mockClient) SendRequest(
	_ context.Context,
	_ string,
	inData interface{},
	_ interface{},
	_ ...rpc.Option,
) error {
	mc.require.Equal(mc.expectedInData, inData)
	return nil
}

func TestClientCreateAsset(t *testing.T) {
	require := require.New(t)
	client := client{}
	{
		// empty slices
		clientHolders := []*ClientHolder{}
		clientMinters := []ClientOwners{}
		clientFrom := []ids.ShortID{}
		clientChangeAddr := ids.GenerateTestShortID()
		serviceHolders := []*Holder{}
		serviceMinters := []Owners{}
		serviceFrom := []string{}
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
			require:        require,
			expectedInData: expectedInData,
		}
		_, err := client.CreateAsset(
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
		require.NoError(err)
	}
	{
		// non empty slices
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
			require:        require,
			expectedInData: expectedInData,
		}
		_, err := client.CreateAsset(
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
		require.NoError(err)
	}
}

func TestClientCreateFixedCapAsset(t *testing.T) {
	require := require.New(t)
	client := client{}
	{
		// empty slices
		clientHolders := []*ClientHolder{}
		clientFrom := []ids.ShortID{}
		clientChangeAddr := ids.GenerateTestShortID()
		serviceHolders := []*Holder{}
		serviceFrom := []string{}
		serviceChangeAddr := clientChangeAddr.String()
		expectedInData := &CreateAssetArgs{
			JSONSpendHeader: api.JSONSpendHeader{
				JSONFromAddrs:  api.JSONFromAddrs{From: serviceFrom},
				JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: serviceChangeAddr},
			},
			InitialHolders: serviceHolders,
		}
		client.requester = &mockClient{
			require:        require,
			expectedInData: expectedInData,
		}
		_, err := client.CreateFixedCapAsset(
			context.Background(),
			api.UserPass{},
			clientFrom,
			clientChangeAddr,
			"",
			"",
			0,
			clientHolders,
		)
		require.NoError(err)
	}
	{
		// non empty slices
		clientHolders := []*ClientHolder{
			{
				Amount:  11,
				Address: ids.GenerateTestShortID(),
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
		serviceFrom := []string{clientFrom[0].String()}
		serviceChangeAddr := clientChangeAddr.String()
		expectedInData := &CreateAssetArgs{
			JSONSpendHeader: api.JSONSpendHeader{
				JSONFromAddrs:  api.JSONFromAddrs{From: serviceFrom},
				JSONChangeAddr: api.JSONChangeAddr{ChangeAddr: serviceChangeAddr},
			},
			InitialHolders: serviceHolders,
		}
		client.requester = &mockClient{
			require:        require,
			expectedInData: expectedInData,
		}
		_, err := client.CreateFixedCapAsset(
			context.Background(),
			api.UserPass{},
			clientFrom,
			clientChangeAddr,
			"",
			"",
			0,
			clientHolders,
		)
		require.NoError(err)
	}
}

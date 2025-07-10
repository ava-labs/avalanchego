// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/registry/registrymock"
	"github.com/ava-labs/avalanchego/vms/vmsmock"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

type loadVMsTest struct {
	admin          *Admin
	mockVMManager  *vmsmock.Manager
	mockVMRegistry *registrymock.VMRegistry
}

func initLoadVMsTest(t *testing.T) *loadVMsTest {
	ctrl := gomock.NewController(t)

	mockVMRegistry := registrymock.NewVMRegistry(ctrl)
	mockVMManager := vmsmock.NewManager(ctrl)

	return &loadVMsTest{
		admin: &Admin{Config: Config{
			Log:        logging.NoLog{},
			VMRegistry: mockVMRegistry,
			VMManager:  mockVMManager,
		}},
		mockVMManager:  mockVMManager,
		mockVMRegistry: mockVMRegistry,
	}
}

// Tests behavior for LoadVMs if everything succeeds.
func TestLoadVMsSuccess(t *testing.T) {
	require := require.New(t)

	resources := initLoadVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	newVMs := []ids.ID{id1, id2}
	failedVMs := map[ids.ID]error{
		ids.GenerateTestID(): errTest,
	}
	// every vm is at least aliased to itself.
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}
	alias2 := []string{id2.String(), "vm2-alias-1", "vm2-alias-2"}
	// we expect that we dedup the redundant alias of vmId.
	expectedVMRegistry := map[ids.ID][]string{
		id1: alias1[1:],
		id2: alias2[1:],
	}

	resources.mockVMRegistry.EXPECT().Reload(gomock.Any()).Times(1).Return(newVMs, failedVMs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(alias2, nil)

	// execute test
	reply := LoadVMsReply{}
	require.NoError(resources.admin.LoadVMs(&http.Request{}, nil, &reply))
	require.Equal(expectedVMRegistry, reply.NewVMs)
}

// Tests behavior for LoadVMs if we fail to reload vms.
func TestLoadVMsReloadFails(t *testing.T) {
	require := require.New(t)

	resources := initLoadVMsTest(t)

	// Reload fails
	resources.mockVMRegistry.EXPECT().Reload(gomock.Any()).Times(1).Return(nil, nil, errTest)

	reply := LoadVMsReply{}
	err := resources.admin.LoadVMs(&http.Request{}, nil, &reply)
	require.ErrorIs(err, errTest)
}

// Tests behavior for LoadVMs if we fail to fetch our aliases
func TestLoadVMsGetAliasesFails(t *testing.T) {
	require := require.New(t)

	resources := initLoadVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()
	newVMs := []ids.ID{id1, id2}
	failedVMs := map[ids.ID]error{
		ids.GenerateTestID(): errTest,
	}
	// every vm is at least aliased to itself.
	alias1 := []string{id1.String(), "vm1-alias-1", "vm1-alias-2"}

	resources.mockVMRegistry.EXPECT().Reload(gomock.Any()).Times(1).Return(newVMs, failedVMs, nil)
	resources.mockVMManager.EXPECT().Aliases(id1).Times(1).Return(alias1, nil)
	resources.mockVMManager.EXPECT().Aliases(id2).Times(1).Return(nil, errTest)

	reply := LoadVMsReply{}
	err := resources.admin.LoadVMs(&http.Request{}, nil, &reply)
	require.ErrorIs(err, errTest)
}

func TestServiceDBGet(t *testing.T) {
	a := &Admin{Config: Config{
		Log: logging.NoLog{},
		DB:  memdb.New(),
	}}

	helloBytes := []byte("hello")
	helloHex, err := formatting.Encode(formatting.HexNC, helloBytes)
	require.NoError(t, err)

	worldBytes := []byte("world")
	worldHex, err := formatting.Encode(formatting.HexNC, worldBytes)
	require.NoError(t, err)

	require.NoError(t, a.DB.Put(helloBytes, worldBytes))

	tests := []struct {
		name              string
		key               string
		expectedValue     string
		expectedErrorCode rpcdbpb.Error
	}{
		{
			name:              "key exists",
			key:               helloHex,
			expectedValue:     worldHex,
			expectedErrorCode: rpcdbpb.Error_ERROR_UNSPECIFIED,
		},
		{
			name:              "key doesn't exist",
			key:               "",
			expectedValue:     "",
			expectedErrorCode: rpcdbpb.Error_ERROR_NOT_FOUND,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			reply := &DBGetReply{}
			require.NoError(a.DbGet(
				nil,
				&DBGetArgs{
					Key: test.key,
				},
				reply,
			))
			require.Equal(test.expectedValue, reply.Value)
			require.Equal(test.expectedErrorCode, reply.ErrorCode)
		})
	}
}

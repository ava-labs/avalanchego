// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/registry/registrymock"

	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
)

type loadVMsTest struct {
	admin          *Admin
	vmManager      *vms.Manager
	mockVMRegistry *registrymock.VMRegistry
}

func initLoadVMsTest(t *testing.T) *loadVMsTest {
	ctrl := gomock.NewController(t)

	mockVMRegistry := registrymock.NewVMRegistry(ctrl)
	vmManager := vms.NewManager(logging.NoLog{}, ids.NewAliaser())

	return &loadVMsTest{
		admin: &Admin{Config: Config{
			Log:        logging.NoLog{},
			VMRegistry: mockVMRegistry,
			VMManager:  vmManager,
		}},
		vmManager:      vmManager,
		mockVMRegistry: mockVMRegistry,
	}
}

// Tests behavior for LoadVMs if everything succeeds.
func TestLoadVMsSuccess(t *testing.T) {
	require := require.New(t)

	resources := initLoadVMsTest(t)

	id1 := ids.GenerateTestID()
	id2 := ids.GenerateTestID()

	// Set up aliases on the real manager
	require.NoError(resources.vmManager.Alias(id1, id1.String()))
	require.NoError(resources.vmManager.Alias(id1, "vm1-alias-1"))
	require.NoError(resources.vmManager.Alias(id1, "vm1-alias-2"))
	require.NoError(resources.vmManager.Alias(id2, id2.String()))
	require.NoError(resources.vmManager.Alias(id2, "vm2-alias-1"))
	require.NoError(resources.vmManager.Alias(id2, "vm2-alias-2"))

	newVMs := []ids.ID{id1, id2}
	failedVMs := map[ids.ID]error{
		ids.GenerateTestID(): errTest,
	}
	// we expect that we dedup the redundant alias of vmId.
	expectedVMRegistry := map[ids.ID][]string{
		id1: {"vm1-alias-1", "vm1-alias-2"},
		id2: {"vm2-alias-1", "vm2-alias-2"},
	}

	resources.mockVMRegistry.EXPECT().Reload(gomock.Any()).Times(1).Return(newVMs, failedVMs, nil)

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

// failingAliaser wraps an Aliaser and returns an error from Aliases.
type failingAliaser struct {
	ids.Aliaser

	err error
}

func (f *failingAliaser) Aliases(ids.ID) ([]string, error) {
	return nil, f.err
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

	resources.mockVMRegistry.EXPECT().Reload(gomock.Any()).Times(1).Return(newVMs, failedVMs, nil)
	// Inject a failing aliaser so Aliases returns an error
	resources.vmManager.Aliaser = &failingAliaser{
		Aliaser: resources.vmManager.Aliaser,
		err:     errTest,
	}

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

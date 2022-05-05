// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// SuccessResponseTest defines the expected result of an API call that returns SuccessResponse
type SuccessResponseTest struct {
	Err error
}

// GetSuccessResponseTests returns a list of possible SuccessResponseTests
func GetSuccessResponseTests() []SuccessResponseTest {
	return []SuccessResponseTest{
		{
			Err: nil,
		},
		{
			Err: errors.New("Non-nil error"),
		},
	}
}

type mockClient struct {
	response interface{}
	err      error
}

// NewMockClient returns a mock client for testing
func NewMockClient(response interface{}, err error) rpc.EndpointRequester {
	return &mockClient{
		response: response,
		err:      err,
	}
}

func (mc *mockClient) SendRequest(ctx context.Context, method string, params interface{}, reply interface{}, options ...rpc.Option) error {
	if mc.err != nil {
		return mc.err
	}

	switch p := reply.(type) {
	case *api.EmptyReply:
		response := mc.response.(*api.EmptyReply)
		*p = *response
	case *GetChainAliasesReply:
		response := mc.response.(*GetChainAliasesReply)
		*p = *response
	case *LoadVMsReply:
		response := mc.response.(*LoadVMsReply)
		*p = *response
	default:
		panic("illegal type")
	}
	return nil
}

func TestStartCPUProfiler(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.StartCPUProfiler(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}
}

func TestStopCPUProfiler(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.StopCPUProfiler(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}
}

func TestMemoryProfile(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.MemoryProfile(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}
	}
}

func TestLockProfile(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.LockProfile(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

	}
}

func TestAlias(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.Alias(context.Background(), "alias", "alias2")
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

	}
}

func TestAliasChain(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.AliasChain(context.Background(), "chain", "chain-alias")
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

	}
}

func TestGetChainAliases(t *testing.T) {
	t.Run("successful", func(t *testing.T) {
		expectedReply := []string{"alias1", "alias2"}
		mockClient := client{requester: NewMockClient(&GetChainAliasesReply{
			Aliases: expectedReply,
		}, nil)}

		reply, err := mockClient.GetChainAliases(context.Background(), "chain")
		assert.NoError(t, err)
		assert.ElementsMatch(t, expectedReply, reply)
	})

	t.Run("failure", func(t *testing.T) {
		mockClient := client{requester: NewMockClient(&GetChainAliasesReply{}, errors.New("some error"))}

		_, err := mockClient.GetChainAliases(context.Background(), "chain")

		assert.EqualError(t, err, "some error")
	})
}

func TestStacktrace(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(&api.EmptyReply{}, test.Err)}
		err := mockClient.Stacktrace(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexpected error: %s", err)
		}

	}
}

func TestReloadInstalledVMs(t *testing.T) {
	t.Run("successful", func(t *testing.T) {
		expectedNewVMs := map[ids.ID][]string{
			ids.GenerateTestID(): {"foo"},
			ids.GenerateTestID(): {"bar"},
		}
		expectedFailedVMs := map[ids.ID]string{
			ids.GenerateTestID(): "oops",
			ids.GenerateTestID(): "uh-oh",
		}
		mockClient := client{requester: NewMockClient(&LoadVMsReply{
			NewVMs:    expectedNewVMs,
			FailedVMs: expectedFailedVMs,
		}, nil)}

		loadedVMs, failedVMs, err := mockClient.LoadVMs(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, expectedNewVMs, loadedVMs)
		assert.Equal(t, expectedFailedVMs, failedVMs)
	})

	t.Run("failure", func(t *testing.T) {
		mockClient := client{requester: NewMockClient(&LoadVMsReply{}, errors.New("some error"))}

		_, _, err := mockClient.LoadVMs(context.Background())

		assert.EqualError(t, err, "some error")
	})
}

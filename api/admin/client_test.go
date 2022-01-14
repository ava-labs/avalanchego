// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

// SuccessResponseTest defines the expected result of an API call that returns SuccessResponse
type SuccessResponseTest struct {
	Success bool
	Err     error
}

// GetSuccessResponseTests returns a list of possible SuccessResponseTests
func GetSuccessResponseTests() []SuccessResponseTest {
	return []SuccessResponseTest{
		{
			Success: true,
			Err:     nil,
		},
		{
			Success: false,
			Err:     nil,
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

func (mc *mockClient) SendRequest(ctx context.Context, method string, params interface{}, reply interface{}) error {
	if mc.err != nil {
		return mc.err
	}

	switch p := reply.(type) {
	case *api.SuccessResponse:
		response := mc.response.(api.SuccessResponse)
		*p = response
	case *GetChainAliasesReply:
		response := mc.response.(*GetChainAliasesReply)
		*p = *response
	default:
		panic("illegal type")
	}
	return nil
}

func TestStartCPUProfiler(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.StartCPUProfiler(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
		}
	}
}

func TestStopCPUProfiler(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.StopCPUProfiler(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
		}
	}
}

func TestMemoryProfile(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.MemoryProfile(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
		}
	}
}

func TestLockProfile(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.LockProfile(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
		}
	}
}

func TestAlias(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.Alias(context.Background(), "alias", "alias2")
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
		}
	}
}

func TestAliasChain(t *testing.T) {
	tests := GetSuccessResponseTests()

	for _, test := range tests {
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.AliasChain(context.Background(), "chain", "chain-alias")
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
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
		mockClient := client{requester: NewMockClient(api.SuccessResponse{Success: test.Success}, test.Err)}
		success, err := mockClient.Stacktrace(context.Background())
		// if there is error as expected, the test passes
		if err != nil && test.Err != nil {
			continue
		}
		if err != nil {
			t.Fatalf("Unexepcted error: %s", err)
		}
		if success != test.Success {
			t.Fatalf("Expected success response to be: %v, but found: %v", test.Success, success)
		}
	}
}

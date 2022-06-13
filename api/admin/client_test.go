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
	"github.com/ava-labs/avalanchego/utils/logging"
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
	case *GetLoggerLevelReply:
		response := mc.response.(*GetLoggerLevelReply)
		*p = *response
	case *interface{}:
		response := mc.response.(*interface{})
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

func TestSetLoggerLevel(t *testing.T) {
	type test struct {
		name            string
		logLevel        string
		displayLevel    string
		serviceErr      bool
		clientShouldErr bool
	}
	tests := []test{
		{
			name:            "Happy path",
			logLevel:        "INFO",
			displayLevel:    "INFO",
			serviceErr:      false,
			clientShouldErr: false,
		},
		{
			name:            "Service errors",
			logLevel:        "INFO",
			displayLevel:    "INFO",
			serviceErr:      true,
			clientShouldErr: true,
		},
		{
			name:            "Invalid log level",
			logLevel:        "invalid",
			displayLevel:    "INFO",
			serviceErr:      false,
			clientShouldErr: true,
		},
		{
			name:            "Invalid display level",
			logLevel:        "INFO",
			displayLevel:    "invalid",
			serviceErr:      false,
			clientShouldErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			var err error
			if tt.serviceErr {
				err = errors.New("some error")
			}
			mockClient := client{requester: NewMockClient(&api.EmptyReply{}, err)}
			err = mockClient.SetLoggerLevel(
				context.Background(),
				"",
				tt.logLevel,
				tt.displayLevel,
			)
			if tt.clientShouldErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestGetLoggerLevel(t *testing.T) {
	type test struct {
		name            string
		loggerName      string
		serviceResponse map[string]LogAndDisplayLevels
		serviceErr      bool
		clientShouldErr bool
	}
	tests := []test{
		{
			name:       "Happy Path",
			loggerName: "foo",
			serviceResponse: map[string]LogAndDisplayLevels{
				"foo": {LogLevel: logging.Info, DisplayLevel: logging.Info},
			},
			serviceErr:      false,
			clientShouldErr: false,
		},
		{
			name:            "service errors",
			loggerName:      "foo",
			serviceResponse: nil,
			serviceErr:      true,
			clientShouldErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			var err error
			if tt.serviceErr {
				err = errors.New("some error")
			}
			mockClient := client{requester: NewMockClient(&GetLoggerLevelReply{LoggerLevels: tt.serviceResponse}, err)}
			res, err := mockClient.GetLoggerLevel(
				context.Background(),
				tt.loggerName,
			)
			if tt.clientShouldErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			assert.EqualValues(tt.serviceResponse, res)
		})
	}
}

func TestGetConfig(t *testing.T) {
	type test struct {
		name             string
		serviceErr       bool
		clientShouldErr  bool
		expectedResponse interface{}
	}
	var resp interface{} = "response"
	tests := []test{
		{
			name:             "Happy path",
			serviceErr:       false,
			clientShouldErr:  false,
			expectedResponse: &resp,
		},
		{
			name:             "service errors",
			serviceErr:       true,
			clientShouldErr:  true,
			expectedResponse: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert := assert.New(t)
			var err error
			if tt.serviceErr {
				err = errors.New("some error")
			}
			mockClient := client{requester: NewMockClient(tt.expectedResponse, err)}
			res, err := mockClient.GetConfig(context.Background())
			if tt.clientShouldErr {
				assert.Error(err)
				return
			}
			assert.NoError(err)
			assert.EqualValues("response", res)
		})
	}
}

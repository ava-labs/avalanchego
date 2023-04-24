// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/rpc"
)

var errTest = errors.New("non-nil error")

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
			Err: errTest,
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

func (mc *mockClient) SendRequest(_ context.Context, _ string, _ interface{}, reply interface{}, _ ...rpc.Option) error {
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
		require.NoError(t, err)
		require.ElementsMatch(t, expectedReply, reply)
	})

	t.Run("failure", func(t *testing.T) {
		mockClient := client{requester: NewMockClient(&GetChainAliasesReply{}, errTest)}

		_, err := mockClient.GetChainAliases(context.Background(), "chain")

		require.ErrorIs(t, err, errTest)
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
		require.NoError(t, err)
		require.Equal(t, expectedNewVMs, loadedVMs)
		require.Equal(t, expectedFailedVMs, failedVMs)
	})

	t.Run("failure", func(t *testing.T) {
		mockClient := client{requester: NewMockClient(&LoadVMsReply{}, errTest)}

		_, _, err := mockClient.LoadVMs(context.Background())

		require.ErrorIs(t, err, errTest)
	})
}

func TestSetLoggerLevel(t *testing.T) {
	type test struct {
		name         string
		logLevel     string
		displayLevel string
		serviceErr   error
		clientErr    error
	}
	tests := []test{
		{
			name:         "Happy path",
			logLevel:     "INFO",
			displayLevel: "INFO",
			serviceErr:   nil,
			clientErr:    nil,
		},
		{
			name:         "Service errors",
			logLevel:     "INFO",
			displayLevel: "INFO",
			serviceErr:   errTest,
			clientErr:    errTest,
		},
		{
			name:         "Invalid log level",
			logLevel:     "invalid",
			displayLevel: "INFO",
			serviceErr:   nil,
			clientErr:    logging.ErrUnknownLevel,
		},
		{
			name:         "Invalid display level",
			logLevel:     "INFO",
			displayLevel: "invalid",
			serviceErr:   nil,
			clientErr:    logging.ErrUnknownLevel,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			c := client{
				requester: NewMockClient(&api.EmptyReply{}, tt.serviceErr),
			}
			err := c.SetLoggerLevel(
				context.Background(),
				"",
				tt.logLevel,
				tt.displayLevel,
			)
			require.ErrorIs(err, tt.clientErr)
		})
	}
}

func TestGetLoggerLevel(t *testing.T) {
	type test struct {
		name            string
		loggerName      string
		serviceResponse map[string]LogAndDisplayLevels
		serviceErr      error
		clientErr       error
	}
	tests := []test{
		{
			name:       "Happy Path",
			loggerName: "foo",
			serviceResponse: map[string]LogAndDisplayLevels{
				"foo": {LogLevel: logging.Info, DisplayLevel: logging.Info},
			},
			serviceErr: nil,
			clientErr:  nil,
		},
		{
			name:            "service errors",
			loggerName:      "foo",
			serviceResponse: nil,
			serviceErr:      errTest,
			clientErr:       errTest,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			c := client{
				requester: NewMockClient(
					&GetLoggerLevelReply{
						LoggerLevels: tt.serviceResponse,
					},
					tt.serviceErr,
				),
			}
			res, err := c.GetLoggerLevel(
				context.Background(),
				tt.loggerName,
			)
			require.ErrorIs(err, tt.clientErr)
			if tt.clientErr == nil {
				require.EqualValues(tt.serviceResponse, res)
			}
		})
	}
}

func TestGetConfig(t *testing.T) {
	type test struct {
		name             string
		serviceErr       error
		clientErr        error
		expectedResponse interface{}
	}
	var resp interface{} = "response"
	tests := []test{
		{
			name:             "Happy path",
			serviceErr:       nil,
			clientErr:        nil,
			expectedResponse: &resp,
		},
		{
			name:             "service errors",
			serviceErr:       errTest,
			clientErr:        errTest,
			expectedResponse: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			c := client{
				requester: NewMockClient(tt.expectedResponse, tt.serviceErr),
			}
			res, err := c.GetConfig(context.Background())
			require.ErrorIs(err, tt.clientErr)
			if tt.clientErr == nil {
				require.Equal(resp, res)
			}
		})
	}
}

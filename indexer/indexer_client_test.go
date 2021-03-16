package indexer

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

type TestClientOutput struct {
	Out1 string
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

func (mc *mockClient) SendRequest(method string, params interface{}, reply interface{}) error {
	if mc.err != nil {
		return mc.err
	}

	switch p := reply.(type) {
	case *interface{}:
		response := mc.response.(*TestClientOutput)
		*p = response
	default:
		panic(fmt.Sprintf("illegal type %s", reflect.TypeOf(reply)))
	}
	return nil
}

func TestClientSend(t *testing.T) {
	cl := NewClient("http://localhost:1234", IndexTypeTransactions, 1*time.Minute)

	testingArgumentValue := fmt.Sprintf("%v", time.Now().String())

	resp := &TestClientOutput{Out1: testingArgumentValue}

	mock := NewMockClient(resp, nil)
	cl.requester = mock

	type Args struct {
		Arg1 string
	}

	args := &Args{Arg1: "arg1"}
	var output *TestClientOutput
	var outputif interface{} = &output

	err := cl.send("testMethod", args, &outputif)
	if err != nil {
		t.Fatal("invalid send")
	}

	if outputif.(*TestClientOutput).Out1 != testingArgumentValue {
		t.Fatal("output.Out1 invalid")
	}
}

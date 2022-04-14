// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	j "encoding/json"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	gorillarpc "github.com/gorilla/rpc/v2"

	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"

	"github.com/stretchr/testify/assert"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	cjson "github.com/ava-labs/avalanchego/utils/json"
)

// Test_VMCreateHandlers tests the Handle and HandleSimple RPCs by creating a plugin and
// serving the handlers exposed by the subnet. The test then will exercise the service
// as a regression test.
func Test_VMCreateHandlers(t *testing.T) {
	assert := assert.New(t)
	pr := &pingRequest{
		Version: "2.0",
		Method:  "subnet.ping",
		Params:  []string{},
		ID:      "1",
	}
	pingBody, err := j.Marshal(pr)
	assert.NoError(err)

	scenarios := []struct {
		name    string
		payload []byte
	}{
		{
			name:    "test HTTP gRPC service",
			payload: pingBody,
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			process := helperProcess("vm")
			c := plugin.NewClient(&plugin.ClientConfig{
				Cmd:              process,
				HandshakeConfig:  TestHandshake,
				Plugins:          TestPluginMap,
				AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
			})
			defer c.Kill()

			_, err := c.Start()
			assert.NoErrorf(err, "failed to start plugin: %v", err)

			if v := c.Protocol(); v != plugin.ProtocolGRPC {
				assert.NoErrorf(err, "invalid protocol %q: :%v", c.Protocol(), err)
			}

			// Get the plugin client.
			client, err := c.Client()
			assert.NoErrorf(err, "failed to get plugin client: %v", err)

			// Grab the vm implementation.
			raw, err := client.Dispense("vm")
			assert.NoErrorf(err, "failed to dispense plugin: %v", err)

			// Get vm client.
			vm, ok := raw.(*TestVMClient)
			if !ok {
				assert.NoError(err)
			}

			// Get the handlers exposed by the subnet vm.
			handlers, err := vm.CreateHandlers()
			assert.NoErrorf(err, "failed to get handlers: %v", err)

			r := mux.NewRouter()
			for ep, handler := range handlers {
				r.Handle(ep, handler.Handler)
			}
			listener, err := net.Listen("tcp", "localhost:0")
			assert.NoErrorf(err, "failed to create listener: %v", err)

			go func() {
				err := http.Serve(listener, r)
				assert.NoErrorf(err, "failed to serve HTTP: %v", err)
			}()

			target := listener.Addr().String()

			for endpoint := range handlers {
				switch endpoint {
				case "/rpc":
					err := testHTTPPingRequest(target, endpoint, scenario.payload)
					assert.NoErrorf(err, "%s rpc ping failed: %v", endpoint, err)

				case "/ws":
					// expected number of msg echos to receive from websocket server.
					// This test is sanity for conn hijack and server push.
					expectedMsgCount := 5
					err := testWebsocketEchoRequest(target, endpoint, expectedMsgCount, scenario.payload)
					assert.NoErrorf(err, "%s websocket echo failed: %v", endpoint, err)
				default:
					t.Fatal("unknown handler")
				}
			}
		})
	}
}

func testHTTPPingRequest(target, endpoint string, payload []byte) error {
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://%s%s", target, endpoint), bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	httpClient := new(http.Client)
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to dial test server: %v", err)
	}
	defer resp.Body.Close()

	pb, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var ping testResult
	err = j.Unmarshal(pb, &ping)
	if err != nil {
		return err
	}

	if !ping.Result.Success {
		return fmt.Errorf("want ping success: true: got %v", ping.Result.Success)
	}
	return nil
}

func testWebsocketEchoRequest(target, endpoint string, expectedMsgCount int, payload []byte) error {
	dialTarget := fmt.Sprintf("ws://%s%s", target, endpoint)
	cli, _, err := websocket.DefaultDialer.Dial(dialTarget, nil) //nolint
	if err != nil {
		return err
	}
	defer cli.Close()

	err = cli.WriteMessage(websocket.TextMessage, payload)
	if err != nil {
		return err
	}

	i := 0
	for i < expectedMsgCount {
		i++
		// TODO: verify message body
		_, _, err := cli.ReadMessage()
		if err != nil {
			return err
		}
	}

	// TODO more robust test...
	if i != expectedMsgCount {
		return fmt.Errorf("want (%d) messages got (%d)", expectedMsgCount, i)
	}
	return nil
}

type TestVM interface {
	CreateHandlers() (map[string]*common.HTTPHandler, error)
}

func NewTestServer(vm TestVM) *TestVMServer {
	return &TestVMServer{
		vm: vm,
	}
}

type TestVMServer struct {
	vmpb.UnimplementedVMServer
	vm TestVM

	serverCloser grpcutils.ServerCloser
}

func (vm *TestVMServer) CreateHandlers(context.Context, *emptypb.Empty) (*vmpb.CreateHandlersResponse, error) {
	handlers, err := vm.vm.CreateHandlers()
	if err != nil {
		return nil, err
	}

	resp := &vmpb.CreateHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		// start the http server
		serverListener, err := grpcutils.NewListener()
		if err != nil {
			return nil, err
		}
		serverAddr := serverListener.Addr().String()

		go grpcutils.Serve(serverListener, func(opts []grpc.ServerOption) *grpc.Server {
			if len(opts) == 0 {
				opts = append(opts, grpcutils.DefaultServerOptions...)
			}
			server := grpc.NewServer(opts...)
			vm.serverCloser.Add(server)
			httppb.RegisterHTTPServer(server, ghttp.NewServer(handler.Handler))
			return server
		})

		resp.Handlers = append(resp.Handlers, &vmpb.Handler{
			Prefix:      prefix,
			LockOptions: uint32(handler.LockOptions),
			ServerAddr:  serverAddr,
		})
	}
	return resp, nil
}

type TestVMClient struct {
	client vmpb.VMClient
	conns  []*grpc.ClientConn
}

func NewTestClient(client vmpb.VMClient) *TestVMClient {
	return &TestVMClient{
		client: client,
	}
}

func (vm *TestVMClient) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	resp, err := vm.client.CreateHandlers(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	handlers := make(map[string]*common.HTTPHandler, len(resp.Handlers))
	for _, handler := range resp.Handlers {
		clientConn, err := grpcutils.Dial(handler.ServerAddr)
		if err != nil {
			return nil, err
		}

		vm.conns = append(vm.conns, clientConn)
		handlers[handler.Prefix] = &common.HTTPHandler{
			LockOptions: common.LockOption(handler.LockOptions),
			Handler:     ghttp.NewClient(httppb.NewHTTPClient(clientConn)),
		}
	}
	return handlers, nil
}

type TestSubnetVM struct {
	logger hclog.Logger
}

func (vm *TestSubnetVM) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	apis := make(map[string]*common.HTTPHandler)

	testEchoMsgCount := 5
	apis["/ws"] = &common.HTTPHandler{
		LockOptions: common.NoLock, Handler: websocketEchoHandler(testEchoMsgCount),
	}
	rpcServer, err := getTestRPCServer()
	if err != nil {
		return nil, err
	}

	apis["/rpc"] = &common.HTTPHandler{
		LockOptions: common.NoLock, Handler: rpcServer,
	}
	return apis, nil
}

type PingService struct{}

type PingReply struct {
	Success bool `json:"success"`
}

type pingRequest struct {
	Version string   `json:"jsonrpc"`
	Method  string   `json:"method"`
	Params  []string `json:"params"`
	ID      string   `json:"id"`
}

type testResult struct {
	Result PingReply `json:"result"`
}

func (p *PingService) Ping(_ *http.Request, _ *struct{}, reply *PingReply) (err error) {
	reply.Success = true
	return nil
}

func getTestRPCServer() (*gorillarpc.Server, error) {
	server := gorillarpc.NewServer()
	server.RegisterCodec(cjson.NewCodec(), "application/json")
	server.RegisterCodec(cjson.NewCodec(), "application/json;charset=UTF-8")
	if err := server.RegisterService(&PingService{}, "subnet"); err != nil {
		return nil, fmt.Errorf("failed to create rpc server %v", err)
	}
	return server, nil
}

// websocketEchoHandler upgrades the request and sends back N(msgCount)
// echos.
func websocketEchoHandler(msgCount int) http.Handler {
	upgrader := websocket.Upgrader{} // use default options

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()

		mt, b, err := c.ReadMessage()
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
		for i := 0; i < msgCount; i++ {
			err = c.WriteMessage(mt, b)
			if err != nil {
				return
			}
		}
	})
}

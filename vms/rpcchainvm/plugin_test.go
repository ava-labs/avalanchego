// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package rpcchainvm

import (
	"bytes"
	"context"
	j "encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"

	"github.com/gorilla/mux"
	gorillarpc "github.com/gorilla/rpc/v2"
	"github.com/gorilla/websocket"
	hclog "github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/api/proto/ghttpproto"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	testvmproto "github.com/ava-labs/avalanchego/vms/rpcchainvm/tests/api/proto/gatewaytest/vm/v1alpha"
)

var (
	TestHandshake = plugin.HandshakeConfig{
		ProtocolVersion:  1,
		MagicCookieKey:   "VM_PLUGIN",
		MagicCookieValue: "dynamic",
	}

	TestPluginMap = map[string]plugin.Plugin{
		"vm": &testVMPlugin{},
	}

	_ plugin.Plugin     = &testVMPlugin{}
	_ plugin.GRPCPlugin = &testVMPlugin{}
)

// TestSubnet_HTTPGRPC tests the HTTP and HTTPSimple services by creating a plugin and
// serving the handlers exposed by the subnet. The test then will exercise the service
// to ensure correctness.
func TestSubnet_HTTPGRPC(t *testing.T) {
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
		name           string
		subnetHandlers map[string]*common.HTTPHandler
		payload        []byte
		wantResponse   string
	}{
		{
			name:    "rpc server /rpc endpoint",
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
					assert.NoErrorf(err, "%s test failed: %v", endpoint, err)

				case "/ws":
					err := testWebsocketPingRequest(target, endpoint, scenario.payload)
					assert.NoErrorf(err, "%s test failed: %v", endpoint, err)
				default:
					t.Fatal("unknown endpoint")
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

func testWebsocketPingRequest(target, endpoint string, payload []byte) error {
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
	for i < 4 {
		i++
		// TODO: verify message body
		_, _, err := cli.ReadMessage()
		if err != nil {
			return err
		}
	}

	// TODO this test is too basic...
	expectedMsgCount := 4
	if i != expectedMsgCount {
		return fmt.Errorf("want (%d) messages got (%d)", expectedMsgCount, i)
	}
	return nil
}

// TODO: move below into separate file
func helperProcess(s ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--"}
	cs = append(cs, s...)
	env := []string{
		"GO_WANT_HELPER_PROCESS=1",
	}
	run := os.Args[0]
	cmd := exec.Command(run, cs...)
	env = append(env, os.Environ()...)
	cmd.Env = env
	return cmd
}

// This is not a real test. This is just a helper process kicked off by
// tests.
func TestHelperProcess(*testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for len(args) > 0 {
		if args[0] == "--" {
			args = args[1:]
			break
		}

		args = args[1:]
	}

	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "No command\n")
		os.Exit(2)
	}

	pluginLogger := hclog.New(&hclog.LoggerOptions{
		Level:      hclog.Trace,
		Output:     os.Stderr,
		JSONFormat: true,
	})

	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: TestHandshake,
		Plugins: map[string]plugin.Plugin{
			"vm": NewTestVM(&TestSubnetVM{logger: pluginLogger}),
		},

		// A non-nil value here enables gRPC serving for this plugin...
		GRPCServer: plugin.DefaultGRPCServer,
	})
	os.Exit(0)
}

type testVMPlugin struct {
	plugin.NetRPCUnsupportedPlugin
	vm TestVM
}

func NewTestVM(vm TestVM) plugin.Plugin {
	return &testVMPlugin{vm: vm}
}

func (p *testVMPlugin) GRPCServer(broker *plugin.GRPCBroker, s *grpc.Server) error {
	testvmproto.RegisterTestVMServer(s, NewTestServer(p.vm, broker))
	return nil
}

func (p *testVMPlugin) GRPCClient(ctx context.Context, broker *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return NewTestClient(testvmproto.NewTestVMClient(c), broker), nil
}

var _ testvmproto.TestVMServer = &TestVMServer{}

type TestVM interface {
	CreateHandlers() (map[string]*common.HTTPHandler, error)
}

func NewTestServer(vm TestVM, broker *plugin.GRPCBroker) *TestVMServer {
	return &TestVMServer{
		vm:     vm,
		broker: broker,
	}
}

type TestVMServer struct {
	testvmproto.UnimplementedTestVMServer
	vm     TestVM
	broker *plugin.GRPCBroker

	serverCloser grpcutils.ServerCloser
}

func (vm *TestVMServer) CreateHandlers(context.Context, *emptypb.Empty) (*testvmproto.CreateHandlersResponse, error) {
	handlers, err := vm.vm.CreateHandlers()
	if err != nil {
		return nil, err
	}

	resp := &testvmproto.CreateHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		serverID := vm.broker.NextId()
		go vm.broker.AcceptAndServe(serverID, func(opts []grpc.ServerOption) *grpc.Server {
			opts = append(opts, serverOptions...)
			server := grpc.NewServer(opts...)
			vm.serverCloser.Add(server)
			ghttpproto.RegisterHTTPServer(server, ghttp.NewServer(handler.Handler, vm.broker))
			return server
		})

		resp.Handlers = append(resp.Handlers, &testvmproto.Handler{
			Prefix:      prefix,
			LockOptions: uint32(handler.LockOptions),
			Server:      serverID,
		})
	}
	return resp, nil
}

type TestVMClient struct {
	client testvmproto.TestVMClient
	broker *plugin.GRPCBroker

	conns []*grpc.ClientConn
}

func NewTestClient(client testvmproto.TestVMClient, broker *plugin.GRPCBroker) *TestVMClient {
	return &TestVMClient{
		client: client,
		broker: broker,
	}
}

func (vm *TestVMClient) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	resp, err := vm.client.CreateHandlers(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	handlers := make(map[string]*common.HTTPHandler, len(resp.Handlers))
	for _, handler := range resp.Handlers {
		conn, err := vm.broker.Dial(handler.Server)
		if err != nil {
			return nil, err
		}

		vm.conns = append(vm.conns, conn)
		handlers[handler.Prefix] = &common.HTTPHandler{
			LockOptions: common.LockOption(handler.LockOptions),
			Handler:     ghttp.NewClient(ghttpproto.NewHTTPClient(conn), vm.broker),
		}
	}
	return handlers, nil
}

type TestSubnetVM struct {
	// vm     TestVM
	logger hclog.Logger
}

func (vm TestSubnetVM) CreateHandlers() (map[string]*common.HTTPHandler, error) {
	apis := make(map[string]*common.HTTPHandler)

	apis["/ws"] = &common.HTTPHandler{
		LockOptions: common.NoLock, Handler: websocketHandlerWithDuration(),
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

func websocketHandlerWithDuration() http.Handler {
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
		for i := 0; i < 5; i++ {
			err = c.WriteMessage(mt, b)
			if err != nil {
				return
			}
		}
	})
}

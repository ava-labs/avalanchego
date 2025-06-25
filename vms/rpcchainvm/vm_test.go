// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blockmock"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block/blocktest"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime/subprocess"

	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

const (
	chainVMTestKey                                 = "chainVMTest"
	stateSyncEnabledTestKey                        = "stateSyncEnabledTest"
	getOngoingSyncStateSummaryTestKey              = "getOngoingSyncStateSummaryTest"
	getLastStateSummaryTestKey                     = "getLastStateSummaryTest"
	parseStateSummaryTestKey                       = "parseStateSummaryTest"
	getStateSummaryTestKey                         = "getStateSummaryTest"
	acceptStateSummaryTestKey                      = "acceptStateSummaryTest"
	lastAcceptedBlockPostStateSummaryAcceptTestKey = "lastAcceptedBlockPostStateSummaryAcceptTest"
	contextTestKey                                 = "contextTest"
	batchedParseBlockCachingTestKey                = "batchedParseBlockCachingTest"
)

var TestServerPluginMap = map[string]func(*testing.T, bool) block.ChainVM{
	stateSyncEnabledTestKey:                        stateSyncEnabledTestPlugin,
	getOngoingSyncStateSummaryTestKey:              getOngoingSyncStateSummaryTestPlugin,
	getLastStateSummaryTestKey:                     getLastStateSummaryTestPlugin,
	parseStateSummaryTestKey:                       parseStateSummaryTestPlugin,
	getStateSummaryTestKey:                         getStateSummaryTestPlugin,
	acceptStateSummaryTestKey:                      acceptStateSummaryTestPlugin,
	lastAcceptedBlockPostStateSummaryAcceptTestKey: lastAcceptedBlockPostStateSummaryAcceptTestPlugin,
	contextTestKey:                                 contextEnabledTestPlugin,
	batchedParseBlockCachingTestKey:                batchedParseBlockCachingTestPlugin,
}

// helperProcess helps with creating the subnet binary for testing.
func helperProcess(s ...string) *exec.Cmd {
	cs := []string{"-test.run=TestHelperProcess", "--"}
	cs = append(cs, s...)
	env := []string{
		"TEST_PROCESS=1",
	}
	run := os.Args[0]
	cmd := exec.Command(run, cs...)
	env = append(env, os.Environ()...)
	cmd.Env = env
	return cmd
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("TEST_PROCESS") != "1" {
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
		fmt.Fprintln(os.Stderr, "failed to receive testKey")
		os.Exit(2)
	}

	testKey := args[0]
	if testKey == "dummy" {
		// block till killed
		select {}
	}

	mockedVM := TestServerPluginMap[testKey](t, true /*loadExpectations*/)
	err := Serve(context.Background(), mockedVM)
	if err != nil {
		os.Exit(1)
	}

	os.Exit(0)
}

// TestVMServerInterface ensures that the RPCs methods defined by VMServer
// interface are implemented.
func TestVMServerInterface(t *testing.T) {
	var wantMethods, gotMethods []string
	pb := reflect.TypeOf((*vmpb.VMServer)(nil)).Elem()
	for i := 0; i < pb.NumMethod()-1; i++ {
		wantMethods = append(wantMethods, pb.Method(i).Name)
	}
	slices.Sort(wantMethods)

	impl := reflect.TypeOf(&VMServer{})
	for i := 0; i < impl.NumMethod(); i++ {
		gotMethods = append(gotMethods, impl.Method(i).Name)
	}
	slices.Sort(gotMethods)

	require.Equal(t, wantMethods, gotMethods)
}

func TestRuntimeSubprocessBootstrap(t *testing.T) {
	tests := []struct {
		name      string
		config    *subprocess.Config
		assertErr func(require *require.Assertions, err error)
		// if false vm initialize bootstrap will fail
		serveVM bool
	}{
		{
			name: "happy path",
			config: &subprocess.Config{
				Stderr:           logging.NoLog{},
				Stdout:           logging.NoLog{},
				Log:              logging.NoLog{},
				HandshakeTimeout: runtime.DefaultHandshakeTimeout,
			},
			assertErr: func(require *require.Assertions, err error) {
				require.NoError(err)
			},
			serveVM: true,
		},
		{
			name: "invalid stderr",
			config: &subprocess.Config{
				Stdout:           logging.NoLog{},
				Log:              logging.NoLog{},
				HandshakeTimeout: runtime.DefaultHandshakeTimeout,
			},
			assertErr: func(require *require.Assertions, err error) {
				require.ErrorIs(err, runtime.ErrInvalidConfig)
			},
			serveVM: true,
		},
		{
			name: "handshake timeout",
			config: &subprocess.Config{
				Stderr:           logging.NoLog{},
				Stdout:           logging.NoLog{},
				Log:              logging.NoLog{},
				HandshakeTimeout: time.Microsecond,
			},
			assertErr: func(require *require.Assertions, err error) {
				require.ErrorIs(err, runtime.ErrHandshakeFailed)
			},
			serveVM: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			ctrl := gomock.NewController(t)
			vm := blockmock.NewChainVM(ctrl)

			listener, err := grpcutils.NewListener()
			require.NoError(err)

			require.NoError(os.Setenv(runtime.EngineAddressKey, listener.Addr().String()))

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if test.serveVM {
				go func() {
					_ = Serve(ctx, vm)
				}()
			}

			status, stopper, err := subprocess.Bootstrap(
				context.Background(),
				listener,
				helperProcess("dummy"),
				test.config,
			)
			if err == nil {
				require.NotEmpty(status.Addr)
				stopper.Stop(ctx)
			}
			test.assertErr(require, err)
		})
	}
}

func TestNewHTTPHandler(t *testing.T) {
	require := require.New(t)

	grpcServer := grpc.NewServer()
	listener := bufconn.Listen(1024)

	serverVM := &blocktest.VM{
		VM: enginetest.VM{
			NewHTTPHandlerF: func(context.Context) (http.Handler, error) {
				return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusOK)
				}), nil
			},
		},
	}

	server := NewServer(serverVM, utils.NewAtomic[bool](false))
	vmpb.RegisterVMServer(grpcServer, server)

	go func() {
		_ = grpcServer.Serve(listener)
	}()

	cc, err := grpc.DialContext(context.Background(), "bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithInsecure(),
	)
	require.NoError(err)

	client := NewClient(
		cc,
		runtime.NewManager(),
		123,
		nil,
		metrics.NewLabelGatherer(""),
		logging.NoLog{},
	)

	handler, err := client.NewHTTPHandler(context.Background())
	require.NoError(err)

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(http.StatusOK, w.Code)
}

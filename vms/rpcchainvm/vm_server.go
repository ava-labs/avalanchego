// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"sync"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/gecko/database/rpcdb"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/engine/snowman"
	"github.com/ava-labs/gecko/utils/wrappers"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/gecko/vms/rpcchainvm/messenger"

	dbproto "github.com/ava-labs/gecko/database/rpcdb/proto"
	httpproto "github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/proto"
	msgproto "github.com/ava-labs/gecko/vms/rpcchainvm/messenger/proto"
	vmproto "github.com/ava-labs/gecko/vms/rpcchainvm/proto"
)

// VMServer is a VM that is managed over RPC.
type VMServer struct {
	vm     snowman.ChainVM
	broker *plugin.GRPCBroker

	lock    sync.Mutex
	closed  bool
	servers []*grpc.Server
	conns   []*grpc.ClientConn

	toEngine chan common.Message
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(vm snowman.ChainVM, broker *plugin.GRPCBroker) *VMServer {
	return &VMServer{
		vm:     vm,
		broker: broker,
	}
}

// Initialize ...
func (vm *VMServer) Initialize(_ context.Context, req *vmproto.InitializeRequest) (*vmproto.InitializeResponse, error) {
	dbConn, err := vm.broker.Dial(req.DbServer)
	if err != nil {
		return nil, err
	}
	msgConn, err := vm.broker.Dial(req.EngineServer)
	if err != nil {
		dbConn.Close()
		return nil, err
	}

	dbClient := rpcdb.NewClient(dbproto.NewDatabaseClient(dbConn))
	msgClient := messenger.NewClient(msgproto.NewMessengerClient(msgConn))

	toEngine := make(chan common.Message, 1)
	go func() {
		for msg := range toEngine {
			msgClient.Notify(msg)
		}
	}()

	// TODO: Needs to populate a real context
	ctx := snow.DefaultContextTest()

	if err := vm.vm.Initialize(ctx, dbClient, req.GenesisBytes, toEngine, nil); err != nil {
		dbConn.Close()
		msgConn.Close()
		close(toEngine)
		return nil, err
	}

	vm.conns = append(vm.conns, dbConn)
	vm.conns = append(vm.conns, msgConn)
	vm.toEngine = toEngine
	return &vmproto.InitializeResponse{}, nil
}

// Shutdown ...
func (vm *VMServer) Shutdown(_ context.Context, _ *vmproto.ShutdownRequest) (*vmproto.ShutdownResponse, error) {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	if vm.closed || vm.toEngine == nil {
		return &vmproto.ShutdownResponse{}, nil
	}

	vm.closed = true

	vm.vm.Shutdown()
	close(vm.toEngine)

	errs := wrappers.Errs{}
	for _, conn := range vm.conns {
		errs.Add(conn.Close())
	}
	return &vmproto.ShutdownResponse{}, errs.Err
}

// CreateHandlers ...
func (vm *VMServer) CreateHandlers(_ context.Context, req *vmproto.CreateHandlersRequest) (*vmproto.CreateHandlersResponse, error) {
	handlers := vm.vm.CreateHandlers()
	resp := &vmproto.CreateHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		// start the messenger server
		serverID := vm.broker.NextId()
		go vm.broker.AcceptAndServe(serverID, func(opts []grpc.ServerOption) *grpc.Server {
			vm.lock.Lock()
			defer vm.lock.Unlock()

			server := grpc.NewServer(opts...)

			if vm.closed {
				server.Stop()
			} else {
				vm.servers = append(vm.servers, server)
			}

			httpproto.RegisterHTTPServer(server, ghttp.NewServer(handler.Handler, vm.broker))
			return server
		})

		resp.Handlers = append(resp.Handlers, &vmproto.Handler{
			Prefix:      prefix,
			LockOptions: uint32(handler.LockOptions),
			Server:      serverID,
		})
	}
	return resp, nil
}

// BuildBlock ...
func (vm *VMServer) BuildBlock(_ context.Context, _ *vmproto.BuildBlockRequest) (*vmproto.BuildBlockResponse, error) {
	blk, err := vm.vm.BuildBlock()
	if err != nil {
		return nil, err
	}
	return &vmproto.BuildBlockResponse{
		Id:       blk.ID().Bytes(),
		ParentID: blk.Parent().ID().Bytes(),
		Bytes:    blk.Bytes(),
	}, nil
}

// ParseBlock ...
func (vm *VMServer) ParseBlock(_ context.Context, req *vmproto.ParseBlockRequest) (*vmproto.ParseBlockResponse, error) {
	blk, err := vm.vm.ParseBlock(req.Bytes)
	if err != nil {
		return nil, err
	}
	return &vmproto.ParseBlockResponse{
		Id:       blk.ID().Bytes(),
		ParentID: blk.Parent().ID().Bytes(),
		Status:   uint32(blk.Status()),
	}, nil
}

// GetBlock ...
func (vm *VMServer) GetBlock(_ context.Context, req *vmproto.GetBlockRequest) (*vmproto.GetBlockResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	return &vmproto.GetBlockResponse{
		ParentID: blk.Parent().ID().Bytes(),
		Bytes:    blk.Bytes(),
		Status:   uint32(blk.Status()),
	}, nil
}

// SetPreference ...
func (vm *VMServer) SetPreference(_ context.Context, req *vmproto.SetPreferenceRequest) (*vmproto.SetPreferenceResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	vm.vm.SetPreference(id)
	return &vmproto.SetPreferenceResponse{}, nil
}

// LastAccepted ...
func (vm *VMServer) LastAccepted(_ context.Context, _ *vmproto.LastAcceptedRequest) (*vmproto.LastAcceptedResponse, error) {
	return &vmproto.LastAcceptedResponse{Id: vm.vm.LastAccepted().Bytes()}, nil
}

// BlockVerify ...
func (vm *VMServer) BlockVerify(_ context.Context, req *vmproto.BlockVerifyRequest) (*vmproto.BlockVerifyResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	return &vmproto.BlockVerifyResponse{}, blk.Verify()
}

// BlockAccept ...
func (vm *VMServer) BlockAccept(_ context.Context, req *vmproto.BlockAcceptRequest) (*vmproto.BlockAcceptResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	blk.Accept()
	return &vmproto.BlockAcceptResponse{}, nil
}

// BlockReject ...
func (vm *VMServer) BlockReject(_ context.Context, req *vmproto.BlockRejectRequest) (*vmproto.BlockRejectResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	blk.Reject()
	return &vmproto.BlockRejectResponse{}, nil
}

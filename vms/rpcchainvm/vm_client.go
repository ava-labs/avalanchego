// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"errors"
	"sync"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/gecko/database"
	"github.com/ava-labs/gecko/database/rpcdb"
	"github.com/ava-labs/gecko/database/rpcdb/rpcdbproto"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow"
	"github.com/ava-labs/gecko/snow/choices"
	"github.com/ava-labs/gecko/snow/consensus/snowman"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/vms/components/missing"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/ghttpproto"
	"github.com/ava-labs/gecko/vms/rpcchainvm/messenger"
	"github.com/ava-labs/gecko/vms/rpcchainvm/messenger/messengerproto"
	"github.com/ava-labs/gecko/vms/rpcchainvm/vmproto"
)

var (
	errUnsupportedFXs = errors.New("unsupported feature extensions")
)

// VMClient is an implementation of VM that talks over RPC.
type VMClient struct {
	client vmproto.VMClient
	broker *plugin.GRPCBroker
	proc   *plugin.Client

	db        *rpcdb.DatabaseServer
	messenger *messenger.Server

	lock    sync.Mutex
	closed  bool
	servers []*grpc.Server
	conns   []*grpc.ClientConn

	ctx  *snow.Context
	blks map[[32]byte]*BlockClient
}

// NewClient returns a database instance connected to a remote database instance
func NewClient(client vmproto.VMClient, broker *plugin.GRPCBroker) *VMClient {
	return &VMClient{
		client: client,
		broker: broker,
		blks:   make(map[[32]byte]*BlockClient),
	}
}

// SetProcess ...
func (vm *VMClient) SetProcess(proc *plugin.Client) {
	vm.proc = proc
}

// Initialize ...
func (vm *VMClient) Initialize(
	ctx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
) error {
	if len(fxs) != 0 {
		return errUnsupportedFXs
	}

	vm.ctx = ctx

	vm.db = rpcdb.NewServer(db)
	vm.messenger = messenger.NewServer(toEngine)

	// start the db server
	dbBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(dbBrokerID, vm.startDBServer)

	// start the messenger server
	messengerBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(messengerBrokerID, vm.startMessengerServer)

	_, err := vm.client.Initialize(context.Background(), &vmproto.InitializeRequest{
		DbServer:     dbBrokerID,
		GenesisBytes: genesisBytes,
		EngineServer: messengerBrokerID,
	})
	return err
}

func (vm *VMClient) startDBServer(opts []grpc.ServerOption) *grpc.Server {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	server := grpc.NewServer(opts...)

	if vm.closed {
		server.Stop()
	} else {
		vm.servers = append(vm.servers, server)
	}

	rpcdbproto.RegisterDatabaseServer(server, vm.db)
	return server
}

func (vm *VMClient) startMessengerServer(opts []grpc.ServerOption) *grpc.Server {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	server := grpc.NewServer(opts...)

	if vm.closed {
		server.Stop()
	} else {
		vm.servers = append(vm.servers, server)
	}

	messengerproto.RegisterMessengerServer(server, vm.messenger)
	return server
}

// Shutdown ...
func (vm *VMClient) Shutdown() {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	if vm.closed {
		return
	}

	vm.closed = true

	vm.client.Shutdown(context.Background(), &vmproto.ShutdownRequest{})

	for _, server := range vm.servers {
		server.Stop()
	}
	for _, conn := range vm.conns {
		conn.Close()
	}

	vm.proc.Kill()
}

// CreateHandlers ...
func (vm *VMClient) CreateHandlers() map[string]*common.HTTPHandler {
	vm.lock.Lock()
	defer vm.lock.Unlock()

	if vm.closed {
		return nil
	}

	resp, err := vm.client.CreateHandlers(context.Background(), &vmproto.CreateHandlersRequest{})
	vm.ctx.Log.AssertNoError(err)

	handlers := make(map[string]*common.HTTPHandler, len(resp.Handlers))
	for _, handler := range resp.Handlers {
		conn, err := vm.broker.Dial(handler.Server)
		vm.ctx.Log.AssertNoError(err)

		vm.conns = append(vm.conns, conn)
		handlers[handler.Prefix] = &common.HTTPHandler{
			LockOptions: common.LockOption(handler.LockOptions),
			Handler:     ghttp.NewClient(ghttpproto.NewHTTPClient(conn), vm.broker),
		}
	}
	return handlers
}

// BuildBlock ...
func (vm *VMClient) BuildBlock() (snowman.Block, error) {
	resp, err := vm.client.BuildBlock(context.Background(), &vmproto.BuildBlockRequest{})
	if err != nil {
		return nil, err
	}

	id, err := ids.ToID(resp.Id)
	vm.ctx.Log.AssertNoError(err)
	parentID, err := ids.ToID(resp.ParentID)
	vm.ctx.Log.AssertNoError(err)

	return &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Processing,
		bytes:    resp.Bytes,
	}, nil
}

// ParseBlock ...
func (vm *VMClient) ParseBlock(bytes []byte) (snowman.Block, error) {
	resp, err := vm.client.ParseBlock(context.Background(), &vmproto.ParseBlockRequest{
		Bytes: bytes,
	})
	if err != nil {
		return nil, err
	}

	id, err := ids.ToID(resp.Id)
	vm.ctx.Log.AssertNoError(err)

	if blk, cached := vm.blks[id.Key()]; cached {
		return blk, nil
	}

	parentID, err := ids.ToID(resp.ParentID)
	vm.ctx.Log.AssertNoError(err)
	status := choices.Status(resp.Status)
	vm.ctx.Log.AssertDeferredNoError(status.Valid)

	return &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    bytes,
	}, nil
}

// GetBlock ...
func (vm *VMClient) GetBlock(id ids.ID) (snowman.Block, error) {
	if blk, cached := vm.blks[id.Key()]; cached {
		return blk, nil
	}

	resp, err := vm.client.GetBlock(context.Background(), &vmproto.GetBlockRequest{
		Id: id.Bytes(),
	})
	if err != nil {
		return nil, err
	}

	parentID, err := ids.ToID(resp.ParentID)
	vm.ctx.Log.AssertNoError(err)
	status := choices.Status(resp.Status)
	vm.ctx.Log.AssertDeferredNoError(status.Valid)

	return &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    resp.Bytes,
	}, nil
}

// SetPreference ...
func (vm *VMClient) SetPreference(id ids.ID) {
	_, err := vm.client.SetPreference(context.Background(), &vmproto.SetPreferenceRequest{
		Id: id.Bytes(),
	})
	vm.ctx.Log.AssertNoError(err)
}

// LastAccepted ...
func (vm *VMClient) LastAccepted() ids.ID {
	resp, err := vm.client.LastAccepted(context.Background(), &vmproto.LastAcceptedRequest{})
	vm.ctx.Log.AssertNoError(err)

	id, err := ids.ToID(resp.Id)
	vm.ctx.Log.AssertNoError(err)

	return id
}

// BlockClient is an implementation of Block that talks over RPC.
type BlockClient struct {
	vm *VMClient

	id       ids.ID
	parentID ids.ID
	status   choices.Status
	bytes    []byte
}

// ID ...
func (b *BlockClient) ID() ids.ID { return b.id }

// Accept ...
func (b *BlockClient) Accept() {
	delete(b.vm.blks, b.id.Key())
	b.status = choices.Accepted
	_, err := b.vm.client.BlockAccept(context.Background(), &vmproto.BlockAcceptRequest{
		Id: b.id.Bytes(),
	})
	b.vm.ctx.Log.AssertNoError(err)
}

// Reject ...
func (b *BlockClient) Reject() {
	delete(b.vm.blks, b.id.Key())
	b.status = choices.Rejected
	_, err := b.vm.client.BlockReject(context.Background(), &vmproto.BlockRejectRequest{
		Id: b.id.Bytes(),
	})
	b.vm.ctx.Log.AssertNoError(err)
}

// Status ...
func (b *BlockClient) Status() choices.Status { return b.status }

// Parent ...
func (b *BlockClient) Parent() snowman.Block {
	if parent, err := b.vm.GetBlock(b.parentID); err == nil {
		return parent
	}
	return &missing.Block{BlkID: b.parentID}
}

// Verify ...
func (b *BlockClient) Verify() error {
	_, err := b.vm.client.BlockVerify(context.Background(), &vmproto.BlockVerifyRequest{
		Id: b.id.Bytes(),
	})
	if err != nil {
		return err
	}

	b.vm.blks[b.id.Key()] = b
	return nil
}

// Bytes ...
func (b *BlockClient) Bytes() []byte { return b.bytes }

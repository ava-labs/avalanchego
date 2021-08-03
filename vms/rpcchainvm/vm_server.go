// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"time"

	"google.golang.org/grpc"

	"github.com/hashicorp/go-plugin"

	"github.com/ava-labs/avalanchego/api/keystore/gkeystore"
	"github.com/ava-labs/avalanchego/api/keystore/gkeystore/gkeystoreproto"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory/gsharedmemoryproto"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/database/rpcdb/rpcdbproto"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/appsender"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/appsender/appsenderproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/galiaslookup"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/galiaslookup/galiaslookupproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp/ghttpproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gsubnetlookup"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gsubnetlookup/gsubnetlookupproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/messenger"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/messenger/messengerproto"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/vmproto"
)

var _ vmproto.VMServer = &VMServer{}

// VMServer is a VM that is managed over RPC.
type VMServer struct {
	vmproto.UnimplementedVMServer
	vm     block.ChainVM
	broker *plugin.GRPCBroker

	serverCloser grpcutils.ServerCloser
	connCloser   wrappers.Closer

	ctx      *snow.Context
	toEngine chan common.Message
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(vm block.ChainVM, broker *plugin.GRPCBroker) *VMServer {
	return &VMServer{
		vm:     vm,
		broker: broker,
	}
}

func (vm *VMServer) Initialize(_ context.Context, req *vmproto.InitializeRequest) (*vmproto.InitializeResponse, error) {
	subnetID, err := ids.ToID(req.SubnetID)
	if err != nil {
		return nil, err
	}
	chainID, err := ids.ToID(req.ChainID)
	if err != nil {
		return nil, err
	}
	nodeID, err := ids.ToShortID(req.NodeID)
	if err != nil {
		return nil, err
	}
	xChainID, err := ids.ToID(req.XChainID)
	if err != nil {
		return nil, err
	}
	avaxAssetID, err := ids.ToID(req.AvaxAssetID)
	if err != nil {
		return nil, err
	}

	epochFirstTransition := time.Time{}
	if err := epochFirstTransition.UnmarshalBinary(req.EpochFirstTransition); err != nil {
		return nil, err
	}

	// Dial each database in the request and construct the database manager
	versionedDBs := make([]*manager.VersionedDatabase, len(req.DbServers))
	versionParser := version.NewDefaultParser()
	for i, vDBReq := range req.DbServers {
		version, err := versionParser.Parse(vDBReq.Version)
		if err != nil {
			// Ignore closing errors to return the original error
			_ = vm.connCloser.Close()
			return nil, err
		}

		dbConn, err := vm.broker.Dial(vDBReq.DbServer)
		if err != nil {
			// Ignore closing errors to return the original error
			_ = vm.connCloser.Close()
			return nil, err
		}
		vm.connCloser.Add(dbConn)

		versionedDBs[i] = &manager.VersionedDatabase{
			Database: rpcdb.NewClient(rpcdbproto.NewDatabaseClient(dbConn)),
			Version:  version,
		}
	}
	dbManager, err := manager.NewManagerFromDBs(versionedDBs)
	if err != nil {
		// Ignore closing errors to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	msgConn, err := vm.broker.Dial(req.EngineServer)
	if err != nil {
		// Ignore closing errors to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}
	vm.connCloser.Add(msgConn)

	keystoreConn, err := vm.broker.Dial(req.KeystoreServer)
	if err != nil {
		// Ignore closing error to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}
	vm.connCloser.Add(keystoreConn)

	sharedMemoryConn, err := vm.broker.Dial(req.SharedMemoryServer)
	if err != nil {
		// Ignore closing error to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}
	vm.connCloser.Add(sharedMemoryConn)

	bcLookupConn, err := vm.broker.Dial(req.BcLookupServer)
	if err != nil {
		// Ignore closing error to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}
	vm.connCloser.Add(bcLookupConn)

	snLookupConn, err := vm.broker.Dial(req.SnLookupServer)
	if err != nil {
		// Ignore closing error to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	appSenderConn, err := vm.broker.Dial(req.AppSenderServer)
	if err != nil {
		// Ignore closing error to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	msgClient := messenger.NewClient(messengerproto.NewMessengerClient(msgConn))
	keystoreClient := gkeystore.NewClient(gkeystoreproto.NewKeystoreClient(keystoreConn), vm.broker)
	sharedMemoryClient := gsharedmemory.NewClient(gsharedmemoryproto.NewSharedMemoryClient(sharedMemoryConn))
	bcLookupClient := galiaslookup.NewClient(galiaslookupproto.NewAliasLookupClient(bcLookupConn))
	snLookupClient := gsubnetlookup.NewClient(gsubnetlookupproto.NewSubnetLookupClient(snLookupConn))
	appSenderClient := appsender.NewClient(appsenderproto.NewAppSenderClient(appSenderConn))

	toEngine := make(chan common.Message, 1)
	go func() {
		for msg := range toEngine {
			// Nothing to do with the error within the goroutine
			_ = msgClient.Notify(msg)
		}
	}()

	vm.ctx = &snow.Context{
		NetworkID:            req.NetworkID,
		SubnetID:             subnetID,
		ChainID:              chainID,
		NodeID:               nodeID,
		XChainID:             xChainID,
		AVAXAssetID:          avaxAssetID,
		Log:                  logging.NoLog{},
		DecisionDispatcher:   nil,
		ConsensusDispatcher:  nil,
		Keystore:             keystoreClient,
		SharedMemory:         sharedMemoryClient,
		BCLookup:             bcLookupClient,
		SNLookup:             snLookupClient,
		EpochFirstTransition: epochFirstTransition,
		EpochDuration:        time.Duration(req.EpochDuration),
	}

	if err := vm.vm.Initialize(vm.ctx, dbManager, req.GenesisBytes, req.UpgradeBytes, req.ConfigBytes, toEngine, nil, appSenderClient); err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.connCloser.Close()
		close(toEngine)
		return nil, err
	}

	vm.toEngine = toEngine
	lastAccepted, err := vm.vm.LastAccepted()
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(lastAccepted)
	if err != nil {
		return nil, err
	}
	parentID := blk.Parent().ID()
	return &vmproto.InitializeResponse{
		LastAcceptedID:       lastAccepted[:],
		LastAcceptedParentID: parentID[:],
		Status:               uint32(choices.Accepted),
		Height:               blk.Height(),
		Bytes:                blk.Bytes(),
	}, err
}

func (vm *VMServer) Bootstrapping(context.Context, *vmproto.EmptyMsg) (*vmproto.EmptyMsg, error) {
	return &vmproto.EmptyMsg{}, vm.vm.Bootstrapping()
}

func (vm *VMServer) Bootstrapped(context.Context, *vmproto.EmptyMsg) (*vmproto.EmptyMsg, error) {
	vm.ctx.Bootstrapped()
	return &vmproto.EmptyMsg{}, vm.vm.Bootstrapped()
}

func (vm *VMServer) Shutdown(context.Context, *vmproto.EmptyMsg) (*vmproto.EmptyMsg, error) {
	if vm.toEngine == nil {
		return &vmproto.EmptyMsg{}, nil
	}
	errs := wrappers.Errs{}
	errs.Add(vm.vm.Shutdown())
	close(vm.toEngine)
	vm.serverCloser.Stop()
	errs.Add(vm.connCloser.Close())
	return &vmproto.EmptyMsg{}, errs.Err
}

func (vm *VMServer) CreateStaticHandlers(context.Context, *vmproto.EmptyMsg) (*vmproto.CreateStaticHandlersResponse, error) {
	handlers, err := vm.vm.CreateStaticHandlers()
	if err != nil {
		return nil, err
	}
	resp := &vmproto.CreateStaticHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		// start the messenger server
		serverID := vm.broker.NextId()
		go vm.broker.AcceptAndServe(serverID, func(opts []grpc.ServerOption) *grpc.Server {
			server := grpc.NewServer(opts...)
			vm.serverCloser.Add(server)
			ghttpproto.RegisterHTTPServer(server, ghttp.NewServer(handler.Handler, vm.broker))
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

func (vm *VMServer) CreateHandlers(context.Context, *vmproto.EmptyMsg) (*vmproto.CreateHandlersResponse, error) {
	handlers, err := vm.vm.CreateHandlers()
	if err != nil {
		return nil, err
	}
	resp := &vmproto.CreateHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		// start the messenger server
		serverID := vm.broker.NextId()
		go vm.broker.AcceptAndServe(serverID, func(opts []grpc.ServerOption) *grpc.Server {
			server := grpc.NewServer(opts...)
			vm.serverCloser.Add(server)
			ghttpproto.RegisterHTTPServer(server, ghttp.NewServer(handler.Handler, vm.broker))
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

func (vm *VMServer) BuildBlock(context.Context, *vmproto.EmptyMsg) (*vmproto.BuildBlockResponse, error) {
	blk, err := vm.vm.BuildBlock()
	if err != nil {
		return nil, err
	}
	blkID := blk.ID()
	parentID := blk.Parent().ID()
	return &vmproto.BuildBlockResponse{
		Id:       blkID[:],
		ParentID: parentID[:],
		Bytes:    blk.Bytes(),
		Height:   blk.Height(),
	}, nil
}

func (vm *VMServer) ParseBlock(_ context.Context, req *vmproto.ParseBlockRequest) (*vmproto.ParseBlockResponse, error) {
	blk, err := vm.vm.ParseBlock(req.Bytes)
	if err != nil {
		return nil, err
	}
	blkID := blk.ID()
	parentID := blk.Parent().ID()
	return &vmproto.ParseBlockResponse{
		Id:       blkID[:],
		ParentID: parentID[:],
		Status:   uint32(blk.Status()),
		Height:   blk.Height(),
	}, nil
}

func (vm *VMServer) GetBlock(_ context.Context, req *vmproto.GetBlockRequest) (*vmproto.GetBlockResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	parentID := blk.Parent().ID()
	return &vmproto.GetBlockResponse{
		ParentID: parentID[:],
		Bytes:    blk.Bytes(),
		Status:   uint32(blk.Status()),
		Height:   blk.Height(),
	}, nil
}

func (vm *VMServer) SetPreference(_ context.Context, req *vmproto.SetPreferenceRequest) (*vmproto.EmptyMsg, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	return &vmproto.EmptyMsg{}, vm.vm.SetPreference(id)
}

func (vm *VMServer) Health(context.Context, *vmproto.EmptyMsg) (*vmproto.HealthResponse, error) {
	details, err := vm.vm.HealthCheck()
	if err != nil {
		return &vmproto.HealthResponse{}, err
	}

	// Try to stringify the details
	detailsStr := "couldn't parse health check details to string"
	switch details := details.(type) {
	case nil:
		detailsStr = ""
	case string:
		detailsStr = details
	case map[string]string:
		asJSON, err := json.Marshal(details)
		if err != nil {
			detailsStr = string(asJSON)
		}
	case []byte:
		detailsStr = string(details)
	}

	return &vmproto.HealthResponse{
		Details: detailsStr,
	}, nil
}

func (vm *VMServer) Version(context.Context, *vmproto.EmptyMsg) (*vmproto.VersionResponse, error) {
	version, err := vm.vm.Version()
	return &vmproto.VersionResponse{
		Version: version,
	}, err
}

func (vm *VMServer) AppRequest(_ context.Context, req *vmproto.AppRequestMsg) (*vmproto.EmptyMsg, error) {
	nodeID, err := ids.ToShortID(req.NodeID)
	if err != nil {
		return nil, err
	}
	return nil, vm.vm.AppRequest(nodeID, req.RequestID, req.Request)
}

func (vm *VMServer) AppRequestFailed(_ context.Context, req *vmproto.AppRequestFailedMsg) (*vmproto.EmptyMsg, error) {
	nodeID, err := ids.ToShortID(req.NodeID)
	if err != nil {
		return nil, err
	}
	return nil, vm.vm.AppRequestFailed(nodeID, req.RequestID)
}

func (vm *VMServer) AppResponse(_ context.Context, req *vmproto.AppResponseMsg) (*vmproto.EmptyMsg, error) {
	nodeID, err := ids.ToShortID(req.NodeID)
	if err != nil {
		return nil, err
	}
	return nil, vm.vm.AppResponse(nodeID, req.RequestID, req.Response)
}

func (vm *VMServer) AppGossip(_ context.Context, req *vmproto.AppGossipMsg) (*vmproto.EmptyMsg, error) {
	nodeID, err := ids.ToShortID(req.NodeID)
	if err != nil {
		return nil, err
	}
	return nil, vm.vm.AppGossip(nodeID, req.Msg)
}

func (vm *VMServer) BlockVerify(_ context.Context, req *vmproto.BlockVerifyRequest) (*vmproto.EmptyMsg, error) {
	blk, err := vm.vm.ParseBlock(req.Bytes)
	if err != nil {
		return nil, err
	}
	return &vmproto.EmptyMsg{}, blk.Verify()
}

func (vm *VMServer) BlockAccept(_ context.Context, req *vmproto.BlockAcceptRequest) (*vmproto.EmptyMsg, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	if err := blk.Accept(); err != nil {
		return nil, err
	}
	return &vmproto.EmptyMsg{}, nil
}

func (vm *VMServer) BlockReject(_ context.Context, req *vmproto.BlockRejectRequest) (*vmproto.EmptyMsg, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	if err := blk.Reject(); err != nil {
		return nil, err
	}
	return &vmproto.EmptyMsg{}, nil
}

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/api/keystore/gkeystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ids/galiasreader"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/appsender"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/gsubnetlookup"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/messenger"

	aliasreaderpb "github.com/ava-labs/avalanchego/proto/pb/aliasreader"
	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	keystorepb "github.com/ava-labs/avalanchego/proto/pb/keystore"
	messengerpb "github.com/ava-labs/avalanchego/proto/pb/messenger"
	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
	sharedmemorypb "github.com/ava-labs/avalanchego/proto/pb/sharedmemory"
	subnetlookuppb "github.com/ava-labs/avalanchego/proto/pb/subnetlookup"
	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
)

var (
	versionParser = version.NewDefaultApplicationParser()

	_ vmpb.VMServer = &VMServer{}
)

// VMServer is a VM that is managed over RPC.
type VMServer struct {
	vmpb.UnimplementedVMServer
	vm block.ChainVM

	serverCloser grpcutils.ServerCloser
	connCloser   wrappers.Closer

	ctx    *snow.Context
	closed chan struct{}
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(vm block.ChainVM) *VMServer {
	return &VMServer{
		vm: vm,
	}
}

func (vm *VMServer) Initialize(_ context.Context, req *vmpb.InitializeRequest) (*vmpb.InitializeResponse, error) {
	subnetID, err := ids.ToID(req.SubnetId)
	if err != nil {
		return nil, err
	}
	chainID, err := ids.ToID(req.ChainId)
	if err != nil {
		return nil, err
	}
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	xChainID, err := ids.ToID(req.XChainId)
	if err != nil {
		return nil, err
	}
	avaxAssetID, err := ids.ToID(req.AvaxAssetId)
	if err != nil {
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

		clientConn, err := grpcutils.Dial(vDBReq.ServerAddr)
		if err != nil {
			// Ignore closing errors to return the original error
			_ = vm.connCloser.Close()
			return nil, err
		}
		vm.connCloser.Add(clientConn)
		db := rpcdb.NewClient(rpcdbpb.NewDatabaseClient(clientConn))
		versionedDBs[i] = &manager.VersionedDatabase{
			Database: corruptabledb.New(db),
			Version:  version,
		}
	}
	dbManager, err := manager.NewManagerFromDBs(versionedDBs)
	if err != nil {
		// Ignore closing errors to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	clientConn, err := grpcutils.Dial(req.ServerAddr)
	if err != nil {
		// Ignore closing errors to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	vm.connCloser.Add(clientConn)

	msgClient := messenger.NewClient(messengerpb.NewMessengerClient(clientConn))
	keystoreClient := gkeystore.NewClient(keystorepb.NewKeystoreClient(clientConn))
	sharedMemoryClient := gsharedmemory.NewClient(sharedmemorypb.NewSharedMemoryClient(clientConn))
	bcLookupClient := galiasreader.NewClient(aliasreaderpb.NewAliasReaderClient(clientConn))
	snLookupClient := gsubnetlookup.NewClient(subnetlookuppb.NewSubnetLookupClient(clientConn))
	appSenderClient := appsender.NewClient(appsenderpb.NewAppSenderClient(clientConn))

	toEngine := make(chan common.Message, 1)
	vm.closed = make(chan struct{})
	go func() {
		for {
			select {
			case msg, ok := <-toEngine:
				if !ok {
					return
				}
				// Nothing to do with the error within the goroutine
				_ = msgClient.Notify(msg)
			case <-vm.closed:
				return
			}
		}
	}()

	vm.ctx = &snow.Context{
		NetworkID: req.NetworkId,
		SubnetID:  subnetID,
		ChainID:   chainID,
		NodeID:    nodeID,

		XChainID:    xChainID,
		AVAXAssetID: avaxAssetID,

		Log:          logging.NoLog{},
		Keystore:     keystoreClient,
		SharedMemory: sharedMemoryClient,
		BCLookup:     bcLookupClient,
		SNLookup:     snLookupClient,
		Metrics:      metrics.NewOptionalGatherer(),

		// TODO: support snowman++ fields
	}

	if err := vm.vm.Initialize(vm.ctx, dbManager, req.GenesisBytes, req.UpgradeBytes, req.ConfigBytes, toEngine, nil, appSenderClient); err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.connCloser.Close()
		close(vm.closed)
		return nil, err
	}

	lastAccepted, err := vm.vm.LastAccepted()
	if err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.vm.Shutdown()
		_ = vm.connCloser.Close()
		close(vm.closed)
		return nil, err
	}

	blk, err := vm.vm.GetBlock(lastAccepted)
	if err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.vm.Shutdown()
		_ = vm.connCloser.Close()
		close(vm.closed)
		return nil, err
	}
	parentID := blk.Parent()
	timeBytes, err := blk.Timestamp().MarshalBinary()
	return &vmpb.InitializeResponse{
		LastAcceptedId:       lastAccepted[:],
		LastAcceptedParentId: parentID[:],
		Status:               uint32(choices.Accepted),
		Height:               blk.Height(),
		Bytes:                blk.Bytes(),
		Timestamp:            timeBytes,
	}, err
}

func (vm *VMServer) VerifyHeightIndex(context.Context, *emptypb.Empty) (*vmpb.VerifyHeightIndexResponse, error) {
	var err error
	if hVM, ok := vm.vm.(block.HeightIndexedChainVM); ok {
		err = hVM.VerifyHeightIndex()
	} else {
		err = block.ErrHeightIndexedVMNotImplemented
	}
	return &vmpb.VerifyHeightIndexResponse{
		Err: errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetBlockIDAtHeight(ctx context.Context, req *vmpb.GetBlockIDAtHeightRequest) (*vmpb.GetBlockIDAtHeightResponse, error) {
	var (
		blkID ids.ID
		err   error
	)
	if hVM, ok := vm.vm.(block.HeightIndexedChainVM); ok {
		blkID, err = hVM.GetBlockIDAtHeight(req.Height)
	} else {
		err = block.ErrHeightIndexedVMNotImplemented
	}
	return &vmpb.GetBlockIDAtHeightResponse{
		BlkId: blkID[:],
		Err:   errorToErrCode[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) SetState(_ context.Context, stateReq *vmpb.SetStateRequest) (*emptypb.Empty, error) {
	return &emptypb.Empty{}, vm.vm.SetState(snow.State(stateReq.State))
}

func (vm *VMServer) Shutdown(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	if vm.closed == nil {
		return &emptypb.Empty{}, nil
	}
	errs := wrappers.Errs{}
	errs.Add(vm.vm.Shutdown())
	close(vm.closed)
	vm.serverCloser.Stop()
	errs.Add(vm.connCloser.Close())
	return &emptypb.Empty{}, errs.Err
}

func (vm *VMServer) CreateStaticHandlers(context.Context, *emptypb.Empty) (*vmpb.CreateStaticHandlersResponse, error) {
	handlers, err := vm.vm.CreateStaticHandlers()
	if err != nil {
		return nil, err
	}
	resp := &vmpb.CreateStaticHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		serverListener, err := grpcutils.NewListener()
		if err != nil {
			return nil, err
		}
		serverAddr := serverListener.Addr().String()

		// Start the gRPC server which serves the HTTP service
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

func (vm *VMServer) CreateHandlers(context.Context, *emptypb.Empty) (*vmpb.CreateHandlersResponse, error) {
	handlers, err := vm.vm.CreateHandlers()
	if err != nil {
		return nil, err
	}
	resp := &vmpb.CreateHandlersResponse{}
	for prefix, h := range handlers {
		handler := h

		serverListener, err := grpcutils.NewListener()
		if err != nil {
			return nil, err
		}
		serverAddr := serverListener.Addr().String()

		// Start the gRPC server which serves the HTTP service
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

func (vm *VMServer) BuildBlock(context.Context, *emptypb.Empty) (*vmpb.BuildBlockResponse, error) {
	blk, err := vm.vm.BuildBlock()
	if err != nil {
		return nil, err
	}
	blkID := blk.ID()
	parentID := blk.Parent()
	timeBytes, err := blk.Timestamp().MarshalBinary()
	return &vmpb.BuildBlockResponse{
		Id:        blkID[:],
		ParentId:  parentID[:],
		Bytes:     blk.Bytes(),
		Height:    blk.Height(),
		Timestamp: timeBytes,
	}, err
}

func (vm *VMServer) ParseBlock(_ context.Context, req *vmpb.ParseBlockRequest) (*vmpb.ParseBlockResponse, error) {
	blk, err := vm.vm.ParseBlock(req.Bytes)
	if err != nil {
		return nil, err
	}
	blkID := blk.ID()
	parentID := blk.Parent()
	timeBytes, err := blk.Timestamp().MarshalBinary()
	return &vmpb.ParseBlockResponse{
		Id:        blkID[:],
		ParentId:  parentID[:],
		Status:    uint32(blk.Status()),
		Height:    blk.Height(),
		Timestamp: timeBytes,
	}, err
}

func (vm *VMServer) GetAncestors(_ context.Context, req *vmpb.GetAncestorsRequest) (*vmpb.GetAncestorsResponse, error) {
	blkID, err := ids.ToID(req.BlkId)
	if err != nil {
		return nil, err
	}
	maxBlksNum := int(req.MaxBlocksNum)
	maxBlksSize := int(req.MaxBlocksSize)
	maxBlocksRetrivalTime := time.Duration(req.MaxBlocksRetrivalTime)

	blocks, err := block.GetAncestors(
		vm.vm,
		blkID,
		maxBlksNum,
		maxBlksSize,
		maxBlocksRetrivalTime,
	)
	return &vmpb.GetAncestorsResponse{
		BlksBytes: blocks,
	}, err
}

func (vm *VMServer) BatchedParseBlock(
	ctx context.Context,
	req *vmpb.BatchedParseBlockRequest,
) (*vmpb.BatchedParseBlockResponse, error) {
	blocks := make([]*vmpb.ParseBlockResponse, len(req.Request))
	for i, blockBytes := range req.Request {
		block, err := vm.ParseBlock(ctx, &vmpb.ParseBlockRequest{
			Bytes: blockBytes,
		})
		if err != nil {
			return nil, err
		}
		blocks[i] = block
	}
	return &vmpb.BatchedParseBlockResponse{
		Response: blocks,
	}, nil
}

func (vm *VMServer) GetBlock(_ context.Context, req *vmpb.GetBlockRequest) (*vmpb.GetBlockResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(id)
	if err != nil {
		return nil, err
	}
	parentID := blk.Parent()
	timeBytes, err := blk.Timestamp().MarshalBinary()
	return &vmpb.GetBlockResponse{
		ParentId:  parentID[:],
		Bytes:     blk.Bytes(),
		Status:    uint32(blk.Status()),
		Height:    blk.Height(),
		Timestamp: timeBytes,
	}, err
}

func (vm *VMServer) SetPreference(_ context.Context, req *vmpb.SetPreferenceRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.SetPreference(id)
}

func (vm *VMServer) Health(ctx context.Context, req *vmpb.HealthRequest) (*vmpb.HealthResponse, error) {
	// Perform health checks for vm client gRPC servers
	err := vm.grpcHealthChecks(ctx, req.GrpcChecks)
	if err != nil {
		return &vmpb.HealthResponse{}, err
	}

	details, err := vm.vm.HealthCheck()
	if err != nil {
		return &vmpb.HealthResponse{}, err
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

	return &vmpb.HealthResponse{
		Details: detailsStr,
	}, nil
}

func (vm *VMServer) grpcHealthChecks(ctx context.Context, checks map[string]string) error {
	var errs []error
	for name, address := range checks {
		clientConn, err := grpcutils.Dial(address)
		if err != nil {
			errs = append(errs, fmt.Errorf("grpc health check failed to dial: %q address: %s: %w", name, address, err))
			continue
		}
		client := grpc_health_v1.NewHealthClient(clientConn)
		_, err = client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
		if err != nil {
			errs = append(errs, fmt.Errorf("grpc health check failed for %q address: %s: %w", name, address, err))
		}
		err = clientConn.Close()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return wrappers.NewAggregate(errs)
}

func (vm *VMServer) Version(context.Context, *emptypb.Empty) (*vmpb.VersionResponse, error) {
	version, err := vm.vm.Version()
	return &vmpb.VersionResponse{
		Version: version,
	}, err
}

func (vm *VMServer) Connected(_ context.Context, req *vmpb.ConnectedRequest) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}

	peerVersion, err := versionParser.Parse(req.Version)
	if err != nil {
		return nil, err
	}

	return &emptypb.Empty{}, vm.vm.Connected(nodeID, peerVersion)
}

func (vm *VMServer) Disconnected(_ context.Context, req *vmpb.DisconnectedRequest) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.Disconnected(nodeID)
}

func (vm *VMServer) AppRequest(_ context.Context, req *vmpb.AppRequestMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	var deadline time.Time
	if err := deadline.UnmarshalBinary(req.Deadline); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppRequest(nodeID, req.RequestId, deadline, req.Request)
}

func (vm *VMServer) AppRequestFailed(_ context.Context, req *vmpb.AppRequestFailedMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppRequestFailed(nodeID, req.RequestId)
}

func (vm *VMServer) AppResponse(_ context.Context, req *vmpb.AppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppResponse(nodeID, req.RequestId, req.Response)
}

func (vm *VMServer) AppGossip(_ context.Context, req *vmpb.AppGossipMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToShortID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppGossip(nodeID, req.Msg)
}

func (vm *VMServer) BlockVerify(_ context.Context, req *vmpb.BlockVerifyRequest) (*vmpb.BlockVerifyResponse, error) {
	blk, err := vm.vm.ParseBlock(req.Bytes)
	if err != nil {
		return nil, err
	}
	if err := blk.Verify(); err != nil {
		return nil, err
	}
	timeBytes, err := blk.Timestamp().MarshalBinary()
	return &vmpb.BlockVerifyResponse{
		Timestamp: timeBytes,
	}, err
}

func (vm *VMServer) BlockAccept(_ context.Context, req *vmpb.BlockAcceptRequest) (*emptypb.Empty, error) {
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
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) BlockReject(_ context.Context, req *vmpb.BlockRejectRequest) (*emptypb.Empty, error) {
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
	return &emptypb.Empty{}, nil
}

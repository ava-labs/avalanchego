// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.uber.org/zap"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/corruptabledb"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ids/galiasreader"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/appsender"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators"
	"github.com/ava-labs/avalanchego/snow/validators/gvalidators"
	"github.com/ava-labs/avalanchego/upgrade"
	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/gwarp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"

	aliasreaderpb "github.com/ava-labs/avalanchego/proto/pb/aliasreader"
	appsenderpb "github.com/ava-labs/avalanchego/proto/pb/appsender"
	httppb "github.com/ava-labs/avalanchego/proto/pb/http"
	rpcdbpb "github.com/ava-labs/avalanchego/proto/pb/rpcdb"
	sharedmemorypb "github.com/ava-labs/avalanchego/proto/pb/sharedmemory"
	validatorstatepb "github.com/ava-labs/avalanchego/proto/pb/validatorstate"
	vmpb "github.com/ava-labs/avalanchego/proto/pb/vm"
	warppb "github.com/ava-labs/avalanchego/proto/pb/warp"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
)

var (
	_ vmpb.VMServer = (*VMServer)(nil)

	originalStderr = os.Stderr

	errExpectedBlockWithVerifyContext = errors.New("expected block.WithVerifyContext")
	errNilNetworkUpgradesPB           = errors.New("network upgrades protobuf is nil")
)

// VMServer is a VM that is managed over RPC.
type VMServer struct {
	vmpb.UnsafeVMServer

	vm block.ChainVM
	// If nil, the underlying VM doesn't implement the interface.
	bVM block.BuildBlockWithContextChainVM
	// If nil, the underlying VM doesn't implement the interface.
	ssVM block.StateSyncableVM

	allowShutdown *utils.Atomic[bool]

	metrics metrics.MultiGatherer
	db      database.Database
	log     logging.Logger

	serverCloser grpcutils.ServerCloser
	connCloser   wrappers.Closer

	ctx    *snow.Context
	closed chan struct{}
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(vm block.ChainVM, allowShutdown *utils.Atomic[bool]) *VMServer {
	bVM, _ := vm.(block.BuildBlockWithContextChainVM)
	ssVM, _ := vm.(block.StateSyncableVM)
	vmSrv := &VMServer{
		metrics:       metrics.NewPrefixGatherer(),
		vm:            vm,
		bVM:           bVM,
		ssVM:          ssVM,
		allowShutdown: allowShutdown,
	}
	return vmSrv
}

func (vm *VMServer) Initialize(ctx context.Context, req *vmpb.InitializeRequest) (*vmpb.InitializeResponse, error) {
	subnetID, err := ids.ToID(req.SubnetId)
	if err != nil {
		return nil, err
	}
	chainID, err := ids.ToID(req.ChainId)
	if err != nil {
		return nil, err
	}
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	publicKey, err := bls.PublicKeyFromCompressedBytes(req.PublicKey)
	if err != nil {
		return nil, err
	}

	networkUpgrades, err := convertNetworkUpgrades(req.NetworkUpgrades)
	if err != nil {
		return nil, err
	}

	xChainID, err := ids.ToID(req.XChainId)
	if err != nil {
		return nil, err
	}
	cChainID, err := ids.ToID(req.CChainId)
	if err != nil {
		return nil, err
	}
	avaxAssetID, err := ids.ToID(req.AvaxAssetId)
	if err != nil {
		return nil, err
	}

	processMetrics, err := metrics.MakeAndRegister(
		vm.metrics,
		"process",
	)
	if err != nil {
		return nil, err
	}

	// Current state of process metrics
	processCollector := collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})
	if err := processMetrics.Register(processCollector); err != nil {
		return nil, err
	}

	// Go process metrics using debug.GCStats
	goCollector := collectors.NewGoCollector()
	if err := processMetrics.Register(goCollector); err != nil {
		return nil, err
	}

	grpcMetrics, err := metrics.MakeAndRegister(
		vm.metrics,
		"grpc",
	)
	if err != nil {
		return nil, err
	}

	// gRPC client metrics
	grpcClientMetrics := grpc_prometheus.NewClientMetrics()
	if err := grpcMetrics.Register(grpcClientMetrics); err != nil {
		return nil, err
	}

	vmMetrics := metrics.NewPrefixGatherer()
	if err := vm.metrics.Register("vm", vmMetrics); err != nil {
		return nil, err
	}

	// Dial the database
	dbClientConn, err := grpcutils.Dial(
		req.DbServerAddr,
		grpcutils.WithChainUnaryInterceptor(grpcClientMetrics.UnaryClientInterceptor()),
		grpcutils.WithChainStreamInterceptor(grpcClientMetrics.StreamClientInterceptor()),
	)
	if err != nil {
		return nil, err
	}
	vm.connCloser.Add(dbClientConn)

	// TODO: Allow the logger to be configured by the client
	vm.log = logging.NewLogger(
		fmt.Sprintf("<%s Chain>", chainID),
		logging.NewWrappedCore(
			logging.Info,
			originalStderr,
			logging.Colors.ConsoleEncoder(),
		),
	)

	vm.db = corruptabledb.New(
		rpcdb.NewClient(rpcdbpb.NewDatabaseClient(dbClientConn)),
		vm.log,
	)

	clientConn, err := grpcutils.Dial(
		req.ServerAddr,
		grpcutils.WithChainUnaryInterceptor(grpcClientMetrics.UnaryClientInterceptor()),
		grpcutils.WithChainStreamInterceptor(grpcClientMetrics.StreamClientInterceptor()),
	)
	if err != nil {
		// Ignore closing errors to return the original error
		_ = vm.connCloser.Close()
		return nil, err
	}

	vm.connCloser.Add(clientConn)

	sharedMemoryClient := gsharedmemory.NewClient(sharedmemorypb.NewSharedMemoryClient(clientConn))
	bcLookupClient := galiasreader.NewClient(aliasreaderpb.NewAliasReaderClient(clientConn))
	appSenderClient := appsender.NewClient(appsenderpb.NewAppSenderClient(clientConn))
	validatorStateClient := gvalidators.NewClient(validatorstatepb.NewValidatorStateClient(clientConn))
	warpSignerClient := gwarp.NewClient(warppb.NewSignerClient(clientConn))

	vm.closed = make(chan struct{})

	vm.ctx = &snow.Context{
		NetworkID:       req.NetworkId,
		SubnetID:        subnetID,
		ChainID:         chainID,
		NodeID:          nodeID,
		PublicKey:       publicKey,
		NetworkUpgrades: networkUpgrades,

		XChainID:    xChainID,
		CChainID:    cChainID,
		AVAXAssetID: avaxAssetID,

		Log:          vm.log,
		SharedMemory: sharedMemoryClient,
		BCLookup:     bcLookupClient,
		Metrics:      vmMetrics,

		// Signs warp messages
		WarpSigner: warpSignerClient,

		// The validator state is cached to avoid the gRPC overhead after
		// epoching is activated.
		ValidatorState: validators.NewCachedState(validatorStateClient, networkUpgrades.GraniteTime),
		// TODO: support remaining snowman++ fields

		ChainDataDir: req.ChainDataDir,
	}

	if err := vm.vm.Initialize(ctx, vm.ctx, vm.db, req.GenesisBytes, req.UpgradeBytes, req.ConfigBytes, nil, appSenderClient); err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.connCloser.Close()
		close(vm.closed)
		vm.log.Error("failed to initialize vm", zap.Error(err))
		return nil, err
	}

	lastAccepted, err := vm.vm.LastAccepted(ctx)
	if err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.vm.Shutdown(ctx)
		_ = vm.connCloser.Close()
		close(vm.closed)
		vm.log.Error("failed to get last accepted block ID", zap.Error(err))
		return nil, err
	}

	blk, err := vm.vm.GetBlock(ctx, lastAccepted)
	if err != nil {
		// Ignore errors closing resources to return the original error
		_ = vm.vm.Shutdown(ctx)
		_ = vm.connCloser.Close()
		close(vm.closed)
		vm.log.Error("failed to get last accepted block", zap.Error(err))
		return nil, err
	}
	parentID := blk.Parent()
	return &vmpb.InitializeResponse{
		LastAcceptedId:       lastAccepted[:],
		LastAcceptedParentId: parentID[:],
		Height:               blk.Height(),
		Bytes:                blk.Bytes(),
		Timestamp:            grpcutils.TimestampFromTime(blk.Timestamp()),
	}, nil
}

func (vm *VMServer) SetState(ctx context.Context, stateReq *vmpb.SetStateRequest) (*vmpb.SetStateResponse, error) {
	err := vm.vm.SetState(ctx, snow.State(stateReq.State))
	if err != nil {
		return nil, err
	}

	lastAccepted, err := vm.vm.LastAccepted(ctx)
	if err != nil {
		return nil, err
	}

	blk, err := vm.vm.GetBlock(ctx, lastAccepted)
	if err != nil {
		return nil, err
	}

	parentID := blk.Parent()
	return &vmpb.SetStateResponse{
		LastAcceptedId:       lastAccepted[:],
		LastAcceptedParentId: parentID[:],
		Height:               blk.Height(),
		Bytes:                blk.Bytes(),
		Timestamp:            grpcutils.TimestampFromTime(blk.Timestamp()),
	}, nil
}

func (vm *VMServer) Shutdown(ctx context.Context, _ *emptypb.Empty) (*emptypb.Empty, error) {
	vm.allowShutdown.Set(true)
	if vm.closed == nil {
		return &emptypb.Empty{}, nil
	}
	errs := wrappers.Errs{}
	errs.Add(vm.vm.Shutdown(ctx))
	close(vm.closed)
	vm.serverCloser.Stop()
	errs.Add(vm.connCloser.Close())
	return &emptypb.Empty{}, errs.Err
}

func (vm *VMServer) CreateHandlers(ctx context.Context, _ *emptypb.Empty) (*vmpb.CreateHandlersResponse, error) {
	handlers, err := vm.vm.CreateHandlers(ctx)
	if err != nil {
		return nil, err
	}
	resp := &vmpb.CreateHandlersResponse{}
	for prefix, handler := range handlers {
		serverListener, err := grpcutils.NewListener()
		if err != nil {
			return nil, err
		}
		server := grpcutils.NewServer()
		vm.serverCloser.Add(server)
		httppb.RegisterHTTPServer(server, ghttp.NewServer(handler))

		// Start HTTP service
		go grpcutils.Serve(serverListener, server)

		resp.Handlers = append(resp.Handlers, &vmpb.Handler{
			Prefix:     prefix,
			ServerAddr: serverListener.Addr().String(),
		})
	}
	return resp, nil
}

func (vm *VMServer) NewHTTPHandler(ctx context.Context, _ *emptypb.Empty) (*vmpb.NewHTTPHandlerResponse, error) {
	handler, err := vm.vm.NewHTTPHandler(ctx)
	if err != nil {
		return nil, err
	}

	if handler == nil {
		return &vmpb.NewHTTPHandlerResponse{}, nil
	}

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return nil, err
	}
	server := grpcutils.NewServer()
	vm.serverCloser.Add(server)
	httppb.RegisterHTTPServer(server, ghttp.NewServer(handler))

	// Start HTTP service
	go grpcutils.Serve(serverListener, server)

	return &vmpb.NewHTTPHandlerResponse{
		ServerAddr: serverListener.Addr().String(),
	}, nil
}

func (vm *VMServer) WaitForEvent(ctx context.Context, _ *emptypb.Empty) (*vmpb.WaitForEventResponse, error) {
	message, err := vm.vm.WaitForEvent(ctx)
	if err != nil {
		vm.log.Debug("Received error while waiting for event", zap.Error(err))
	}
	return &vmpb.WaitForEventResponse{
		Message: vmpb.Message(message),
	}, err
}

func (vm *VMServer) Connected(ctx context.Context, req *vmpb.ConnectedRequest) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	peerVersion := &version.Application{
		Name:  req.Name,
		Major: int(req.Major),
		Minor: int(req.Minor),
		Patch: int(req.Patch),
	}
	return &emptypb.Empty{}, vm.vm.Connected(ctx, nodeID, peerVersion)
}

func (vm *VMServer) Disconnected(ctx context.Context, req *vmpb.DisconnectedRequest) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.Disconnected(ctx, nodeID)
}

// If the underlying VM doesn't actually implement this method, its [BuildBlock]
// method will be called instead.
func (vm *VMServer) BuildBlock(ctx context.Context, req *vmpb.BuildBlockRequest) (*vmpb.BuildBlockResponse, error) {
	var (
		blk snowman.Block
		err error
	)
	if vm.bVM == nil || req.PChainHeight == nil {
		blk, err = vm.vm.BuildBlock(ctx)
	} else {
		blk, err = vm.bVM.BuildBlockWithContext(ctx, &block.Context{
			PChainHeight: *req.PChainHeight,
		})
	}
	if err != nil {
		return nil, err
	}

	blkWithCtx, verifyWithCtx := blk.(block.WithVerifyContext)
	if verifyWithCtx {
		verifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	var (
		blkID    = blk.ID()
		parentID = blk.Parent()
	)
	return &vmpb.BuildBlockResponse{
		Id:                blkID[:],
		ParentId:          parentID[:],
		Bytes:             blk.Bytes(),
		Height:            blk.Height(),
		Timestamp:         grpcutils.TimestampFromTime(blk.Timestamp()),
		VerifyWithContext: verifyWithCtx,
	}, nil
}

func (vm *VMServer) ParseBlock(ctx context.Context, req *vmpb.ParseBlockRequest) (*vmpb.ParseBlockResponse, error) {
	blk, err := vm.vm.ParseBlock(ctx, req.Bytes)
	if err != nil {
		return nil, err
	}

	blkWithCtx, verifyWithCtx := blk.(block.WithVerifyContext)
	if verifyWithCtx {
		verifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	var (
		blkID    = blk.ID()
		parentID = blk.Parent()
	)
	return &vmpb.ParseBlockResponse{
		Id:                blkID[:],
		ParentId:          parentID[:],
		Height:            blk.Height(),
		Timestamp:         grpcutils.TimestampFromTime(blk.Timestamp()),
		VerifyWithContext: verifyWithCtx,
	}, nil
}

func (vm *VMServer) GetBlock(ctx context.Context, req *vmpb.GetBlockRequest) (*vmpb.GetBlockResponse, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(ctx, id)
	if err != nil {
		return &vmpb.GetBlockResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	blkWithCtx, verifyWithCtx := blk.(block.WithVerifyContext)
	if verifyWithCtx {
		verifyWithCtx, err = blkWithCtx.ShouldVerifyWithContext(ctx)
		if err != nil {
			return nil, err
		}
	}

	parentID := blk.Parent()
	return &vmpb.GetBlockResponse{
		ParentId:          parentID[:],
		Bytes:             blk.Bytes(),
		Height:            blk.Height(),
		Timestamp:         grpcutils.TimestampFromTime(blk.Timestamp()),
		VerifyWithContext: verifyWithCtx,
	}, nil
}

func (vm *VMServer) SetPreference(ctx context.Context, req *vmpb.SetPreferenceRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.SetPreference(ctx, id)
}

func (vm *VMServer) Health(ctx context.Context, _ *emptypb.Empty) (*vmpb.HealthResponse, error) {
	vmHealth, err := vm.vm.HealthCheck(ctx)
	if err != nil {
		return &vmpb.HealthResponse{}, err
	}
	dbHealth, err := vm.db.HealthCheck(ctx)
	if err != nil {
		return &vmpb.HealthResponse{}, err
	}
	report := map[string]interface{}{
		"database": dbHealth,
		"health":   vmHealth,
	}

	details, err := json.Marshal(report)
	return &vmpb.HealthResponse{
		Details: details,
	}, err
}

func (vm *VMServer) Version(ctx context.Context, _ *emptypb.Empty) (*vmpb.VersionResponse, error) {
	version, err := vm.vm.Version(ctx)
	return &vmpb.VersionResponse{
		Version: version,
	}, err
}

func (vm *VMServer) AppRequest(ctx context.Context, req *vmpb.AppRequestMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	deadline, err := grpcutils.TimestampAsTime(req.Deadline)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppRequest(ctx, nodeID, req.RequestId, deadline, req.Request)
}

func (vm *VMServer) AppRequestFailed(ctx context.Context, req *vmpb.AppRequestFailedMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}

	appErr := &common.AppError{
		Code:    req.ErrorCode,
		Message: req.ErrorMessage,
	}
	return &emptypb.Empty{}, vm.vm.AppRequestFailed(ctx, nodeID, req.RequestId, appErr)
}

func (vm *VMServer) AppResponse(ctx context.Context, req *vmpb.AppResponseMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppResponse(ctx, nodeID, req.RequestId, req.Response)
}

func (vm *VMServer) AppGossip(ctx context.Context, req *vmpb.AppGossipMsg) (*emptypb.Empty, error) {
	nodeID, err := ids.ToNodeID(req.NodeId)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, vm.vm.AppGossip(ctx, nodeID, req.Msg)
}

func (vm *VMServer) Gather(context.Context, *emptypb.Empty) (*vmpb.GatherResponse, error) {
	metrics, err := vm.metrics.Gather()
	return &vmpb.GatherResponse{MetricFamilies: metrics}, err
}

func (vm *VMServer) GetAncestors(ctx context.Context, req *vmpb.GetAncestorsRequest) (*vmpb.GetAncestorsResponse, error) {
	blkID, err := ids.ToID(req.BlkId)
	if err != nil {
		return nil, err
	}
	maxBlksNum := int(req.MaxBlocksNum)
	maxBlksSize := int(req.MaxBlocksSize)
	maxBlocksRetrivalTime := time.Duration(req.MaxBlocksRetrivalTime)

	blocks, err := block.GetAncestors(
		ctx,
		vm.log,
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

func (vm *VMServer) GetBlockIDAtHeight(
	ctx context.Context,
	req *vmpb.GetBlockIDAtHeightRequest,
) (*vmpb.GetBlockIDAtHeightResponse, error) {
	blkID, err := vm.vm.GetBlockIDAtHeight(ctx, req.Height)
	return &vmpb.GetBlockIDAtHeightResponse{
		BlkId: blkID[:],
		Err:   errorToErrEnum[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) StateSyncEnabled(ctx context.Context, _ *emptypb.Empty) (*vmpb.StateSyncEnabledResponse, error) {
	var (
		enabled bool
		err     error
	)
	if vm.ssVM != nil {
		enabled, err = vm.ssVM.StateSyncEnabled(ctx)
	}

	return &vmpb.StateSyncEnabledResponse{
		Enabled: enabled,
		Err:     errorToErrEnum[err],
	}, errorToRPCError(err)
}

func (vm *VMServer) GetOngoingSyncStateSummary(
	ctx context.Context,
	_ *emptypb.Empty,
) (*vmpb.GetOngoingSyncStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetOngoingSyncStateSummary(ctx)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetOngoingSyncStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetOngoingSyncStateSummaryResponse{
		Id:     summaryID[:],
		Height: summary.Height(),
		Bytes:  summary.Bytes(),
	}, nil
}

func (vm *VMServer) GetLastStateSummary(ctx context.Context, _ *emptypb.Empty) (*vmpb.GetLastStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetLastStateSummary(ctx)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetLastStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetLastStateSummaryResponse{
		Id:     summaryID[:],
		Height: summary.Height(),
		Bytes:  summary.Bytes(),
	}, nil
}

func (vm *VMServer) ParseStateSummary(
	ctx context.Context,
	req *vmpb.ParseStateSummaryRequest,
) (*vmpb.ParseStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.ParseStateSummary(ctx, req.Bytes)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.ParseStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.ParseStateSummaryResponse{
		Id:     summaryID[:],
		Height: summary.Height(),
	}, nil
}

func (vm *VMServer) GetStateSummary(
	ctx context.Context,
	req *vmpb.GetStateSummaryRequest,
) (*vmpb.GetStateSummaryResponse, error) {
	var (
		summary block.StateSummary
		err     error
	)
	if vm.ssVM != nil {
		summary, err = vm.ssVM.GetStateSummary(ctx, req.Height)
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	if err != nil {
		return &vmpb.GetStateSummaryResponse{
			Err: errorToErrEnum[err],
		}, errorToRPCError(err)
	}

	summaryID := summary.ID()
	return &vmpb.GetStateSummaryResponse{
		Id:    summaryID[:],
		Bytes: summary.Bytes(),
	}, nil
}

func (vm *VMServer) BlockVerify(ctx context.Context, req *vmpb.BlockVerifyRequest) (*vmpb.BlockVerifyResponse, error) {
	blk, err := vm.vm.ParseBlock(ctx, req.Bytes)
	if err != nil {
		return nil, err
	}

	if req.PChainHeight == nil {
		err = blk.Verify(ctx)
	} else {
		blkWithCtx, ok := blk.(block.WithVerifyContext)
		if !ok {
			return nil, fmt.Errorf("%w but got %T", errExpectedBlockWithVerifyContext, blk)
		}
		blockCtx := &block.Context{
			PChainHeight: *req.PChainHeight,
		}
		err = blkWithCtx.VerifyWithContext(ctx, blockCtx)
	}
	if err != nil {
		return nil, err
	}

	return &vmpb.BlockVerifyResponse{
		Timestamp: grpcutils.TimestampFromTime(blk.Timestamp()),
	}, nil
}

func (vm *VMServer) BlockAccept(ctx context.Context, req *vmpb.BlockAcceptRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := blk.Accept(ctx); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) BlockReject(ctx context.Context, req *vmpb.BlockRejectRequest) (*emptypb.Empty, error) {
	id, err := ids.ToID(req.Id)
	if err != nil {
		return nil, err
	}
	blk, err := vm.vm.GetBlock(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := blk.Reject(ctx); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (vm *VMServer) StateSummaryAccept(
	ctx context.Context,
	req *vmpb.StateSummaryAcceptRequest,
) (*vmpb.StateSummaryAcceptResponse, error) {
	var (
		mode = block.StateSyncSkipped
		err  error
	)
	if vm.ssVM != nil {
		var summary block.StateSummary
		summary, err = vm.ssVM.ParseStateSummary(ctx, req.Bytes)
		if err == nil {
			mode, err = summary.Accept(ctx)
		}
	} else {
		err = block.ErrStateSyncableVMNotImplemented
	}

	return &vmpb.StateSummaryAcceptResponse{
		Mode: vmpb.StateSummaryAcceptResponse_Mode(mode),
		Err:  errorToErrEnum[err],
	}, errorToRPCError(err)
}

func convertNetworkUpgrades(pbUpgrades *vmpb.NetworkUpgrades) (upgrade.Config, error) {
	if pbUpgrades == nil {
		return upgrade.Config{}, errNilNetworkUpgradesPB
	}

	ap1, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhase_1Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	ap2, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhase_2Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	ap3, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhase_3Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	ap4, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhase_4Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	ap5, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhase_5Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	apPre6, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhasePre_6Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	ap6, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhase_6Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	apPost6, err := grpcutils.TimestampAsTime(pbUpgrades.ApricotPhasePost_6Time)
	if err != nil {
		return upgrade.Config{}, err
	}
	banff, err := grpcutils.TimestampAsTime(pbUpgrades.BanffTime)
	if err != nil {
		return upgrade.Config{}, err
	}
	cortina, err := grpcutils.TimestampAsTime(pbUpgrades.CortinaTime)
	if err != nil {
		return upgrade.Config{}, err
	}
	cortinaXChainStopVertexID, err := ids.ToID(pbUpgrades.CortinaXChainStopVertexId)
	if err != nil {
		return upgrade.Config{}, err
	}
	durango, err := grpcutils.TimestampAsTime(pbUpgrades.DurangoTime)
	if err != nil {
		return upgrade.Config{}, err
	}
	etna, err := grpcutils.TimestampAsTime(pbUpgrades.EtnaTime)
	if err != nil {
		return upgrade.Config{}, err
	}
	fortuna, err := grpcutils.TimestampAsTime(pbUpgrades.FortunaTime)
	if err != nil {
		return upgrade.Config{}, err
	}
	granite, err := grpcutils.TimestampAsTime(pbUpgrades.GraniteTime)
	if err != nil {
		return upgrade.Config{}, err
	}
	graniteEpochDuration := pbUpgrades.GraniteEpochDuration.AsDuration()
	helicon, err := grpcutils.TimestampAsTime(pbUpgrades.HeliconTime)
	if err != nil {
		return upgrade.Config{}, err
	}

	return upgrade.Config{
		ApricotPhase1Time:            ap1,
		ApricotPhase2Time:            ap2,
		ApricotPhase3Time:            ap3,
		ApricotPhase4Time:            ap4,
		ApricotPhase4MinPChainHeight: pbUpgrades.ApricotPhase_4MinPChainHeight,
		ApricotPhase5Time:            ap5,
		ApricotPhasePre6Time:         apPre6,
		ApricotPhase6Time:            ap6,
		ApricotPhasePost6Time:        apPost6,
		BanffTime:                    banff,
		CortinaTime:                  cortina,
		CortinaXChainStopVertexID:    cortinaXChainStopVertexID,
		DurangoTime:                  durango,
		EtnaTime:                     etna,
		FortunaTime:                  fortuna,
		GraniteTime:                  granite,
		GraniteEpochDuration:         graniteEpochDuration,
		HeliconTime:                  helicon,
	}, nil
}

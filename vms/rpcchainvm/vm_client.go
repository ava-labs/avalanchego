// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory"
	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ids/galiasreader"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/appsender"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/snow/validators/gvalidators"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/chain"
	"github.com/ava-labs/avalanchego/vms/platformvm/warp/gwarp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/ghttp"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/grpcutils"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm/runtime"

	aliasreaderpb "github.com/ava-labs/avalanchego/buf/proto/pb/aliasreader"
	appsenderpb "github.com/ava-labs/avalanchego/buf/proto/pb/appsender"
	httppb "github.com/ava-labs/avalanchego/buf/proto/pb/http"
	rpcdbpb "github.com/ava-labs/avalanchego/buf/proto/pb/rpcdb"
	sharedmemorypb "github.com/ava-labs/avalanchego/buf/proto/pb/sharedmemory"
	validatorstatepb "github.com/ava-labs/avalanchego/buf/proto/pb/validatorstate"
	vmpb "github.com/ava-labs/avalanchego/buf/proto/pb/vm"
	warppb "github.com/ava-labs/avalanchego/buf/proto/pb/warp"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	dto "github.com/prometheus/client_model/go"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// TODO: Enable these to be configured by the user
const (
	decidedCacheSize    = 64 * units.MiB
	missingCacheSize    = 2048
	unverifiedCacheSize = 64 * units.MiB
	bytesToIDCacheSize  = 64 * units.MiB
)

var (
	errUnsupportedFXs                       = errors.New("unsupported feature extensions")
	errBatchedParseBlockWrongNumberOfBlocks = errors.New("BatchedParseBlock returned different number of blocks than expected")

	_ block.ChainVM                      = (*VMClient)(nil)
	_ block.BuildBlockWithContextChainVM = (*VMClient)(nil)
	_ block.BatchedChainVM               = (*VMClient)(nil)
	_ block.StateSyncableVM              = (*VMClient)(nil)
	_ prometheus.Gatherer                = (*VMClient)(nil)

	_ snowman.Block           = (*blockClient)(nil)
	_ block.WithVerifyContext = (*blockClient)(nil)

	_ block.StateSummary = (*summaryClient)(nil)
)

// VMClient is an implementation of a VM that talks over RPC.
type VMClient struct {
	*chain.State
	logger          logging.Logger
	client          vmpb.VMClient
	runtime         runtime.Stopper
	pid             int
	processTracker  resource.ProcessTracker
	metricsGatherer metrics.MultiGatherer

	sharedMemory         *gsharedmemory.Server
	bcLookup             *galiasreader.Server
	appSender            *appsender.Server
	validatorStateServer *gvalidators.Server
	warpSignerServer     *gwarp.Server

	serverCloser grpcutils.ServerCloser
	conns        []*grpc.ClientConn

	grpcServerMetrics *grpc_prometheus.ServerMetrics
}

// NewClient returns a VM connected to a remote VM
func NewClient(
	clientConn *grpc.ClientConn,
	runtime runtime.Stopper,
	pid int,
	processTracker resource.ProcessTracker,
	metricsGatherer metrics.MultiGatherer,
	logger logging.Logger,
) *VMClient {
	return &VMClient{
		client:          vmpb.NewVMClient(clientConn),
		runtime:         runtime,
		pid:             pid,
		processTracker:  processTracker,
		metricsGatherer: metricsGatherer,
		conns:           []*grpc.ClientConn{clientConn},
		logger:          logger,
	}
}

func (vm *VMClient) Initialize(
	ctx context.Context,
	chainCtx *snow.Context,
	db database.Database,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	if len(fxs) != 0 {
		return errUnsupportedFXs
	}

	primaryAlias, err := chainCtx.BCLookup.PrimaryAlias(chainCtx.ChainID)
	if err != nil {
		// If fetching the alias fails, we default to the chain's ID
		primaryAlias = chainCtx.ChainID.String()
	}

	// Register metrics
	serverReg, err := metrics.MakeAndRegister(
		vm.metricsGatherer,
		primaryAlias,
	)
	if err != nil {
		return err
	}
	vm.grpcServerMetrics = grpc_prometheus.NewServerMetrics()
	if err := serverReg.Register(vm.grpcServerMetrics); err != nil {
		return err
	}

	// Initialize the database
	dbServerListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	dbServerAddr := dbServerListener.Addr().String()

	go grpcutils.Serve(dbServerListener, vm.newDBServer(db))
	chainCtx.Log.Info("grpc: serving database",
		zap.String("address", dbServerAddr),
	)

	vm.sharedMemory = gsharedmemory.NewServer(chainCtx.SharedMemory, db)
	vm.bcLookup = galiasreader.NewServer(chainCtx.BCLookup)
	vm.appSender = appsender.NewServer(appSender)
	vm.validatorStateServer = gvalidators.NewServer(chainCtx.ValidatorState)
	vm.warpSignerServer = gwarp.NewServer(chainCtx.WarpSigner)

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	serverAddr := serverListener.Addr().String()

	go grpcutils.Serve(serverListener, vm.newInitServer())
	chainCtx.Log.Info("grpc: serving vm services",
		zap.String("address", serverAddr),
	)

	networkUpgrades := &vmpb.NetworkUpgrades{
		ApricotPhase_1Time:            grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhase1Time),
		ApricotPhase_2Time:            grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhase2Time),
		ApricotPhase_3Time:            grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhase3Time),
		ApricotPhase_4Time:            grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhase4Time),
		ApricotPhase_4MinPChainHeight: chainCtx.NetworkUpgrades.ApricotPhase4MinPChainHeight,
		ApricotPhase_5Time:            grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhase5Time),
		ApricotPhasePre_6Time:         grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhasePre6Time),
		ApricotPhase_6Time:            grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhase6Time),
		ApricotPhasePost_6Time:        grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.ApricotPhasePost6Time),
		BanffTime:                     grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.BanffTime),
		CortinaTime:                   grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.CortinaTime),
		CortinaXChainStopVertexId:     chainCtx.NetworkUpgrades.CortinaXChainStopVertexID[:],
		DurangoTime:                   grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.DurangoTime),
		EtnaTime:                      grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.EtnaTime),
		FortunaTime:                   grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.FortunaTime),
		GraniteTime:                   grpcutils.TimestampFromTime(chainCtx.NetworkUpgrades.GraniteTime),
	}

	resp, err := vm.client.Initialize(ctx, &vmpb.InitializeRequest{
		NetworkId:       chainCtx.NetworkID,
		SubnetId:        chainCtx.SubnetID[:],
		ChainId:         chainCtx.ChainID[:],
		NodeId:          chainCtx.NodeID.Bytes(),
		PublicKey:       bls.PublicKeyToCompressedBytes(chainCtx.PublicKey),
		NetworkUpgrades: networkUpgrades,
		XChainId:        chainCtx.XChainID[:],
		CChainId:        chainCtx.CChainID[:],
		AvaxAssetId:     chainCtx.AVAXAssetID[:],
		ChainDataDir:    chainCtx.ChainDataDir,
		GenesisBytes:    genesisBytes,
		UpgradeBytes:    upgradeBytes,
		ConfigBytes:     configBytes,
		DbServerAddr:    dbServerAddr,
		ServerAddr:      serverAddr,
	})
	if err != nil {
		return err
	}

	if err := chainCtx.Metrics.Register("", vm); err != nil {
		return err
	}

	id, err := ids.ToID(resp.LastAcceptedId)
	if err != nil {
		return err
	}
	parentID, err := ids.ToID(resp.LastAcceptedParentId)
	if err != nil {
		return err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return err
	}

	// We don't need to check whether this is a block.WithVerifyContext because
	// we'll never Verify this block.
	lastAcceptedBlk := &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	}

	vm.State, err = chain.NewMeteredState(
		serverReg,
		&chain.Config{
			DecidedCacheSize:      decidedCacheSize,
			MissingCacheSize:      missingCacheSize,
			UnverifiedCacheSize:   unverifiedCacheSize,
			BytesToIDCacheSize:    bytesToIDCacheSize,
			LastAcceptedBlock:     lastAcceptedBlk,
			GetBlock:              vm.getBlock,
			UnmarshalBlock:        vm.parseBlock,
			BatchedUnmarshalBlock: vm.batchedParseBlock,
			BuildBlock:            vm.buildBlock,
			BuildBlockWithContext: vm.buildBlockWithContext,
		},
	)
	return err
}

func (vm *VMClient) newDBServer(db database.Database) *grpc.Server {
	server := grpcutils.NewServer(
		grpcutils.WithUnaryInterceptor(vm.grpcServerMetrics.UnaryServerInterceptor()),
		grpcutils.WithStreamInterceptor(vm.grpcServerMetrics.StreamServerInterceptor()),
	)

	// See https://github.com/grpc/grpc/blob/master/doc/health-checking.md
	grpcHealth := health.NewServer()
	grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	vm.serverCloser.Add(server)

	// Register services
	rpcdbpb.RegisterDatabaseServer(server, rpcdb.NewServer(db))
	healthpb.RegisterHealthServer(server, grpcHealth)

	// Ensure metric counters are zeroed on restart
	grpc_prometheus.Register(server)

	return server
}

func (vm *VMClient) newInitServer() *grpc.Server {
	server := grpcutils.NewServer(
		grpcutils.WithUnaryInterceptor(vm.grpcServerMetrics.UnaryServerInterceptor()),
		grpcutils.WithStreamInterceptor(vm.grpcServerMetrics.StreamServerInterceptor()),
	)

	// See https://github.com/grpc/grpc/blob/master/doc/health-checking.md
	grpcHealth := health.NewServer()
	grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	vm.serverCloser.Add(server)

	// Register services
	sharedmemorypb.RegisterSharedMemoryServer(server, vm.sharedMemory)
	aliasreaderpb.RegisterAliasReaderServer(server, vm.bcLookup)
	appsenderpb.RegisterAppSenderServer(server, vm.appSender)
	healthpb.RegisterHealthServer(server, grpcHealth)
	validatorstatepb.RegisterValidatorStateServer(server, vm.validatorStateServer)
	warppb.RegisterSignerServer(server, vm.warpSignerServer)

	// Ensure metric counters are zeroed on restart
	grpc_prometheus.Register(server)

	return server
}

func (vm *VMClient) SetState(ctx context.Context, state snow.State) error {
	resp, err := vm.client.SetState(ctx, &vmpb.SetStateRequest{
		State: vmpb.State(state),
	})
	if err != nil {
		return err
	}

	id, err := ids.ToID(resp.LastAcceptedId)
	if err != nil {
		return err
	}

	parentID, err := ids.ToID(resp.LastAcceptedParentId)
	if err != nil {
		return err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return err
	}

	// We don't need to check whether this is a block.WithVerifyContext because
	// we'll never Verify this block.
	return vm.State.SetLastAcceptedBlock(&blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	})
}

func (vm *VMClient) Shutdown(ctx context.Context) error {
	errs := wrappers.Errs{}
	_, err := vm.client.Shutdown(ctx, &emptypb.Empty{})
	errs.Add(err)

	vm.serverCloser.Stop()
	for _, conn := range vm.conns {
		errs.Add(conn.Close())
	}

	vm.runtime.Stop(ctx)

	vm.processTracker.UntrackProcess(vm.pid)
	return errs.Err
}

func (vm *VMClient) CreateHandlers(ctx context.Context) (map[string]http.Handler, error) {
	resp, err := vm.client.CreateHandlers(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	handlers := make(map[string]http.Handler, len(resp.Handlers))
	for _, handler := range resp.Handlers {
		clientConn, err := grpcutils.Dial(handler.ServerAddr)
		if err != nil {
			return nil, err
		}

		vm.conns = append(vm.conns, clientConn)
		handlers[handler.Prefix] = ghttp.NewClient(httppb.NewHTTPClient(clientConn))
	}
	return handlers, nil
}

func (vm *VMClient) NewHTTPHandler(ctx context.Context) (http.Handler, error) {
	resp, err := vm.client.NewHTTPHandler(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}

	if resp.ServerAddr == "" {
		return nil, nil
	}

	clientConn, err := grpcutils.Dial(resp.ServerAddr)
	if err != nil {
		return nil, err
	}

	vm.conns = append(vm.conns, clientConn)
	return ghttp.NewClient(httppb.NewHTTPClient(clientConn)), nil
}

func (vm *VMClient) WaitForEvent(ctx context.Context) (common.Message, error) {
	resp, err := vm.client.WaitForEvent(ctx, &emptypb.Empty{})
	if err != nil {
		vm.logger.Debug("failed to subscribe to events", zap.Error(err))
		return 0, err
	}
	return common.Message(resp.Message), nil
}

func (vm *VMClient) Connected(ctx context.Context, nodeID ids.NodeID, nodeVersion *version.Application) error {
	_, err := vm.client.Connected(ctx, &vmpb.ConnectedRequest{
		NodeId: nodeID.Bytes(),
		Name:   nodeVersion.Name,
		Major:  uint32(nodeVersion.Major),
		Minor:  uint32(nodeVersion.Minor),
		Patch:  uint32(nodeVersion.Patch),
	})
	return err
}

func (vm *VMClient) Disconnected(ctx context.Context, nodeID ids.NodeID) error {
	_, err := vm.client.Disconnected(ctx, &vmpb.DisconnectedRequest{
		NodeId: nodeID.Bytes(),
	})
	return err
}

// If the underlying VM doesn't actually implement this method, its [BuildBlock]
// method will be called instead.
func (vm *VMClient) buildBlockWithContext(ctx context.Context, blockCtx *block.Context) (snowman.Block, error) {
	resp, err := vm.client.BuildBlock(ctx, &vmpb.BuildBlockRequest{
		PChainHeight: &blockCtx.PChainHeight,
	})
	if err != nil {
		return nil, err
	}
	return vm.newBlockFromBuildBlock(resp)
}

func (vm *VMClient) buildBlock(ctx context.Context) (snowman.Block, error) {
	resp, err := vm.client.BuildBlock(ctx, &vmpb.BuildBlockRequest{})
	if err != nil {
		return nil, err
	}
	return vm.newBlockFromBuildBlock(resp)
}

func (vm *VMClient) parseBlock(ctx context.Context, bytes []byte) (snowman.Block, error) {
	resp, err := vm.client.ParseBlock(ctx, &vmpb.ParseBlockRequest{
		Bytes: bytes,
	})
	if err != nil {
		return nil, err
	}

	id, err := ids.ToID(resp.Id)
	if err != nil {
		return nil, err
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	if err != nil {
		return nil, err
	}
	return &blockClient{
		vm:                  vm,
		id:                  id,
		parentID:            parentID,
		bytes:               bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}, nil
}

func (vm *VMClient) getBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	resp, err := vm.client.GetBlock(ctx, &vmpb.GetBlockRequest{
		Id: blkID[:],
	})
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	return &blockClient{
		vm:                  vm,
		id:                  blkID,
		parentID:            parentID,
		bytes:               resp.Bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}, err
}

func (vm *VMClient) SetPreference(ctx context.Context, blkID ids.ID) error {
	_, err := vm.client.SetPreference(ctx, &vmpb.SetPreferenceRequest{
		Id: blkID[:],
	})
	return err
}

func (vm *VMClient) HealthCheck(ctx context.Context) (interface{}, error) {
	// HealthCheck is a special case, where we want to fail fast instead of block.
	failFast := grpc.WaitForReady(false)
	health, err := vm.client.Health(ctx, &emptypb.Empty{}, failFast)
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	return json.RawMessage(health.Details), nil
}

func (vm *VMClient) Version(ctx context.Context) (string, error) {
	resp, err := vm.client.Version(ctx, &emptypb.Empty{})
	if err != nil {
		return "", err
	}
	return resp.Version, nil
}

func (vm *VMClient) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	_, err := vm.client.AppRequest(
		ctx,
		&vmpb.AppRequestMsg{
			NodeId:    nodeID.Bytes(),
			RequestId: requestID,
			Request:   request,
			Deadline:  grpcutils.TimestampFromTime(deadline),
		},
	)
	return err
}

func (vm *VMClient) AppResponse(ctx context.Context, nodeID ids.NodeID, requestID uint32, response []byte) error {
	_, err := vm.client.AppResponse(
		ctx,
		&vmpb.AppResponseMsg{
			NodeId:    nodeID.Bytes(),
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (vm *VMClient) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32, appErr *common.AppError) error {
	msg := &vmpb.AppRequestFailedMsg{
		NodeId:       nodeID.Bytes(),
		RequestId:    requestID,
		ErrorCode:    appErr.Code,
		ErrorMessage: appErr.Message,
	}

	_, err := vm.client.AppRequestFailed(ctx, msg)
	return err
}

func (vm *VMClient) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	_, err := vm.client.AppGossip(
		ctx,
		&vmpb.AppGossipMsg{
			NodeId: nodeID.Bytes(),
			Msg:    msg,
		},
	)
	return err
}

func (vm *VMClient) Gather() ([]*dto.MetricFamily, error) {
	resp, err := vm.client.Gather(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.MetricFamilies, nil
}

func (vm *VMClient) GetAncestors(
	ctx context.Context,
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	resp, err := vm.client.GetAncestors(ctx, &vmpb.GetAncestorsRequest{
		BlkId:                 blkID[:],
		MaxBlocksNum:          int32(maxBlocksNum),
		MaxBlocksSize:         int32(maxBlocksSize),
		MaxBlocksRetrivalTime: int64(maxBlocksRetrivalTime),
	})
	if err != nil {
		return nil, err
	}
	return resp.BlksBytes, nil
}

func (vm *VMClient) batchedParseBlock(ctx context.Context, blksBytes [][]byte) ([]snowman.Block, error) {
	resp, err := vm.client.BatchedParseBlock(ctx, &vmpb.BatchedParseBlockRequest{
		Request: blksBytes,
	})
	if err != nil {
		return nil, err
	}
	if len(blksBytes) != len(resp.Response) {
		return nil, errBatchedParseBlockWrongNumberOfBlocks
	}

	res := make([]snowman.Block, 0, len(blksBytes))
	for idx, blkResp := range resp.Response {
		id, err := ids.ToID(blkResp.Id)
		if err != nil {
			return nil, err
		}

		parentID, err := ids.ToID(blkResp.ParentId)
		if err != nil {
			return nil, err
		}

		time, err := grpcutils.TimestampAsTime(blkResp.Timestamp)
		if err != nil {
			return nil, err
		}

		res = append(res, &blockClient{
			vm:                  vm,
			id:                  id,
			parentID:            parentID,
			bytes:               blksBytes[idx],
			height:              blkResp.Height,
			time:                time,
			shouldVerifyWithCtx: blkResp.VerifyWithContext,
		})
	}

	return res, nil
}

func (vm *VMClient) GetBlockIDAtHeight(ctx context.Context, height uint64) (ids.ID, error) {
	resp, err := vm.client.GetBlockIDAtHeight(
		ctx,
		&vmpb.GetBlockIDAtHeightRequest{Height: height},
	)
	if err != nil {
		return ids.Empty, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return ids.Empty, errEnumToError[errEnum]
	}
	return ids.ToID(resp.BlkId)
}

func (vm *VMClient) StateSyncEnabled(ctx context.Context) (bool, error) {
	resp, err := vm.client.StateSyncEnabled(ctx, &emptypb.Empty{})
	if err != nil {
		return false, err
	}
	err = errEnumToError[resp.Err]
	if err == block.ErrStateSyncableVMNotImplemented {
		return false, nil
	}
	return resp.Enabled, err
}

func (vm *VMClient) GetOngoingSyncStateSummary(ctx context.Context) (block.StateSummary, error) {
	resp, err := vm.client.GetOngoingSyncStateSummary(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) GetLastStateSummary(ctx context.Context) (block.StateSummary, error) {
	resp, err := vm.client.GetLastStateSummary(ctx, &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) ParseStateSummary(ctx context.Context, summaryBytes []byte) (block.StateSummary, error) {
	resp, err := vm.client.ParseStateSummary(
		ctx,
		&vmpb.ParseStateSummaryRequest{
			Bytes: summaryBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  summaryBytes,
	}, err
}

func (vm *VMClient) GetStateSummary(ctx context.Context, summaryHeight uint64) (block.StateSummary, error) {
	resp, err := vm.client.GetStateSummary(
		ctx,
		&vmpb.GetStateSummaryRequest{
			Height: summaryHeight,
		},
	)
	if err != nil {
		return nil, err
	}
	if errEnum := resp.Err; errEnum != vmpb.Error_ERROR_UNSPECIFIED {
		return nil, errEnumToError[errEnum]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: summaryHeight,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) newBlockFromBuildBlock(resp *vmpb.BuildBlockResponse) (*blockClient, error) {
	id, err := ids.ToID(resp.Id)
	if err != nil {
		return nil, err
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	return &blockClient{
		vm:                  vm,
		id:                  id,
		parentID:            parentID,
		bytes:               resp.Bytes,
		height:              resp.Height,
		time:                time,
		shouldVerifyWithCtx: resp.VerifyWithContext,
	}, err
}

type blockClient struct {
	vm *VMClient

	id                  ids.ID
	parentID            ids.ID
	bytes               []byte
	height              uint64
	time                time.Time
	shouldVerifyWithCtx bool
}

func (b *blockClient) ID() ids.ID {
	return b.id
}

func (b *blockClient) Accept(ctx context.Context) error {
	_, err := b.vm.client.BlockAccept(ctx, &vmpb.BlockAcceptRequest{
		Id: b.id[:],
	})
	return err
}

func (b *blockClient) Reject(ctx context.Context) error {
	_, err := b.vm.client.BlockReject(ctx, &vmpb.BlockRejectRequest{
		Id: b.id[:],
	})
	return err
}

func (b *blockClient) Parent() ids.ID {
	return b.parentID
}

func (b *blockClient) Verify(ctx context.Context) error {
	resp, err := b.vm.client.BlockVerify(ctx, &vmpb.BlockVerifyRequest{
		Bytes: b.bytes,
	})
	if err != nil {
		return err
	}

	b.time, err = grpcutils.TimestampAsTime(resp.Timestamp)
	return err
}

func (b *blockClient) Bytes() []byte {
	return b.bytes
}

func (b *blockClient) Height() uint64 {
	return b.height
}

func (b *blockClient) Timestamp() time.Time {
	return b.time
}

func (b *blockClient) ShouldVerifyWithContext(context.Context) (bool, error) {
	return b.shouldVerifyWithCtx, nil
}

func (b *blockClient) VerifyWithContext(ctx context.Context, blockCtx *block.Context) error {
	resp, err := b.vm.client.BlockVerify(ctx, &vmpb.BlockVerifyRequest{
		Bytes:        b.bytes,
		PChainHeight: &blockCtx.PChainHeight,
	})
	if err != nil {
		return err
	}

	b.time, err = grpcutils.TimestampAsTime(resp.Timestamp)
	return err
}

type summaryClient struct {
	vm *VMClient

	id     ids.ID
	height uint64
	bytes  []byte
}

func (s *summaryClient) ID() ids.ID {
	return s.id
}

func (s *summaryClient) Height() uint64 {
	return s.height
}

func (s *summaryClient) Bytes() []byte {
	return s.bytes
}

func (s *summaryClient) Accept(ctx context.Context) (block.StateSyncMode, error) {
	resp, err := s.vm.client.StateSummaryAccept(
		ctx,
		&vmpb.StateSummaryAcceptRequest{
			Bytes: s.bytes,
		},
	)
	if err != nil {
		return block.StateSyncSkipped, err
	}
	return block.StateSyncMode(resp.Mode), errEnumToError[resp.Err]
}

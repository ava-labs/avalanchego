// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"

	"github.com/hashicorp/go-plugin"

	"github.com/prometheus/client_golang/prometheus"

	dto "github.com/prometheus/client_model/go"

	"go.uber.org/zap"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/protobuf/types/known/emptypb"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"github.com/ava-labs/avalanchego/api/keystore/gkeystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/chains/atomic/gsharedmemory"
	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/database/rpcdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ids/galiasreader"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/common/appsender"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/resource"
	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms/components/chain"
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

const (
	decidedCacheSize    = 2048
	missingCacheSize    = 2048
	unverifiedCacheSize = 2048
	bytesToIDCacheSize  = 2048
)

var (
	errUnsupportedFXs                       = errors.New("unsupported feature extensions")
	errBatchedParseBlockWrongNumberOfBlocks = errors.New("BatchedParseBlock returned different number of blocks than expected")

	_ block.ChainVM              = (*VMClient)(nil)
	_ block.BatchedChainVM       = (*VMClient)(nil)
	_ block.HeightIndexedChainVM = (*VMClient)(nil)
	_ block.StateSyncableVM      = (*VMClient)(nil)
	_ prometheus.Gatherer        = (*VMClient)(nil)

	_ snowman.Block = (*blockClient)(nil)

	_ block.StateSummary = (*summaryClient)(nil)
)

// VMClient is an implementation of a VM that talks over RPC.
type VMClient struct {
	*chain.State
	client         vmpb.VMClient
	proc           *plugin.Client
	pid            int
	processTracker resource.ProcessTracker

	messenger    *messenger.Server
	keystore     *gkeystore.Server
	sharedMemory *gsharedmemory.Server
	bcLookup     *galiasreader.Server
	snLookup     *gsubnetlookup.Server
	appSender    *appsender.Server

	serverCloser grpcutils.ServerCloser
	conns        []*grpc.ClientConn

	grpcServerMetrics *grpc_prometheus.ServerMetrics

	ctx *snow.Context
}

// NewClient returns a VM connected to a remote VM
func NewClient(client vmpb.VMClient) *VMClient {
	return &VMClient{
		client: client,
	}
}

// SetProcess gives ownership of the server process to the client.
func (vm *VMClient) SetProcess(ctx *snow.Context, proc *plugin.Client, processTracker resource.ProcessTracker) {
	vm.ctx = ctx
	vm.proc = proc
	vm.processTracker = processTracker
	vm.pid = proc.ReattachConfig().Pid
	processTracker.TrackProcess(vm.pid)
}

func (vm *VMClient) Initialize(
	ctx *snow.Context,
	dbManager manager.Manager,
	genesisBytes []byte,
	upgradeBytes []byte,
	configBytes []byte,
	toEngine chan<- common.Message,
	fxs []*common.Fx,
	appSender common.AppSender,
) error {
	if len(fxs) != 0 {
		return errUnsupportedFXs
	}

	vm.ctx = ctx

	// Register metrics
	registerer := prometheus.NewRegistry()
	multiGatherer := metrics.NewMultiGatherer()
	vm.grpcServerMetrics = grpc_prometheus.NewServerMetrics()
	if err := registerer.Register(vm.grpcServerMetrics); err != nil {
		return err
	}
	if err := multiGatherer.Register("rpcchainvm", registerer); err != nil {
		return err
	}
	if err := multiGatherer.Register("", vm); err != nil {
		return err
	}

	// Initialize and serve each database and construct the db manager
	// initialize request parameters
	versionedDBs := dbManager.GetDatabases()
	versionedDBServers := make([]*vmpb.VersionedDBServer, len(versionedDBs))
	for i, semDB := range versionedDBs {
		db := rpcdb.NewServer(semDB.Database)
		dbVersion := semDB.Version.String()
		serverListener, err := grpcutils.NewListener()
		if err != nil {
			return err
		}
		serverAddr := serverListener.Addr().String()

		go grpcutils.Serve(serverListener, vm.getDBServerFunc(db))
		vm.ctx.Log.Info("grpc: serving database",
			zap.String("version", dbVersion),
			zap.String("address", serverAddr),
		)

		versionedDBServers[i] = &vmpb.VersionedDBServer{
			ServerAddr: serverAddr,
			Version:    dbVersion,
		}
	}

	vm.messenger = messenger.NewServer(toEngine)
	vm.keystore = gkeystore.NewServer(ctx.Keystore)
	vm.sharedMemory = gsharedmemory.NewServer(ctx.SharedMemory, dbManager.Current().Database)
	vm.bcLookup = galiasreader.NewServer(ctx.BCLookup)
	vm.snLookup = gsubnetlookup.NewServer(ctx.SNLookup)
	vm.appSender = appsender.NewServer(appSender)

	serverListener, err := grpcutils.NewListener()
	if err != nil {
		return err
	}
	serverAddr := serverListener.Addr().String()

	go grpcutils.Serve(serverListener, vm.getInitServer)
	vm.ctx.Log.Info("grpc: serving vm services",
		zap.String("address", serverAddr),
	)

	resp, err := vm.client.Initialize(context.Background(), &vmpb.InitializeRequest{
		NetworkId:    ctx.NetworkID,
		SubnetId:     ctx.SubnetID[:],
		ChainId:      ctx.ChainID[:],
		NodeId:       ctx.NodeID.Bytes(),
		XChainId:     ctx.XChainID[:],
		AvaxAssetId:  ctx.AVAXAssetID[:],
		GenesisBytes: genesisBytes,
		UpgradeBytes: upgradeBytes,
		ConfigBytes:  configBytes,
		DbServers:    versionedDBServers,
		ServerAddr:   serverAddr,
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

	lastAcceptedBlk := &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Accepted,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	}

	chainState, err := chain.NewMeteredState(
		registerer,
		&chain.Config{
			DecidedCacheSize:    decidedCacheSize,
			MissingCacheSize:    missingCacheSize,
			UnverifiedCacheSize: unverifiedCacheSize,
			BytesToIDCacheSize:  bytesToIDCacheSize,
			LastAcceptedBlock:   lastAcceptedBlk,
			GetBlock:            vm.getBlock,
			UnmarshalBlock:      vm.parseBlock,
			BuildBlock:          vm.buildBlock,
		},
	)
	if err != nil {
		return err
	}
	vm.State = chainState

	return vm.ctx.Metrics.Register(multiGatherer)
}

func (vm *VMClient) getDBServerFunc(db rpcdbpb.DatabaseServer) func(opts []grpc.ServerOption) *grpc.Server { // #nolint
	return func(opts []grpc.ServerOption) *grpc.Server {
		if len(opts) == 0 {
			opts = append(opts, grpcutils.DefaultServerOptions...)
		}

		// Collect gRPC serving metrics
		opts = append(opts, grpc.UnaryInterceptor(vm.grpcServerMetrics.UnaryServerInterceptor()))
		opts = append(opts, grpc.StreamInterceptor(vm.grpcServerMetrics.StreamServerInterceptor()))

		server := grpc.NewServer(opts...)

		grpcHealth := health.NewServer()
		// The server should use an empty string as the key for server's overall
		// health status.
		// See https://github.com/grpc/grpc/blob/master/doc/health-checking.md
		grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

		vm.serverCloser.Add(server)

		// register database service
		rpcdbpb.RegisterDatabaseServer(server, db)
		// register health service
		healthpb.RegisterHealthServer(server, grpcHealth)

		// Ensure metric counters are zeroed on restart
		grpc_prometheus.Register(server)

		return server
	}
}

func (vm *VMClient) getInitServer(opts []grpc.ServerOption) *grpc.Server {
	if len(opts) == 0 {
		opts = append(opts, grpcutils.DefaultServerOptions...)
	}

	// Collect gRPC serving metrics
	opts = append(opts, grpc.UnaryInterceptor(vm.grpcServerMetrics.UnaryServerInterceptor()))
	opts = append(opts, grpc.StreamInterceptor(vm.grpcServerMetrics.StreamServerInterceptor()))

	server := grpc.NewServer(opts...)

	grpcHealth := health.NewServer()
	// The server should use an empty string as the key for server's overall
	// health status.
	// See https://github.com/grpc/grpc/blob/master/doc/health-checking.md
	grpcHealth.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	vm.serverCloser.Add(server)

	// register the messenger service
	messengerpb.RegisterMessengerServer(server, vm.messenger)
	// register the keystore service
	keystorepb.RegisterKeystoreServer(server, vm.keystore)
	// register the shared memory service
	sharedmemorypb.RegisterSharedMemoryServer(server, vm.sharedMemory)
	// register the blockchain alias service
	aliasreaderpb.RegisterAliasReaderServer(server, vm.bcLookup)
	// register the subnet alias service
	subnetlookuppb.RegisterSubnetLookupServer(server, vm.snLookup)
	// register the app sender service
	appsenderpb.RegisterAppSenderServer(server, vm.appSender)
	// register the health service
	healthpb.RegisterHealthServer(server, grpcHealth)

	// Ensure metric counters are zeroed on restart
	grpc_prometheus.Register(server)

	return server
}

func (vm *VMClient) SetState(state snow.State) error {
	resp, err := vm.client.SetState(context.Background(), &vmpb.SetStateRequest{
		State: uint32(state),
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

	return vm.State.SetLastAcceptedBlock(&blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Accepted,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	})
}

func (vm *VMClient) Shutdown() error {
	errs := wrappers.Errs{}
	_, err := vm.client.Shutdown(context.Background(), &emptypb.Empty{})
	errs.Add(err)

	vm.serverCloser.Stop()
	for _, conn := range vm.conns {
		errs.Add(conn.Close())
	}

	vm.proc.Kill()
	vm.processTracker.UntrackProcess(vm.pid)
	return errs.Err
}

func (vm *VMClient) CreateHandlers() (map[string]*common.HTTPHandler, error) {
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

func (vm *VMClient) CreateStaticHandlers() (map[string]*common.HTTPHandler, error) {
	resp, err := vm.client.CreateStaticHandlers(context.Background(), &emptypb.Empty{})
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

func (vm *VMClient) Connected(nodeID ids.NodeID, nodeVersion *version.Application) error {
	_, err := vm.client.Connected(context.Background(), &vmpb.ConnectedRequest{
		NodeId:  nodeID[:],
		Version: nodeVersion.String(),
	})
	return err
}

func (vm *VMClient) Disconnected(nodeID ids.NodeID) error {
	_, err := vm.client.Disconnected(context.Background(), &vmpb.DisconnectedRequest{
		NodeId: nodeID[:],
	})
	return err
}

func (vm *VMClient) buildBlock() (snowman.Block, error) {
	resp, err := vm.client.BuildBlock(context.Background(), &emptypb.Empty{})
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
	return &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Processing,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	}, err
}

func (vm *VMClient) parseBlock(bytes []byte) (snowman.Block, error) {
	resp, err := vm.client.ParseBlock(context.Background(), &vmpb.ParseBlockRequest{
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

	status := choices.Status(resp.Status)
	if err := status.Valid(); err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	return &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    bytes,
		height:   resp.Height,
		time:     time,
	}, err
}

func (vm *VMClient) getBlock(id ids.ID) (snowman.Block, error) {
	resp, err := vm.client.GetBlock(context.Background(), &vmpb.GetBlockRequest{
		Id: id[:],
	})
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	parentID, err := ids.ToID(resp.ParentId)
	if err != nil {
		return nil, err
	}

	status := choices.Status(resp.Status)
	if err := status.Valid(); err != nil {
		return nil, err
	}

	time, err := grpcutils.TimestampAsTime(resp.Timestamp)
	return &blockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     time,
	}, err
}

func (vm *VMClient) SetPreference(id ids.ID) error {
	_, err := vm.client.SetPreference(context.Background(), &vmpb.SetPreferenceRequest{
		Id: id[:],
	})
	return err
}

func (vm *VMClient) HealthCheck() (interface{}, error) {
	health, err := vm.client.Health(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}

	return json.RawMessage(health.Details), nil
}

func (vm *VMClient) Version() (string, error) {
	resp, err := vm.client.Version(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return "", err
	}
	return resp.Version, nil
}

func (vm *VMClient) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, request []byte) error {
	_, err := vm.client.CrossChainAppRequest(
		ctx,
		&vmpb.CrossChainAppRequestMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
			Deadline:  grpcutils.TimestampFromTime(deadline),
			Request:   request,
		},
	)
	return err
}

func (vm *VMClient) CrossChainAppRequestFailed(ctx context.Context, chainID ids.ID, requestID uint32) error {
	_, err := vm.client.CrossChainAppRequestFailed(
		ctx,
		&vmpb.CrossChainAppRequestFailedMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
		},
	)
	return err
}

func (vm *VMClient) CrossChainAppResponse(ctx context.Context, chainID ids.ID, requestID uint32, response []byte) error {
	_, err := vm.client.CrossChainAppResponse(
		ctx,
		&vmpb.CrossChainAppResponseMsg{
			ChainId:   chainID[:],
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (vm *VMClient) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, request []byte) error {
	_, err := vm.client.AppRequest(
		ctx,
		&vmpb.AppRequestMsg{
			NodeId:    nodeID[:],
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
			NodeId:    nodeID[:],
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (vm *VMClient) AppRequestFailed(ctx context.Context, nodeID ids.NodeID, requestID uint32) error {
	_, err := vm.client.AppRequestFailed(
		ctx,
		&vmpb.AppRequestFailedMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
		},
	)
	return err
}

func (vm *VMClient) AppGossip(ctx context.Context, nodeID ids.NodeID, msg []byte) error {
	_, err := vm.client.AppGossip(
		ctx,
		&vmpb.AppGossipMsg{
			NodeId: nodeID[:],
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
	blkID ids.ID,
	maxBlocksNum int,
	maxBlocksSize int,
	maxBlocksRetrivalTime time.Duration,
) ([][]byte, error) {
	resp, err := vm.client.GetAncestors(context.Background(), &vmpb.GetAncestorsRequest{
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

func (vm *VMClient) BatchedParseBlock(blksBytes [][]byte) ([]snowman.Block, error) {
	resp, err := vm.client.BatchedParseBlock(context.Background(), &vmpb.BatchedParseBlockRequest{
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

		status := choices.Status(blkResp.Status)
		if err := status.Valid(); err != nil {
			return nil, err
		}

		time, err := grpcutils.TimestampAsTime(blkResp.Timestamp)
		if err != nil {
			return nil, err
		}

		res = append(res, &blockClient{
			vm:       vm,
			id:       id,
			parentID: parentID,
			status:   status,
			bytes:    blksBytes[idx],
			height:   blkResp.Height,
			time:     time,
		})
	}

	return res, nil
}

func (vm *VMClient) VerifyHeightIndex() error {
	resp, err := vm.client.VerifyHeightIndex(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return err
	}
	return errCodeToError[resp.Err]
}

func (vm *VMClient) GetBlockIDAtHeight(height uint64) (ids.ID, error) {
	resp, err := vm.client.GetBlockIDAtHeight(
		context.Background(),
		&vmpb.GetBlockIDAtHeightRequest{Height: height},
	)
	if err != nil {
		return ids.Empty, err
	}
	if errCode := resp.Err; errCode != 0 {
		return ids.Empty, errCodeToError[errCode]
	}
	return ids.ToID(resp.BlkId)
}

func (vm *VMClient) StateSyncEnabled() (bool, error) {
	resp, err := vm.client.StateSyncEnabled(
		context.Background(),
		&emptypb.Empty{},
	)
	if err != nil {
		return false, err
	}
	err = errCodeToError[resp.Err]
	if err == block.ErrStateSyncableVMNotImplemented {
		return false, nil
	}
	return resp.Enabled, err
}

func (vm *VMClient) GetOngoingSyncStateSummary() (block.StateSummary, error) {
	resp, err := vm.client.GetOngoingSyncStateSummary(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) GetLastStateSummary() (block.StateSummary, error) {
	resp, err := vm.client.GetLastStateSummary(context.Background(), &emptypb.Empty{})
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  resp.Bytes,
	}, err
}

func (vm *VMClient) ParseStateSummary(summaryBytes []byte) (block.StateSummary, error) {
	resp, err := vm.client.ParseStateSummary(
		context.Background(),
		&vmpb.ParseStateSummaryRequest{
			Bytes: summaryBytes,
		},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: resp.Height,
		bytes:  summaryBytes,
	}, err
}

func (vm *VMClient) GetStateSummary(summaryHeight uint64) (block.StateSummary, error) {
	resp, err := vm.client.GetStateSummary(
		context.Background(),
		&vmpb.GetStateSummaryRequest{
			Height: summaryHeight,
		},
	)
	if err != nil {
		return nil, err
	}
	if errCode := resp.Err; errCode != 0 {
		return nil, errCodeToError[errCode]
	}

	summaryID, err := ids.ToID(resp.Id)
	return &summaryClient{
		vm:     vm,
		id:     summaryID,
		height: summaryHeight,
		bytes:  resp.Bytes,
	}, err
}

type blockClient struct {
	vm *VMClient

	id       ids.ID
	parentID ids.ID
	status   choices.Status
	bytes    []byte
	height   uint64
	time     time.Time
}

func (b *blockClient) ID() ids.ID { return b.id }

func (b *blockClient) Accept() error {
	b.status = choices.Accepted
	_, err := b.vm.client.BlockAccept(context.Background(), &vmpb.BlockAcceptRequest{
		Id: b.id[:],
	})
	return err
}

func (b *blockClient) Reject() error {
	b.status = choices.Rejected
	_, err := b.vm.client.BlockReject(context.Background(), &vmpb.BlockRejectRequest{
		Id: b.id[:],
	})
	return err
}

func (b *blockClient) Status() choices.Status { return b.status }

func (b *blockClient) Parent() ids.ID { return b.parentID }

func (b *blockClient) Verify() error {
	resp, err := b.vm.client.BlockVerify(context.Background(), &vmpb.BlockVerifyRequest{
		Bytes: b.bytes,
	})
	if err != nil {
		return err
	}

	b.time, err = grpcutils.TimestampAsTime(resp.Timestamp)
	return err
}

func (b *blockClient) Bytes() []byte        { return b.bytes }
func (b *blockClient) Height() uint64       { return b.height }
func (b *blockClient) Timestamp() time.Time { return b.time }

type summaryClient struct {
	vm *VMClient

	id     ids.ID
	height uint64
	bytes  []byte
}

func (s *summaryClient) ID() ids.ID     { return s.id }
func (s *summaryClient) Height() uint64 { return s.height }
func (s *summaryClient) Bytes() []byte  { return s.bytes }

func (s *summaryClient) Accept() (bool, error) {
	resp, err := s.vm.client.StateSummaryAccept(
		context.Background(),
		&vmpb.StateSummaryAcceptRequest{
			Bytes: s.bytes,
		},
	)
	if err != nil {
		return false, err
	}
	return resp.Accepted, errCodeToError[resp.Err]
}

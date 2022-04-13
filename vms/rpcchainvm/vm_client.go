// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcchainvm

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hashicorp/go-plugin"

	"github.com/prometheus/client_golang/prometheus"

	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"

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

var (
	errUnsupportedFXs = errors.New("unsupported feature extensions")

	_ block.ChainVM              = &VMClient{}
	_ block.BatchedChainVM       = &VMClient{}
	_ block.HeightIndexedChainVM = &VMClient{}
)

const (
	decidedCacheSize    = 2048
	missingCacheSize    = 2048
	unverifiedCacheSize = 2048
	bytesToIDCacheSize  = 2048
)

// VMClient is an implementation of VM that talks over RPC.
type VMClient struct {
	*chain.State
	client vmpb.VMClient
	proc   *plugin.Client

	messenger    *messenger.Server
	keystore     *gkeystore.Server
	sharedMemory *gsharedmemory.Server
	bcLookup     *galiasreader.Server
	snLookup     *gsubnetlookup.Server
	appSender    *appsender.Server

	serverCloser grpcutils.ServerCloser
	conns        []*grpc.ClientConn

	grpcHealthChecks map[string]string

	grpcServerMetrics *grpc_prometheus.ServerMetrics

	ctx *snow.Context
}

// NewClient returns a VM connected to a remote VM
func NewClient(client vmpb.VMClient) *VMClient {
	return &VMClient{
		client:           client,
		grpcHealthChecks: make(map[string]string),
	}
}

// SetProcess gives ownership of the server process to the client.
func (vm *VMClient) SetProcess(proc *plugin.Client) {
	vm.proc = proc
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
		// Register gRPC server for health checks
		vm.grpcHealthChecks[fmt.Sprintf("database-%s", dbVersion)] = serverAddr

		go grpcutils.Serve(serverListener, vm.getDBServerFunc(db))
		vm.ctx.Log.Info("grpc: serving database version: %s on: %s", dbVersion, serverAddr)

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

	// Register gRPC server for health checks
	vm.grpcHealthChecks["vm"] = serverAddr

	go grpcutils.Serve(serverListener, vm.getInitServer)
	vm.ctx.Log.Info("grpc: serving vm services on: %s", serverAddr)

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

	status := choices.Status(resp.Status)
	if err := status.Valid(); err != nil {
		return err
	}

	timestamp := time.Time{}
	if err := timestamp.UnmarshalBinary(resp.Timestamp); err != nil {
		return err
	}

	lastAcceptedBlk := &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     timestamp,
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
	_, err := vm.client.SetState(context.Background(), &vmpb.SetStateRequest{
		State: uint32(state),
	})

	return err
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

	timestamp := time.Time{}
	if err := timestamp.UnmarshalBinary(resp.Timestamp); err != nil {
		return nil, err
	}

	return &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   choices.Processing,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     timestamp,
	}, nil
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

	timestamp := time.Time{}
	if err := timestamp.UnmarshalBinary(resp.Timestamp); err != nil {
		return nil, err
	}

	blk := &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    bytes,
		height:   resp.Height,
		time:     timestamp,
	}

	return blk, nil
}

func (vm *VMClient) getBlock(id ids.ID) (snowman.Block, error) {
	resp, err := vm.client.GetBlock(context.Background(), &vmpb.GetBlockRequest{
		Id: id[:],
	})
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

	timestamp := time.Time{}
	if err := timestamp.UnmarshalBinary(resp.Timestamp); err != nil {
		return nil, err
	}

	blk := &BlockClient{
		vm:       vm,
		id:       id,
		parentID: parentID,
		status:   status,
		bytes:    resp.Bytes,
		height:   resp.Height,
		time:     timestamp,
	}

	return blk, nil
}

func (vm *VMClient) SetPreference(id ids.ID) error {
	_, err := vm.client.SetPreference(context.Background(), &vmpb.SetPreferenceRequest{
		Id: id[:],
	})
	return err
}

func (vm *VMClient) HealthCheck() (interface{}, error) {
	resp, err := vm.client.Health(context.Background(), &vmpb.HealthRequest{
		GrpcChecks: vm.grpcHealthChecks,
	})
	if err != nil {
		vm.ctx.Log.Warn("health check failed: %v", err)
	}
	return resp, err
}

func (vm *VMClient) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	deadlineBytes, err := deadline.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = vm.client.AppRequest(
		context.Background(),
		&vmpb.AppRequestMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
			Request:   request,
			Deadline:  deadlineBytes,
		},
	)
	return err
}

func (vm *VMClient) AppResponse(nodeID ids.ShortID, requestID uint32, response []byte) error {
	_, err := vm.client.AppResponse(
		context.Background(),
		&vmpb.AppResponseMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
			Response:  response,
		},
	)
	return err
}

func (vm *VMClient) AppRequestFailed(nodeID ids.ShortID, requestID uint32) error {
	_, err := vm.client.AppRequestFailed(
		context.Background(),
		&vmpb.AppRequestFailedMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
		},
	)
	return err
}

func (vm *VMClient) AppGossip(nodeID ids.ShortID, msg []byte) error {
	_, err := vm.client.AppGossip(
		context.Background(),
		&vmpb.AppGossipMsg{
			NodeId: nodeID[:],
			Msg:    msg,
		},
	)
	return err
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
		return nil, fmt.Errorf("BatchedParse block returned different number of blocks than expected")
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

		timestamp := time.Time{}
		if err := timestamp.UnmarshalBinary(blkResp.Timestamp); err != nil {
			return nil, err
		}

		blk := &BlockClient{
			vm:       vm,
			id:       id,
			parentID: parentID,
			status:   status,
			bytes:    blksBytes[idx],
			height:   blkResp.Height,
			time:     timestamp,
		}

		res = append(res, blk)
	}

	return res, nil
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

func (vm *VMClient) Connected(nodeID ids.ShortID, nodeVersion version.Application) error {
	_, err := vm.client.Connected(context.Background(), &vmpb.ConnectedRequest{
		NodeId:  nodeID[:],
		Version: nodeVersion.String(),
	})
	return err
}

func (vm *VMClient) Disconnected(nodeID ids.ShortID) error {
	_, err := vm.client.Disconnected(context.Background(), &vmpb.DisconnectedRequest{
		NodeId: nodeID[:],
	})
	return err
}

// BlockClient is an implementation of Block that talks over RPC.
type BlockClient struct {
	vm *VMClient

	id       ids.ID
	parentID ids.ID
	status   choices.Status
	bytes    []byte
	height   uint64
	time     time.Time
}

func (b *BlockClient) ID() ids.ID { return b.id }

func (b *BlockClient) Accept() error {
	b.status = choices.Accepted
	_, err := b.vm.client.BlockAccept(context.Background(), &vmpb.BlockAcceptRequest{
		Id: b.id[:],
	})
	return err
}

func (b *BlockClient) Reject() error {
	b.status = choices.Rejected
	_, err := b.vm.client.BlockReject(context.Background(), &vmpb.BlockRejectRequest{
		Id: b.id[:],
	})
	return err
}

func (b *BlockClient) Status() choices.Status { return b.status }

func (b *BlockClient) Parent() ids.ID {
	return b.parentID
}

func (b *BlockClient) Verify() error {
	resp, err := b.vm.client.BlockVerify(context.Background(), &vmpb.BlockVerifyRequest{
		Bytes: b.bytes,
	})
	if err != nil {
		return err
	}
	return b.time.UnmarshalBinary(resp.Timestamp)
}

func (b *BlockClient) Bytes() []byte        { return b.bytes }
func (b *BlockClient) Height() uint64       { return b.height }
func (b *BlockClient) Timestamp() time.Time { return b.time }

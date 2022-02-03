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

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/ava-labs/avalanchego/api/keystore/gkeystore"
	"github.com/ava-labs/avalanchego/api/metrics"
	"github.com/ava-labs/avalanchego/api/proto/appsenderproto"
	"github.com/ava-labs/avalanchego/api/proto/galiasreaderproto"
	"github.com/ava-labs/avalanchego/api/proto/ghttpproto"
	"github.com/ava-labs/avalanchego/api/proto/gkeystoreproto"
	"github.com/ava-labs/avalanchego/api/proto/gsharedmemoryproto"
	"github.com/ava-labs/avalanchego/api/proto/gsubnetlookupproto"
	"github.com/ava-labs/avalanchego/api/proto/messengerproto"
	"github.com/ava-labs/avalanchego/api/proto/rpcdbproto"
	"github.com/ava-labs/avalanchego/api/proto/vmproto"
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
	client vmproto.VMClient
	broker *plugin.GRPCBroker
	proc   *plugin.Client

	messenger    *messenger.Server
	keystore     *gkeystore.Server
	sharedMemory *gsharedmemory.Server
	bcLookup     *galiasreader.Server
	snLookup     *gsubnetlookup.Server
	appSender    *appsender.Server

	serverCloser grpcutils.ServerCloser
	conns        []*grpc.ClientConn

	ctx *snow.Context
}

// NewClient returns a VM connected to a remote VM
func NewClient(client vmproto.VMClient, broker *plugin.GRPCBroker) *VMClient {
	return &VMClient{
		client: client,
		broker: broker,
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

	// Initialize and serve each database and construct the db manager
	// initialize request parameters
	versionedDBs := dbManager.GetDatabases()
	versionedDBServers := make([]*vmproto.VersionedDBServer, len(versionedDBs))
	for i, semDB := range versionedDBs {
		dbBrokerID := vm.broker.NextId()
		db := rpcdb.NewServer(semDB.Database)
		go vm.broker.AcceptAndServe(dbBrokerID, vm.startDBServerFunc(db))
		versionedDBServers[i] = &vmproto.VersionedDBServer{
			DbServer: dbBrokerID,
			Version:  semDB.Version.String(),
		}
	}

	vm.messenger = messenger.NewServer(toEngine)
	vm.keystore = gkeystore.NewServer(ctx.Keystore, vm.broker)
	vm.sharedMemory = gsharedmemory.NewServer(ctx.SharedMemory, dbManager.Current().Database)
	vm.bcLookup = galiasreader.NewServer(ctx.BCLookup)
	vm.snLookup = gsubnetlookup.NewServer(ctx.SNLookup)
	vm.appSender = appsender.NewServer(appSender)

	// start the messenger server
	messengerBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(messengerBrokerID, vm.startMessengerServer)

	// start the keystore server
	keystoreBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(keystoreBrokerID, vm.startKeystoreServer)

	// start the shared memory server
	sharedMemoryBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(sharedMemoryBrokerID, vm.startSharedMemoryServer)

	// start the blockchain alias server
	bcLookupBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(bcLookupBrokerID, vm.startBCLookupServer)

	// start the subnet alias server
	snLookupBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(snLookupBrokerID, vm.startSNLookupServer)

	// start the AppSender server
	appSenderBrokerID := vm.broker.NextId()
	go vm.broker.AcceptAndServe(appSenderBrokerID, vm.startAppSenderServer)

	resp, err := vm.client.Initialize(context.Background(), &vmproto.InitializeRequest{
		NetworkId:          ctx.NetworkID,
		SubnetId:           ctx.SubnetID[:],
		ChainId:            ctx.ChainID[:],
		NodeId:             ctx.NodeID.Bytes(),
		XChainId:           ctx.XChainID[:],
		AvaxAssetId:        ctx.AVAXAssetID[:],
		GenesisBytes:       genesisBytes,
		UpgradeBytes:       upgradeBytes,
		ConfigBytes:        configBytes,
		DbServers:          versionedDBServers,
		EngineServer:       messengerBrokerID,
		KeystoreServer:     keystoreBrokerID,
		SharedMemoryServer: sharedMemoryBrokerID,
		BcLookupServer:     bcLookupBrokerID,
		SnLookupServer:     snLookupBrokerID,
		AppSenderServer:    appSenderBrokerID,
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

	registerer := prometheus.NewRegistry()
	multiGatherer := metrics.NewMultiGatherer()
	if err := multiGatherer.Register("rpcchainvm", registerer); err != nil {
		return err
	}
	if err := multiGatherer.Register("", vm); err != nil {
		return err
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

func (vm *VMClient) startDBServerFunc(db rpcdbproto.DatabaseServer) func(opts []grpc.ServerOption) *grpc.Server { // #nolint
	return func(opts []grpc.ServerOption) *grpc.Server {
		opts = append(opts, serverOptions...)
		server := grpc.NewServer(opts...)
		vm.serverCloser.Add(server)
		rpcdbproto.RegisterDatabaseServer(server, db)
		return server
	}
}

func (vm *VMClient) startMessengerServer(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts, serverOptions...)
	server := grpc.NewServer(opts...)
	vm.serverCloser.Add(server)
	messengerproto.RegisterMessengerServer(server, vm.messenger)
	return server
}

func (vm *VMClient) startKeystoreServer(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts, serverOptions...)
	server := grpc.NewServer(opts...)
	vm.serverCloser.Add(server)
	gkeystoreproto.RegisterKeystoreServer(server, vm.keystore)
	return server
}

func (vm *VMClient) startSharedMemoryServer(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts, serverOptions...)
	server := grpc.NewServer(opts...)
	vm.serverCloser.Add(server)
	gsharedmemoryproto.RegisterSharedMemoryServer(server, vm.sharedMemory)
	return server
}

func (vm *VMClient) startBCLookupServer(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts, serverOptions...)
	server := grpc.NewServer(opts...)
	vm.serverCloser.Add(server)
	galiasreaderproto.RegisterAliasReaderServer(server, vm.bcLookup)
	return server
}

func (vm *VMClient) startSNLookupServer(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts, serverOptions...)
	server := grpc.NewServer(opts...)
	vm.serverCloser.Add(server)
	gsubnetlookupproto.RegisterSubnetLookupServer(server, vm.snLookup)
	return server
}

func (vm *VMClient) startAppSenderServer(opts []grpc.ServerOption) *grpc.Server {
	opts = append(opts, serverOptions...)
	server := grpc.NewServer(opts...)
	vm.serverCloser.Add(server)
	appsenderproto.RegisterAppSenderServer(server, vm.appSender)
	return server
}

func (vm *VMClient) SetState(state snow.State) error {
	_, err := vm.client.SetState(context.Background(), &vmproto.SetStateRequest{
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

func (vm *VMClient) CreateStaticHandlers() (map[string]*common.HTTPHandler, error) {
	resp, err := vm.client.CreateStaticHandlers(context.Background(), &emptypb.Empty{})
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
	resp, err := vm.client.ParseBlock(context.Background(), &vmproto.ParseBlockRequest{
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
	resp, err := vm.client.GetBlock(context.Background(), &vmproto.GetBlockRequest{
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
	_, err := vm.client.SetPreference(context.Background(), &vmproto.SetPreferenceRequest{
		Id: id[:],
	})
	return err
}

func (vm *VMClient) HealthCheck() (interface{}, error) {
	return vm.client.Health(
		context.Background(),
		&emptypb.Empty{},
	)
}

func (vm *VMClient) AppRequest(nodeID ids.ShortID, requestID uint32, deadline time.Time, request []byte) error {
	deadlineBytes, err := deadline.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = vm.client.AppRequest(
		context.Background(),
		&vmproto.AppRequestMsg{
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
		&vmproto.AppResponseMsg{
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
		&vmproto.AppRequestFailedMsg{
			NodeId:    nodeID[:],
			RequestId: requestID,
		},
	)
	return err
}

func (vm *VMClient) AppGossip(nodeID ids.ShortID, msg []byte) error {
	_, err := vm.client.AppGossip(
		context.Background(),
		&vmproto.AppGossipMsg{
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
		&vmproto.GetBlockIDAtHeightRequest{Height: height},
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
	resp, err := vm.client.GetAncestors(context.Background(), &vmproto.GetAncestorsRequest{
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
	resp, err := vm.client.BatchedParseBlock(context.Background(), &vmproto.BatchedParseBlockRequest{
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
	_, err := vm.client.Connected(context.Background(), &vmproto.ConnectedRequest{
		NodeId:  nodeID[:],
		Version: nodeVersion.String(),
	})
	return err
}

func (vm *VMClient) Disconnected(nodeID ids.ShortID) error {
	_, err := vm.client.Disconnected(context.Background(), &vmproto.DisconnectedRequest{
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
	_, err := b.vm.client.BlockAccept(context.Background(), &vmproto.BlockAcceptRequest{
		Id: b.id[:],
	})
	return err
}

func (b *BlockClient) Reject() error {
	b.status = choices.Rejected
	_, err := b.vm.client.BlockReject(context.Background(), &vmproto.BlockRejectRequest{
		Id: b.id[:],
	})
	return err
}

func (b *BlockClient) Status() choices.Status { return b.status }

func (b *BlockClient) Parent() ids.ID {
	return b.parentID
}

func (b *BlockClient) Verify() error {
	resp, err := b.vm.client.BlockVerify(context.Background(), &vmproto.BlockVerifyRequest{
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

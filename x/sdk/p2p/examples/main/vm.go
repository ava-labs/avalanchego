// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/database/manager"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/snowman/block"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
	"github.com/ava-labs/avalanchego/vms"
	"github.com/ava-labs/avalanchego/vms/rpcchainvm"
	"github.com/ava-labs/avalanchego/x/sdk/p2p"
)

var (
	_ block.ChainVM = (*PingPongVM)(nil)
	_ vms.Factory   = (*PingPongVMFactory)(nil)
	_ p2p.Handler   = (*pingPongHandler)(nil)
)

type pingPongHandler struct {
	snowCtx *snow.Context
}

func (p pingPongHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) error {
	return nil
}

func (p pingPongHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, requestID uint32, deadline time.Time, requestBytes []byte) ([]byte, error) {
	p.snowCtx.Log.Info("got message from peer", zap.Stringer("nodeID", nodeID), zap.String("message", string(requestBytes)))

	return []byte("pong"), nil
}

func (p pingPongHandler) CrossChainAppRequest(ctx context.Context, chainID ids.ID, requestID uint32, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return nil, nil
}

type PingPongVMFactory struct {
}

func (p PingPongVMFactory) New(logger logging.Logger) (interface{}, error) {
	return &PingPongVM{
		shutdownCh: make(chan struct{}),
		shutdownWg: &sync.WaitGroup{},
	}, nil
}

type PingPongVM struct {
	common.AppHandler
	snowCtx        *snow.Context
	pingPongClient *p2p.Client

	peers set.Set[ids.NodeID]
	lock  sync.Mutex

	shutdownCh chan struct{}
	shutdownWg *sync.WaitGroup
}

func (p *PingPongVM) Initialize(_ context.Context, snowCtx *snow.Context, _ manager.Manager, _ []byte, _ []byte, _ []byte, _ chan<- common.Message, _ []*common.Fx, appSender common.AppSender) error {
	snowCtx.Log.Info("initializing ping pong vm")
	router := p2p.NewRouter()
	pingPongClient, err := router.RegisterAppProtocol(0x1, pingPongHandler{snowCtx: snowCtx}, appSender)
	if err != nil {
		return err
	}
	p.AppHandler = router
	p.snowCtx = snowCtx
	p.pingPongClient = pingPongClient
	p.shutdownCh = make(chan struct{})
	p.shutdownWg = &sync.WaitGroup{}

	p.shutdownWg.Add(1)
	go func() {
		defer p.shutdownWg.Done()

		for {
			select {
			case <-p.shutdownCh:
				return
			default:
				p.lock.Lock()
				for peer := range p.peers {
					if err := pingPongClient.AppRequest(context.TODO(), set.Set[ids.NodeID]{peer: struct{}{}}, []byte("ping"), func(nodeID ids.NodeID, response []byte, err error) {
						if err != nil {
							snowCtx.Log.Error("response timed out", zap.Error(err))
						}

						snowCtx.Log.Info("got message from peer", zap.Stringer("nodeID", nodeID), zap.String("message", string(response)))
					}); err != nil {
						snowCtx.Log.Error("failed to send message", zap.Error(err))
					}
				}
				p.lock.Unlock()

				time.Sleep(time.Second)
			}
		}
	}()

	return nil
}

func (p *PingPongVM) HealthCheck(ctx context.Context) (interface{}, error) {
	return nil, nil
}

func (p *PingPongVM) Connected(_ context.Context, nodeID ids.NodeID, _ *version.Application) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.Add(nodeID)
	return nil
}

func (p *PingPongVM) Disconnected(_ context.Context, nodeID ids.NodeID) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	p.peers.Remove(nodeID)
	return nil
}

func (p *PingPongVM) SetState(ctx context.Context, state snow.State) error {
	return nil
}

func (p *PingPongVM) Shutdown(ctx context.Context) error {
	close(p.shutdownCh)
	p.shutdownWg.Wait()
	return nil
}

func (p *PingPongVM) Version(ctx context.Context) (string, error) {
	return "", nil
}

func (p *PingPongVM) CreateStaticHandlers(ctx context.Context) (map[string]*common.HTTPHandler, error) {
	return nil, nil
}

func (p *PingPongVM) CreateHandlers(ctx context.Context) (map[string]*common.HTTPHandler, error) {
	return nil, nil
}

func (p *PingPongVM) GetBlock(ctx context.Context, blkID ids.ID) (snowman.Block, error) {
	return Block{}, nil
}

func (p *PingPongVM) ParseBlock(ctx context.Context, blockBytes []byte) (snowman.Block, error) {
	return nil, nil
}

func (p *PingPongVM) BuildBlock(ctx context.Context) (snowman.Block, error) {
	return nil, nil
}

func (p *PingPongVM) SetPreference(ctx context.Context, blkID ids.ID) error {
	return nil
}

func (p *PingPongVM) LastAccepted(ctx context.Context) (ids.ID, error) {
	return ids.ID{}, nil
}

var _ snowman.Block = (*Block)(nil)

type Block struct{}

func (b Block) ID() ids.ID {
	return ids.ID{}
}

func (b Block) Accept(ctx context.Context) error {
	return nil
}

func (b Block) Reject(ctx context.Context) error {
	return nil
}

func (b Block) Status() choices.Status {
	return choices.Accepted
}

func (b Block) Parent() ids.ID {
	return ids.ID{}
}

func (b Block) Verify(ctx context.Context) error {
	return nil
}

func (b Block) Bytes() []byte {
	return nil
}

func (b Block) Height() uint64 {
	return 0
}

func (b Block) Timestamp() time.Time {
	return time.Time{}
}

func main() {
	ctx := context.Background()
	if err := rpcchainvm.Serve(ctx, &PingPongVM{}); err != nil {
		panic(err)
	}
}

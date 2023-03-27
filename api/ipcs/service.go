// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"net/http"

	"github.com/gorilla/rpc/v2"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/api/server"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
)

// IPCServer maintains the IPCs
type IPCServer struct {
	httpServer   server.Server
	chainManager chains.Manager
	log          logging.Logger
	ipcs         *ipcs.ChainIPCs
}

// NewService returns a new IPCs API service
func NewService(log logging.Logger, chainManager chains.Manager, httpServer server.Server, ipcs *ipcs.ChainIPCs) (*common.HTTPHandler, error) {
	ipcServer := &IPCServer{
		log:          log,
		chainManager: chainManager,
		httpServer:   httpServer,

		ipcs: ipcs,
	}

	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")

	return &common.HTTPHandler{Handler: newServer}, newServer.RegisterService(ipcServer, "ipcs")
}

// PublishBlockchainArgs are the arguments for calling PublishBlockchain
type PublishBlockchainArgs struct {
	BlockchainID string `json:"blockchainID"`
}

// PublishBlockchainReply are the results from calling PublishBlockchain
type PublishBlockchainReply struct {
	ConsensusURL string `json:"consensusURL"`
	DecisionsURL string `json:"decisionsURL"`
}

// PublishBlockchain publishes the finalized accepted transactions from the blockchainID over the IPC
func (ipc *IPCServer) PublishBlockchain(_ *http.Request, args *PublishBlockchainArgs, reply *PublishBlockchainReply) error {
	ipc.log.Warn("deprecated API called",
		zap.String("service", "ipcs"),
		zap.String("method", "publishBlockchain"),
		logging.UserString("blockchainID", args.BlockchainID),
	)

	chainID, err := ipc.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		ipc.log.Error("chain lookup failed",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
		return err
	}

	ipcs, err := ipc.ipcs.Publish(chainID)
	if err != nil {
		ipc.log.Error("couldn't publish chain",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
		return err
	}

	reply.ConsensusURL = ipcs.ConsensusURL()
	reply.DecisionsURL = ipcs.DecisionsURL()

	return nil
}

// UnpublishBlockchainArgs are the arguments for calling UnpublishBlockchain
type UnpublishBlockchainArgs struct {
	BlockchainID string `json:"blockchainID"`
}

// UnpublishBlockchain closes publishing of a blockchainID
func (ipc *IPCServer) UnpublishBlockchain(_ *http.Request, args *UnpublishBlockchainArgs, _ *api.EmptyReply) error {
	ipc.log.Warn("deprecated API called",
		zap.String("service", "ipcs"),
		zap.String("method", "unpublishBlockchain"),
		logging.UserString("blockchainID", args.BlockchainID),
	)

	chainID, err := ipc.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		ipc.log.Error("chain lookup failed",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
		return err
	}

	ok, err := ipc.ipcs.Unpublish(chainID)
	if !ok {
		ipc.log.Error("couldn't publish chain",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
	}

	return err
}

// GetPublishedBlockchainsReply is the result from calling GetPublishedBlockchains
type GetPublishedBlockchainsReply struct {
	Chains []ids.ID `json:"chains"`
}

// GetPublishedBlockchains returns blockchains being published
func (ipc *IPCServer) GetPublishedBlockchains(_ *http.Request, _ *struct{}, reply *GetPublishedBlockchainsReply) error {
	ipc.log.Warn("deprecated API called",
		zap.String("service", "ipcs"),
		zap.String("method", "getPublishedBlockchains"),
	)
	reply.Chains = ipc.ipcs.GetPublishedBlockchains()
	return nil
}

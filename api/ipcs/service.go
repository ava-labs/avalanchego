// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"net/http"
	"sync"

	"github.com/gorilla/rpc/v2"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/chains"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/ipcs"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/logging"
)

type Service struct {
	log          logging.Logger
	chainManager chains.Manager
	lock         sync.RWMutex
	ipcs         *ipcs.ChainIPCs
}

func NewService(log logging.Logger, chainManager chains.Manager, ipcs *ipcs.ChainIPCs) (http.Handler, error) {
	server := rpc.NewServer()
	codec := json.NewCodec()
	server.RegisterCodec(codec, "application/json")
	server.RegisterCodec(codec, "application/json;charset=UTF-8")
	return server, server.RegisterService(
		&Service{
			log:          log,
			chainManager: chainManager,
			ipcs:         ipcs,
		},
		"ipcs",
	)
}

type PublishBlockchainArgs struct {
	BlockchainID string `json:"blockchainID"`
}

type PublishBlockchainReply struct {
	ConsensusURL string `json:"consensusURL"`
	DecisionsURL string `json:"decisionsURL"`
}

// PublishBlockchain publishes the finalized accepted transactions from the
// blockchainID over the IPC
func (s *Service) PublishBlockchain(_ *http.Request, args *PublishBlockchainArgs, reply *PublishBlockchainReply) error {
	s.log.Warn("deprecated API called",
		zap.String("service", "ipcs"),
		zap.String("method", "publishBlockchain"),
		logging.UserString("blockchainID", args.BlockchainID),
	)

	chainID, err := s.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		s.log.Error("chain lookup failed",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
		return err
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	ipcs, err := s.ipcs.Publish(chainID)
	if err != nil {
		s.log.Error("couldn't publish chain",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
		return err
	}

	reply.ConsensusURL = ipcs.ConsensusURL()
	reply.DecisionsURL = ipcs.DecisionsURL()

	return nil
}

type UnpublishBlockchainArgs struct {
	BlockchainID string `json:"blockchainID"`
}

// UnpublishBlockchain closes publishing of a blockchainID
func (s *Service) UnpublishBlockchain(_ *http.Request, args *UnpublishBlockchainArgs, _ *api.EmptyReply) error {
	s.log.Warn("deprecated API called",
		zap.String("service", "ipcs"),
		zap.String("method", "unpublishBlockchain"),
		logging.UserString("blockchainID", args.BlockchainID),
	)

	chainID, err := s.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		s.log.Error("chain lookup failed",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
		return err
	}

	s.lock.Lock()
	defer s.lock.Unlock()

	ok, err := s.ipcs.Unpublish(chainID)
	if !ok {
		s.log.Error("couldn't publish chain",
			logging.UserString("blockchainID", args.BlockchainID),
			zap.Error(err),
		)
	}

	return err
}

type GetPublishedBlockchainsReply struct {
	Chains []ids.ID `json:"chains"`
}

// GetPublishedBlockchains returns blockchains being published
func (s *Service) GetPublishedBlockchains(_ *http.Request, _ *struct{}, reply *GetPublishedBlockchainsReply) error {
	s.log.Warn("deprecated API called",
		zap.String("service", "ipcs"),
		zap.String("method", "getPublishedBlockchains"),
	)

	s.lock.RLock()
	defer s.lock.RUnlock()

	reply.Chains = s.ipcs.GetPublishedBlockchains()
	return nil
}

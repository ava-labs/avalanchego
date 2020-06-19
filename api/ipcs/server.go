// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"fmt"
	"net/http"

	_ "nanomsg.org/go/mangos/v2/transport/ipc" // registers the IPC transport

	"github.com/gorilla/rpc/v2"

	"github.com/ava-labs/gecko/api"
	"github.com/ava-labs/gecko/chains"
	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/utils/logging"
)

// IPCs maintains the IPCs
type IPCs struct {
	log             logging.Logger
	chainManager    chains.Manager
	networkID       uint32
	httpServer      *api.Server
	chains          map[[32]byte]*ChainIPCs
	consensusEvents *triggers.EventDispatcher
	decisionEvents  *triggers.EventDispatcher
}

// NewService returns a new IPCs API service
func NewService(log logging.Logger, chainManager chains.Manager, networkID uint32, consensusEvents *triggers.EventDispatcher, decisionEvents *triggers.EventDispatcher, defaultChainIDs []string, httpServer *api.Server) (*common.HTTPHandler, error) {
	ipcs := &IPCs{
		log:          log,
		chainManager: chainManager,
		networkID:    networkID,
		httpServer:   httpServer,
		chains:       map[[32]byte]*ChainIPCs{},

		consensusEvents: consensusEvents,
		decisionEvents:  decisionEvents,
	}

	for _, chainID := range defaultChainIDs {
		id, err := ids.FromString(chainID)
		if err != nil {
			return nil, err
		}
		if _, err := ipcs.publish(id); err != nil {
			return nil, err
		}
	}

	newServer := rpc.NewServer()
	codec := json.NewCodec()
	newServer.RegisterCodec(codec, "application/json")
	newServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	newServer.RegisterService(ipcs, "ipcs")
	return &common.HTTPHandler{Handler: newServer}, nil
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
func (ipc *IPCs) PublishBlockchain(r *http.Request, args *PublishBlockchainArgs, reply *PublishBlockchainReply) error {
	ipc.log.Info("IPCs: PublishBlockchain called with BlockchainID: %s", args.BlockchainID)
	chainID, err := ipc.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		ipc.log.Error("unknown blockchainID: %s", err)
		return err
	}

	ipcs, err := ipc.publish(chainID)
	if err != nil {
		ipc.log.Error("couldn't publish blockchainID: %s", err)
		return err
	}

	reply.ConsensusURL = ipcs.Consensus.URL()
	reply.DecisionsURL = ipcs.Decisions.URL()

	return nil
}

// UnpublishBlockchainArgs are the arguments for calling UnpublishBlockchain
type UnpublishBlockchainArgs struct {
	BlockchainID string `json:"blockchainID"`
}

// UnpublishBlockchainReply are the results from calling UnpublishBlockchain
type UnpublishBlockchainReply struct {
	Success bool `json:"success"`
}

// UnpublishBlockchain closes publishing of a blockchainID
func (ipc *IPCs) UnpublishBlockchain(r *http.Request, args *UnpublishBlockchainArgs, reply *UnpublishBlockchainReply) error {
	ipc.log.Info("IPCs: UnpublishBlockchain called with BlockchainID: %s", args.BlockchainID)
	chainID, err := ipc.chainManager.Lookup(args.BlockchainID)
	if err != nil {
		ipc.log.Error("unknown blockchainID %s: %s", args.BlockchainID, err)
		return err
	}

	chainIPCs, ok := ipc.chains[chainID.Key()]
	if !ok {
		return fmt.Errorf("blockchainID not publishing: %s", chainID)
	}

	reply.Success = true
	return chainIPCs.Stop()
}

func (ipc *IPCs) publish(chainID ids.ID) (*ChainIPCs, error) {
	chainIDKey := chainID.Key()

	if ipcs, ok := ipc.chains[chainIDKey]; ok {
		ipc.log.Info("returning existing blockchainID %s", chainID.String())
		return ipcs, nil
	}

	ipcs, err := NewChainIPCs(ipc.log, ipc.networkID, chainID, ipc.consensusEvents, ipc.decisionEvents)
	if err != nil {
		ipc.log.Error("can't create ipcs: %s", err)
		return nil, err
	}

	ipc.chains[chainIDKey] = ipcs
	return ipcs, nil
}

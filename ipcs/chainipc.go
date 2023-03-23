// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"fmt"
	"path/filepath"

	"go.uber.org/zap"

	"golang.org/x/exp/maps"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

const (
	// DefaultBaseURL can be used as a reasonable default value for the base URL
	DefaultBaseURL = "/tmp"

	ipcIdentifierPrefix    = "ipc"
	ipcConsensusIdentifier = "consensus"
	ipcDecisionsIdentifier = "decisions"
)

type context struct {
	log       logging.Logger
	networkID uint32
	path      string
}

// ChainIPCs maintains IPCs for a set of chains
type ChainIPCs struct {
	context
	chains              map[ids.ID]*EventSockets
	blockAcceptorGroup  snow.AcceptorGroup
	txAcceptorGroup     snow.AcceptorGroup
	vertexAcceptorGroup snow.AcceptorGroup
}

// NewChainIPCs creates a new *ChainIPCs that writes consensus and decision
// events to IPC sockets
func NewChainIPCs(
	log logging.Logger,
	path string,
	networkID uint32,
	blockAcceptorGroup snow.AcceptorGroup,
	txAcceptorGroup snow.AcceptorGroup,
	vertexAcceptorGroup snow.AcceptorGroup,
	defaultChainIDs []ids.ID,
) (*ChainIPCs, error) {
	cipcs := &ChainIPCs{
		context: context{
			log:       log,
			networkID: networkID,
			path:      path,
		},
		chains:              make(map[ids.ID]*EventSockets),
		blockAcceptorGroup:  blockAcceptorGroup,
		txAcceptorGroup:     txAcceptorGroup,
		vertexAcceptorGroup: vertexAcceptorGroup,
	}
	for _, chainID := range defaultChainIDs {
		if _, err := cipcs.Publish(chainID); err != nil {
			return nil, err
		}
	}
	return cipcs, nil
}

// Publish creates a set of eventSockets for the given chainID
func (cipcs *ChainIPCs) Publish(chainID ids.ID) (*EventSockets, error) {
	if es, ok := cipcs.chains[chainID]; ok {
		cipcs.log.Info("returning existing event sockets",
			zap.Stringer("blockchainID", chainID),
		)
		return es, nil
	}

	es, err := newEventSockets(
		cipcs.context,
		chainID,
		cipcs.blockAcceptorGroup,
		cipcs.txAcceptorGroup,
		cipcs.vertexAcceptorGroup,
	)
	if err != nil {
		cipcs.log.Error("can't create ipcs",
			zap.Error(err),
		)
		return nil, err
	}

	cipcs.chains[chainID] = es
	cipcs.log.Info("created IPC sockets",
		zap.Stringer("blockchainID", chainID),
		zap.String("consensusURL", es.ConsensusURL()),
		zap.String("decisionsURL", es.DecisionsURL()),
	)
	return es, nil
}

// Unpublish stops the eventSocket for the given chain if it exists. It returns
// whether or not the socket existed and errors when trying to close it
func (cipcs *ChainIPCs) Unpublish(chainID ids.ID) (bool, error) {
	chainIPCs, ok := cipcs.chains[chainID]
	if !ok {
		return false, nil
	}
	delete(cipcs.chains, chainID)
	return true, chainIPCs.stop()
}

// GetPublishedBlockchains returns the chains that are currently being published
func (cipcs *ChainIPCs) GetPublishedBlockchains() []ids.ID {
	return maps.Keys(cipcs.chains)
}

func (cipcs *ChainIPCs) Shutdown() error {
	cipcs.log.Info("shutting down chain IPCs")

	errs := wrappers.Errs{}
	for _, ch := range cipcs.chains {
		errs.Add(ch.stop())
	}
	return errs.Err
}

func ipcURL(ctx context, chainID ids.ID, eventType string) string {
	return filepath.Join(ctx.path, fmt.Sprintf("%d-%s-%s", ctx.networkID, chainID.String(), eventType))
}

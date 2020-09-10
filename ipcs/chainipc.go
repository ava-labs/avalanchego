// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"fmt"
	"path/filepath"

	"github.com/ava-labs/avalanche-go/ids"
	"github.com/ava-labs/avalanche-go/snow/triggers"
	"github.com/ava-labs/avalanche-go/utils/logging"
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
	chains          map[[32]byte]*EventSockets
	consensusEvents *triggers.EventDispatcher
	decisionEvents  *triggers.EventDispatcher
}

// NewChainIPCs creates a new *ChainIPCs that writes consensus and decision
// events to IPC sockets
func NewChainIPCs(log logging.Logger, path string, networkID uint32, consensusEvents *triggers.EventDispatcher, decisionEvents *triggers.EventDispatcher, defaultChainIDs []ids.ID) (*ChainIPCs, error) {
	cipcs := &ChainIPCs{
		context: context{
			log:       log,
			networkID: networkID,
			path:      path,
		},
		chains:          make(map[[32]byte]*EventSockets),
		consensusEvents: consensusEvents,
		decisionEvents:  decisionEvents,
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
	chainIDKey := chainID.Key()

	if es, ok := cipcs.chains[chainIDKey]; ok {
		cipcs.log.Info("returning existing blockchainID %s", chainID.String())
		return es, nil
	}

	es, err := newEventSockets(cipcs.context, chainID, cipcs.consensusEvents, cipcs.decisionEvents)
	if err != nil {
		cipcs.log.Error("can't create ipcs: %s", err)
		return nil, err
	}

	cipcs.chains[chainIDKey] = es
	cipcs.log.Info("created IPC sockets for blockchain %s at %s and %s", chainID.String(), es.ConsensusURL(), es.DecisionsURL())
	return es, nil
}

// Unpublish stops the eventSocket for the given chain if it exists. It returns
// whether or not the socket existed and errors when trying to close it
func (cipcs *ChainIPCs) Unpublish(chainID ids.ID) (bool, error) {
	chainIPCs, ok := cipcs.chains[chainID.Key()]
	if !ok {
		return false, nil
	}
	return true, chainIPCs.stop()
}

func ipcURL(ctx context, chainID ids.ID, eventType string) string {
	return filepath.Join(ctx.path, fmt.Sprintf("%d-%s-%s", ctx.networkID, chainID.String(), eventType))
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"fmt"
	"path"

	"nanomsg.org/go/mangos/v2"
	"nanomsg.org/go/mangos/v2/protocol/pub"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

const (
	defaultBaseURL = "/tmp"

	ipcIdentifierPrefix    = "ipc"
	ipcConsensusIdentifier = "consensus"
	ipcDecisionsIdentifier = "decisions"
)

// ChainIPCs manages multiple IPCs for a single chain. Not all IPCs are
// required to be set
type ChainIPCs struct {
	Consensus *eventIPCSocket
	Decisions *eventIPCSocket
}

// NewChainIPCs creates a *ChainIPCs with both consensus and decisions IPCs
func NewChainIPCs(log logging.Logger, networkID uint32, chainID ids.ID, consensusEvents *triggers.EventDispatcher, decisionEvents *triggers.EventDispatcher) (*ChainIPCs, error) {
	consensusIPC, err := newEventIPCSocket(log, networkID, chainID, ipcConsensusIdentifier, consensusEvents)
	if err != nil {
		return nil, err
	}

	decisionsIPC, err := newEventIPCSocket(log, networkID, chainID, ipcDecisionsIdentifier, decisionEvents)
	if err != nil {
		return nil, err
	}

	return &ChainIPCs{Consensus: consensusIPC, Decisions: decisionsIPC}, nil
}

// Accept delivers a message to the underlying IPCs
func (ipcs *ChainIPCs) Accept(chainID, containerID ids.ID, container []byte) error {
	if ipcs.Consensus != nil {
		if err := ipcs.Consensus.Accept(chainID, containerID, container); err != nil {
			return err
		}
	}

	if ipcs.Decisions != nil {
		if err := ipcs.Decisions.Accept(chainID, containerID, container); err != nil {
			return err
		}
	}
	return nil
}

// Stop closes the underlying sockets
func (ipcs *ChainIPCs) Stop() error {
	errs := wrappers.Errs{}

	if ipcs.Consensus != nil {
		errs.Add(ipcs.Consensus.Stop())
	}

	if ipcs.Decisions != nil {
		errs.Add(ipcs.Decisions.Stop())
	}

	return errs.Err
}

// eventIPCSocket is a single IPC socket for a single chain
type eventIPCSocket struct {
	url          string
	log          logging.Logger
	socket       mangos.Socket
	unregisterFn func() error
}

// newEventIPCSocket creates a *eventIPCSocket for the given chain and
// EventDispatcher that writes to a local IPC socket
func newEventIPCSocket(log logging.Logger, networkID uint32, chainID ids.ID, name string, events *triggers.EventDispatcher) (*eventIPCSocket, error) {
	sock, err := pub.NewSocket()
	if err != nil {
		return nil, err
	}

	ipcName := ipcIdentifierPrefix + "-" + name

	eis := &eventIPCSocket{
		log:    log,
		socket: sock,
		url:    ipcURL(defaultBaseURL, networkID, chainID, name),
		unregisterFn: func() error {
			return events.DeregisterChain(chainID, ipcName)
		},
	}

	if err = sock.Listen("ipc://" + eis.url); err != nil {
		sock.Close()
		return nil, err
	}

	if err := events.RegisterChain(chainID, ipcName, eis); err != nil {
		eis.Stop()
		return nil, err
	}

	return eis, nil
}

// Accept delivers a message to the eventIPCSocket
func (eis *eventIPCSocket) Accept(_, _ ids.ID, container []byte) error {
	err := eis.socket.Send(container)
	if err != nil {
		eis.log.Error("%s while trying to send:\n%s", err, formatting.DumpBytes{Bytes: container})
	}
	return err
}

// Stop halts the eventIPCSocket event loop
func (eis *eventIPCSocket) Stop() error {
	eis.log.Info("closing Chain IPC")

	errs := wrappers.Errs{}
	errs.Add(eis.unregisterFn(), eis.socket.Close())

	return errs.Err
}

// URL returns the URL of the socket
func (eis *eventIPCSocket) URL() string {
	return eis.url
}

func ipcURL(base string, networkID uint32, chainID ids.ID, eventType string) string {
	return path.Join(base, fmt.Sprintf("%d-%s-%s", networkID, chainID.String(), eventType))
}

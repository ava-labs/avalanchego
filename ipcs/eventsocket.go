// (c) 2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"go.nanomsg.org/mangos/v3/protocol/pub"

	mangos "go.nanomsg.org/mangos/v3"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/triggers"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
	"github.com/ava-labs/gecko/utils/wrappers"
)

type chainEventDipatcher struct {
	chainID ids.ID
	events  *triggers.EventDispatcher
}

// eventSockets is a set of named eventSockets
type eventSockets struct {
	consensusSocket *eventSocket
	decisionsSocket *eventSocket
}

// newEventSockets creates a *ChainIPCs with both consensus and decisions IPCs
func newEventSockets(ctx context, chainID ids.ID, consensusEvents *triggers.EventDispatcher, decisionEvents *triggers.EventDispatcher) (*eventSockets, error) {
	consensusIPC, err := newEventIPCSocket(ctx, chainID, ipcConsensusIdentifier, consensusEvents)
	if err != nil {
		return nil, err
	}

	decisionsIPC, err := newEventIPCSocket(ctx, chainID, ipcDecisionsIdentifier, decisionEvents)
	if err != nil {
		return nil, err
	}

	return &eventSockets{
		consensusSocket: consensusIPC,
		decisionsSocket: decisionsIPC,
	}, nil
}

// Accept delivers a message to the underlying eventSockets
func (ipcs *eventSockets) Accept(chainID, containerID ids.ID, container []byte) error {
	if ipcs.consensusSocket != nil {
		if err := ipcs.consensusSocket.Accept(chainID, containerID, container); err != nil {
			return err
		}
	}

	if ipcs.decisionsSocket != nil {
		if err := ipcs.decisionsSocket.Accept(chainID, containerID, container); err != nil {
			return err
		}
	}

	return nil
}

// stop closes the underlying eventSockets
func (ipcs *eventSockets) stop() error {
	errs := wrappers.Errs{}

	if ipcs.consensusSocket != nil {
		errs.Add(ipcs.consensusSocket.stop())
	}

	if ipcs.decisionsSocket != nil {
		errs.Add(ipcs.decisionsSocket.stop())
	}

	return errs.Err
}

// ConsensusURL returns the URL of socket receiving consensus events
func (ipcs *eventSockets) ConsensusURL() string {
	return ipcs.consensusSocket.URL()
}

// DecisionsURL returns the URL of socket receiving decisions events
func (ipcs *eventSockets) DecisionsURL() string {
	return ipcs.decisionsSocket.URL()
}

// eventSocket is a single IPC socket for a single chain
type eventSocket struct {
	url          string
	log          logging.Logger
	socket       mangos.Socket
	unregisterFn func() error
}

// newEventIPCSocket creates a *eventSocket for the given chain and
// EventDispatcher that writes to a local IPC socket
func newEventIPCSocket(ctx context, chainID ids.ID, name string, events *triggers.EventDispatcher) (*eventSocket, error) {
	sock, err := pub.NewSocket()
	if err != nil {
		return nil, err
	}

	ipcName := ipcIdentifierPrefix + "-" + name

	eis := &eventSocket{
		log:    ctx.log,
		socket: sock,
		url:    ipcURL(ctx, chainID, name),
		unregisterFn: func() error {
			return events.DeregisterChain(chainID, ipcName)
		},
	}

	if err = sock.Listen("ipc://" + eis.url); err != nil {
		if err := sock.Close(); err != nil {
			return nil, err
		}
		return nil, err
	}

	if err := events.RegisterChain(chainID, ipcName, eis); err != nil {
		if err := eis.stop(); err != nil {
			return nil, err
		}
		return nil, err
	}

	return eis, nil
}

// Accept delivers a message to the eventSocket
func (eis *eventSocket) Accept(_, _ ids.ID, container []byte) error {
	err := eis.socket.Send(container)
	if err != nil {
		eis.log.Error("%s while trying to send:\n%s", err, formatting.DumpBytes{Bytes: container})
	}
	return err
}

// stop unregisters the event handler and closes the eventSocket
func (eis *eventSocket) stop() error {
	eis.log.Info("closing Chain IPC")

	errs := wrappers.Errs{}
	errs.Add(eis.unregisterFn(), eis.socket.Close())

	return errs.Err
}

// URL returns the URL of the socket
func (eis *eventSocket) URL() string {
	return eis.url
}

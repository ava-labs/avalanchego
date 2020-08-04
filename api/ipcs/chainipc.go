// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ipcs

import (
	"go.nanomsg.org/mangos/v3"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/utils/formatting"
	"github.com/ava-labs/gecko/utils/logging"
)

// ChainIPC a struct which holds IPC socket information
type ChainIPC struct {
	log    logging.Logger
	socket mangos.Socket
}

// Accept delivers a message to the ChainIPC
func (cipc *ChainIPC) Accept(chainID, containerID ids.ID, container []byte) error {
	err := cipc.socket.Send(container)
	if err != nil {
		cipc.log.Error("%s while trying to send:\n%s", err, formatting.DumpBytes{Bytes: container})
	}
	return err
}

// Stop halts the ChainIPC event loop
func (cipc *ChainIPC) Stop() error {
	cipc.log.Info("closing Chain IPC")
	return cipc.socket.Close()
}

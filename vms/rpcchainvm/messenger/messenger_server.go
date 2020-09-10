// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package messenger

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanche-go/snow/engine/common"
	"github.com/ava-labs/avalanche-go/vms/rpcchainvm/messenger/messengerproto"
)

var (
	errFullQueue = errors.New("full message queue")
)

// Server is a messenger that is managed over RPC.
type Server struct {
	messenger chan<- common.Message
}

// NewServer returns a vm instance connected to a remote vm instance
func NewServer(messenger chan<- common.Message) *Server {
	return &Server{messenger: messenger}
}

// Notify ...
func (s *Server) Notify(_ context.Context, req *messengerproto.NotifyRequest) (*messengerproto.NotifyResponse, error) {
	msg := common.Message(req.Message)
	select {
	case s.messenger <- msg:
		return &messengerproto.NotifyResponse{}, nil
	default:
		return nil, errFullQueue
	}
}

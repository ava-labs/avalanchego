// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gteleporter

import (
	"context"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/vms/platformvm/teleporter"

	pb "github.com/ava-labs/avalanchego/proto/pb/teleporter"
)

var _ pb.SignerServer = (*Server)(nil)

type Server struct {
	pb.UnsafeSignerServer
	signer teleporter.Signer
}

func NewServer(signer teleporter.Signer) *Server {
	return &Server{signer: signer}
}

func (s *Server) Sign(_ context.Context, unsignedMsg *pb.SignRequest) (*pb.SignResponse, error) {
	sourceChainID, err := ids.ToID(unsignedMsg.SourceChainId)
	if err != nil {
		return nil, err
	}

	destinationChainID, err := ids.ToID(unsignedMsg.DestinationChainId)
	if err != nil {
		return nil, err
	}

	msg, err := teleporter.NewUnsignedMessage(
		sourceChainID,
		destinationChainID,
		unsignedMsg.Payload,
	)
	if err != nil {
		return nil, err
	}

	sig, err := s.signer.Sign(msg)
	return &pb.SignResponse{
		Signature: sig,
	}, err
}

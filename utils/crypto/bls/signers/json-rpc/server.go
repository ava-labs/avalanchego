// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package jsonrpc

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signers/localsigner"
)

type signerService struct {
	signer *localsigner.LocalSigner
}

type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

func NewSignerService() *signerService {
	signer, err := localsigner.NewSigner()
	if err != nil {
		panic(err)
	}

	return &signerService{signer: signer}
}

func Serve(service *signerService) (*Server, error) {
	server := rpc.NewServer()
	server.RegisterCodec(json.NewCodec(), "application/json")

	err := server.RegisterService(service, "Signer")
	if err != nil {
		return nil, err
	}

	httpServer := &http.Server{
		Handler:           server,
		ReadHeaderTimeout: 1 * time.Second,
	}

	listener, err := net.Listen("tcp", "")
	if err != nil {
		return nil, err
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	return &Server{
		httpServer: httpServer,
		listener:   listener,
	}, nil
}

func (s *Server) Addr() net.Addr {
	return s.listener.Addr()
}

func (s *Server) Close() error {
	// Create a context with a timeout to allow for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Shutdown the HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return err
	}

	// Close the listener
	return s.listener.Close()
}

func (s *signerService) PublicKey(r *http.Request, args *PublicKeyArgs, reply *PublicKeyReply) error {
	*reply = toPkReply(s.signer.PublicKey())
	return nil
}

func (s *signerService) Sign(r *http.Request, args *struct{ Msg []byte }, reply *SignReply) error {
	*reply = toSignReply(s.signer.Sign(args.Msg))
	return nil
}

func (s *signerService) SignProofOfPossession(r *http.Request, args *struct{ Msg []byte }, reply *SignProofOfPossessionReply) error {
	*reply = toSignProofOfPossessionReply(s.signer.SignProofOfPossession(args.Msg))
	return nil
}

func toPkReply(pk *bls.PublicKey) PublicKeyReply {
	return PublicKeyReply{PublicKey: pk.Serialize()}
}

func toSignReply(sig *bls.Signature) SignReply {
	return SignReply{Signature: sig.Serialize()}
}

func toSignProofOfPossessionReply(sig *bls.Signature) SignProofOfPossessionReply {
	return SignProofOfPossessionReply{Signature: sig.Serialize()}
}

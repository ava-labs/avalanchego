package jsonrpc

import (
	"log"
	"net"
	"net/http"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signers/local"
	"github.com/ava-labs/avalanchego/utils/json"
	"github.com/gorilla/rpc/v2"
)

type signerService struct {
	signer *local.LocalSigner
}

func SignerServerListenAndServe() (*net.TCPAddr, error) {
	signer, err := local.NewSigner()

	if err != nil {
		panic(err)
	}

	s := rpc.NewServer()
	s.RegisterCodec(json.NewCodec(), "application/json")

	err = s.RegisterService(&signerService{signer: signer}, "Signer")

	if err != nil {
		log.Fatal("error registering service: ", err)
		panic(err)

	}

	http.Handle("/", s)

	// TODO: get port as argument or use ephemeral port
	listener, err := net.Listen("tcp", "")

	if err != nil {
		return nil, err
	}

	go func() {
		if err := http.Serve(listener, s); err != nil {
			log.Fatal(err)
		}
	}()

	return listener.Addr().(*net.TCPAddr), nil
}

func (s *signerService) PublicKey(r *http.Request, args *struct{}, reply *PublicKeyReply) error {
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

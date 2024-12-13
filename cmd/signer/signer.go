// wrap a utils/crypto/bls/signers/local Signer with an http server with a json_rpc api with three methods:
// 1. Sign
// 2. SignProofOfPossession
// 3. PublicKey

package main

import (
	"log"
	"net/http"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	local "github.com/ava-labs/avalanchego/utils/crypto/bls/signers/local"
	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json"
)

func main() {
	signer, err := local.NewSigner()

	if err != nil {
		panic(err)
	}

	s := rpc.NewServer()
	s.RegisterCodec(json.NewCodec(), "application/json")

	err = s.RegisterService(&SignerService{signer: signer}, "Signer")

	if err != nil {
		log.Fatal("error registering service: ", err)
		panic(err)

	}

	http.Handle("/", s)

	// TODO: get port as argument or use ephemeral port
	err = http.ListenAndServe(":8080", s)

	if err != nil {
		panic(err)
	}
}

type SignerService struct {
	signer *local.LocalSigner
}

type PublicKeyReply struct {
	PublicKey []byte
}

type SignReply struct {
	Signature []byte
}

type SignProofOfPossessionReply struct {
	Signature []byte
}

func (s *SignerService) PublicKey(r *http.Request, args *struct{}, reply *PublicKeyReply) error {
	*reply = toPkReply(s.signer.PublicKey())
	return nil
}

func (s *SignerService) Sign(r *http.Request, args *struct{ Msg []byte }, reply *SignReply) error {
	*reply = toSignReply(s.signer.Sign(args.Msg))
	return nil
}

func (s *SignerService) SignProofOfPossession(r *http.Request, args *struct{ Msg []byte }, reply *SignProofOfPossessionReply) error {
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

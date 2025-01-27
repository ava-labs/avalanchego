package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/ava-labs/avalanchego/cubist/api"
	"github.com/ava-labs/avalanchego/proto/pb/signer"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"google.golang.org/grpc"
)

var popDst = base64.StdEncoding.EncodeToString(bls.CiphersuiteProofOfPossession)

type SignerServer struct {
	signer.UnimplementedSignerServer
	orgId   string
	keyId   string
	client  *api.ClientWithResponses
	session *api.NewSessionResponse
}

func (s *SignerServer) AddAuthHeader() api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", s.session.Token)
		return nil
	}
}

func (s *SignerServer) PublicKey(ctx context.Context, in *signer.PublicKeyRequest) (*signer.PublicKeyResponse, error) {
	log.Println("PublicKey request received")
	res, err := s.client.GetKeyInOrgWithResponse(ctx, s.orgId, s.keyId, s.AddAuthHeader())

	if err != nil {
		log.Println("Error getting key in org:", err)
		return nil, err
	}

	if res.JSONDefault != nil {
		// TODO: handle refresh
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	log.Printf("PublicKey: %+v", res.JSON200.PublicKey)

	publicKey, err := hex.DecodeString(res.JSON200.PublicKey[2:])
	if err != nil {
		return nil, err
	}

	publicKeyRes := &signer.PublicKeyResponse{
		PublicKey: publicKey,
	}

	return publicKeyRes, nil
}

func sign(ctx context.Context, s *SignerServer, bytes []byte, blsDst *string) ([]byte, error) {
	msg := base64.StdEncoding.EncodeToString(bytes)
	blobSignReq := &api.BlobSignRequest{
		MessageBase64: msg,
		BlsDst:        blsDst,
	}

	log.Println("about to make blob sign request with message: ", msg)
	res, err := s.client.BlobSignWithResponse(ctx, s.orgId, s.keyId, *blobSignReq, s.AddAuthHeader())
	if err != nil {
		return nil, err
	}

	log.Printf("Response body: %s", res.Body)

	if res.JSON200 == nil {
		return nil, fmt.Errorf("unexpected status code: %d", res.StatusCode())
	}

	signature, err := hex.DecodeString(res.JSON200.Signature[2:])
	if err != nil {
		return nil, err
	}

	return signature, nil
}

func (s *SignerServer) Sign(ctx context.Context, in *signer.SignatureRequest) (*signer.SignatureResponse, error) {
	signature, err := sign(ctx, s, in.Message, nil)
	if err != nil {
		return nil, err
	}

	signatureRes := &signer.SignatureResponse{
		Signature: signature,
	}

	return signatureRes, nil
}

func (s *SignerServer) ProofOfPossession(ctx context.Context, in *signer.ProofOfPossessionSignatureRequest) (*signer.ProofOfPossessionSignatureResponse, error) {
	signature, err := sign(ctx, s, in.Message, &popDst)
	if err != nil {
		return nil, err
	}

	signatureRes := &signer.ProofOfPossessionSignatureResponse{
		Signature: signature,
	}

	return signatureRes, nil
}

type TokenData struct {
	api.NewSessionResponse
	OrgID  string `json:"org_id"`
	RoleID string `json:"role_id"`
}

func main() {
	tokenFilePath := os.Getenv("TOKEN_FILE_PATH")
	if tokenFilePath == "" {
		log.Fatal("TOKEN_FILE_PATH environment variable is not set")
	}

	file, err := os.Open(tokenFilePath)
	if err != nil {
		log.Fatalf("failed to open token file: %v", err)
	}
	defer file.Close()

	var tokenData TokenData
	if err := json.NewDecoder(file).Decode(&tokenData); err != nil {
		log.Fatalf("failed to decode token file: %v", err)
	}

	orgId := tokenData.OrgID
	keyId := os.Getenv("KEY_ID")
	if keyId == "" {
		log.Fatal("KEY_ID environment variable is not set")
	}

	endpoint := os.Getenv("SIGNER_ENDPOINT")
	if endpoint == "" {
		log.Fatal("SIGNER_ENDPOINT environment variable is not set")
	}

	client, err := api.NewClientWithResponses(endpoint)
	if err != nil {
		log.Fatalf("failed to create API client: %v", err)
	}

	authData := &api.AuthData{
		EpochNum:   tokenData.SessionInfo.Epoch,
		EpochToken: tokenData.SessionInfo.EpochToken,
		OtherToken: tokenData.SessionInfo.RefreshToken,
	}

	res, err := client.SignerSessionRefreshWithResponse(context.Background(), orgId, *authData, func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", tokenData.Token)
		return nil
	})

	if err != nil {
		log.Fatal("failed to refresh session: %v", err)
	}
	log.Printf("response: %+v", res.JSONDefault)

	if res.JSON200 == nil {
		log.Fatalf("unexpected status code: %d", res.StatusCode())
	}

	signerServer := &SignerServer{
		orgId:   orgId,
		keyId:   keyId,
		client:  client,
		session: res.JSON200,
	}

	grpcServer := grpc.NewServer()
	signer.RegisterSignerServer(grpcServer, signerServer)

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	log.Println("Starting gRPC server on port 50051...")
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}

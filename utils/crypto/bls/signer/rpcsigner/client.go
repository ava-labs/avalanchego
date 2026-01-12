// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	pb "github.com/ava-labs/avalanchego/proto/pb/signer"
)

var _ bls.Signer = (*Client)(nil)

type Client struct {
	client pb.SignerClient
	pk     *bls.PublicKey
	// grpc.ClientConn handles transient connection errors.
	connection *grpc.ClientConn
}

func NewClient(ctx context.Context, url string) (*Client, error) {
	// TODO: figure out the best parameters here given the target block-time
	opts := grpc.WithConnectParams(grpc.ConnectParams{
		Backoff: backoff.DefaultConfig,
		// same as grpc default
		MinConnectTimeout: 20 * time.Second,
	})

	// the rpc-signer client should call a proxy server (on the same machine) that forwards
	// the request to the actual signer instead of relying on tls-credentials
	conn, err := grpc.NewClient(url, opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to create rpc signer client: %w", err)
	}

	client := pb.NewSignerClient(conn)

	pkResponse, err := client.PublicKey(ctx, &pb.PublicKeyRequest{})
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("failed to get public key: %w", err),
			conn.Close(),
		)
	}

	pk, err := bls.PublicKeyFromCompressedBytes(pkResponse.GetPublicKey())
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("failed to parse compressed public key bytes: %w", err),
			conn.Close(),
		)
	}

	return &Client{
		client:     client,
		pk:         pk,
		connection: conn,
	}, nil
}

func (c *Client) PublicKey() *bls.PublicKey {
	return c.pk
}

func (c *Client) Sign(message []byte) (*bls.Signature, error) {
	resp, err := c.client.Sign(context.TODO(), &pb.SignRequest{Message: message})
	if err != nil {
		return nil, fmt.Errorf("failed to sign message: %w", err)
	}

	sig, err := bls.SignatureFromBytes(resp.GetSignature())
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature: %w", err)
	}

	return sig, nil
}

// SignProofOfPossession produces a ProofOfPossession signature.
// See BLS spec for more details.
func (c *Client) SignProofOfPossession(message []byte) (*bls.Signature, error) {
	resp, err := c.client.SignProofOfPossession(context.TODO(), &pb.SignProofOfPossessionRequest{Message: message})
	if err != nil {
		return nil, fmt.Errorf("failed to sign proof of possession: %w", err)
	}

	sigBytes := resp.GetSignature()
	sig, err := bls.SignatureFromBytes(sigBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse signature: %w", err)
	}

	return sig, nil
}

func (c *Client) Shutdown() error {
	if err := c.connection.Close(); err != nil {
		return fmt.Errorf("failed to close connection: %w", err)
	}

	return nil
}

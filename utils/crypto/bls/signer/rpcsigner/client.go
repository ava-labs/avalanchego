// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"
	"fmt"

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
}

func NewClient(ctx context.Context, url string) (*Client, func() error, error) {
	// TODO: figure out the best parameters here given the target block-time
	opts := grpc.WithConnectParams(grpc.ConnectParams{
		Backoff: backoff.DefaultConfig,
	})

	// the rpc-signer client should call a proxy server (on the same machine) that forwards
	// the request to the actual signer instead of relying on tls-credentials
	conn, err := grpc.NewClient(url, opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, func() error { return nil }, fmt.Errorf("failed to create rpc signer client: %w", err)
	}

	cleanup := func() error {
		return conn.Close()
	}

	client := pb.NewSignerClient(conn)

	pubkeyResponse, err := client.PublicKey(ctx, &pb.PublicKeyRequest{})
	if err != nil {
		return nil, cleanup, err
	}

	pkBytes := pubkeyResponse.GetPublicKey()
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return nil, cleanup, err
	}

	return &Client{
		client: client,
		pk:     pk,
	}, cleanup, nil
}

func (c *Client) PublicKey() *bls.PublicKey {
	return c.pk
}

// Sign a message. The [Client] already handles transient connection errors. If this method fails, it will
// render the client in an unusable state and the client should be discarded.
func (c *Client) Sign(message []byte) (*bls.Signature, error) {
	resp, err := c.client.Sign(context.TODO(), &pb.SignRequest{Message: message})
	if err != nil {
		return nil, err
	}

	sigBytes := resp.GetSignature()
	return bls.SignatureFromBytes(sigBytes)
}

// [SignProofOfPossession] has the same behavior as [Sign] but will product a different signature.
// See BLS spec for more details.
func (c *Client) SignProofOfPossession(message []byte) (*bls.Signature, error) {
	resp, err := c.client.SignProofOfPossession(context.TODO(), &pb.SignProofOfPossessionRequest{Message: message})
	if err != nil {
		return nil, err
	}

	sigBytes := resp.GetSignature()
	return bls.SignatureFromBytes(sigBytes)
}

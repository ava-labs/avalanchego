// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	pb "github.com/ava-labs/avalanchego/proto/pb/signer"
)

var _ bls.Signer = (*Client)(nil)

type Client struct {
	client pb.SignerClient
	pk     *bls.PublicKey
}

func NewClient(ctx context.Context, conn *grpc.ClientConn) (*Client, error) {
	client := pb.NewSignerClient(conn)

	pubkeyResponse, err := client.PublicKey(ctx, &pb.PublicKeyRequest{})
	if err != nil {
		return nil, err
	}

	pkBytes := pubkeyResponse.GetPublicKey()
	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		return nil, err
	}

	return &Client{
		client: client,
		pk:     pk,
	}, nil
}

func (c *Client) PublicKey() *bls.PublicKey {
	return c.pk
}

func (c *Client) Sign(message []byte) (*bls.Signature, error) {
	resp, err := c.client.Sign(context.TODO(), &pb.SignRequest{Message: message})
	if err != nil {
		return nil, err
	}
	signature := resp.GetSignature()

	return bls.SignatureFromBytes(signature)
}

func (c *Client) SignProofOfPossession(message []byte) (*bls.Signature, error) {
	resp, err := c.client.SignProofOfPossession(context.TODO(), &pb.SignProofOfPossessionRequest{Message: message})
	if err != nil {
		return nil, err
	}
	signature := resp.GetSignature()

	return bls.SignatureFromBytes(signature)
}

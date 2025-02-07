// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"context"
	"errors"

	"google.golang.org/grpc"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	pb "github.com/ava-labs/avalanchego/proto/pb/signer"
)

var (
	_                   bls.Signer = (*Client)(nil)
	ErrorEmptyPublicKey            = errors.New("empty public key")
	ErrorEmptySignature            = errors.New("empty signature")
)

type Client struct {
	client pb.SignerClient
	conn   *grpc.ClientConn
	pk     *bls.PublicKey
}

func NewClient(conn *grpc.ClientConn) (*Client, error) {
	client := pb.NewSignerClient(conn)

	pubkeyResponse, err := client.PublicKey(context.TODO(), &pb.PublicKeyRequest{})
	if err != nil {
		conn.Close()
		return nil, err
	}

	pkBytes := pubkeyResponse.GetPublicKey()

	if pkBytes == nil {
		conn.Close()
		return nil, ErrorEmptyPublicKey
	}

	pk, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	if err != nil {
		conn.Close()
		return nil, err
	}

	return &Client{
		client: client,
		conn:   conn,
		pk:     pk,
	}, nil
}

func (c *Client) Close() error {
	return c.conn.Close()
}

type signatureResponse interface {
	GetSignature() []byte
}

func getSignatureFromResponse[T signatureResponse](resp T) (*bls.Signature, error) {
	signature := resp.GetSignature()
	if len(signature) == 0 {
		return nil, ErrorEmptySignature
	}

	return bls.SignatureFromBytes(signature)
}

func (c *Client) PublicKey() *bls.PublicKey {
	return c.pk
}

func (c *Client) Sign(message []byte) (*bls.Signature, error) {
	resp, err := c.client.Sign(context.TODO(), &pb.SignRequest{Message: message})
	if err != nil {
		return nil, err
	}

	return getSignatureFromResponse(resp)
}

func (c *Client) SignProofOfPossession(message []byte) (*bls.Signature, error) {
	resp, err := c.client.SignProofOfPossession(context.TODO(), &pb.SignProofOfPossessionRequest{Message: message})
	if err != nil {
		return nil, err
	}
	return getSignatureFromResponse(resp)
}

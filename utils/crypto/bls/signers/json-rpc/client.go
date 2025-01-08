// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package jsonrpc

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/rpc/v2/json2"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

var _ bls.Signer = (*Client)(nil)

type Client struct {
	// http client
	http *http.Client
	url  url.URL
}

func NewClient(url url.URL) *Client {
	return &Client{
		http: &http.Client{},
		url:  url,
	}
}

func (c *Client) call(method string, params []interface{}, result interface{}) error {
	requestBody, err := json2.EncodeClientRequest(method, params)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel() // Ensure the context is canceled to release resources

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url.String(), bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return json2.DecodeClientResponse(resp.Body, result)
}

func (c *Client) PublicKey() *bls.PublicKey {
	reply := new(PublicKeyReply)

	err := c.call("Signer.PublicKey", []interface{}{PublicKeyArgs{}}, reply)
	if err != nil {
		panic(err)
	}

	pk := new(bls.PublicKey)
	pk = pk.Deserialize(reply.PublicKey)

	return pk
}

// Sign [msg] to authorize this message
func (c *Client) Sign(msg []byte) *bls.Signature {
	// request the public key from the json-rpc server
	reply := new(SignReply)
	err := c.call("Signer.Sign", []interface{}{SignArgs{msg}}, reply)
	// TODO: handle this
	if err != nil {
		panic(err)
	}

	// deserialize the public key
	sig := new(bls.Signature)
	sig = sig.Deserialize(reply.Signature)

	// can be nil if the public key is invalid
	return sig
}

// Sign [msg] to prove the ownership
func (c *Client) SignProofOfPossession(msg []byte) *bls.Signature {
	// request the public key from the json-rpc server
	reply := new(SignReply)
	err := c.call("Signer.SignProofOfPossession", []interface{}{SignArgs{msg}}, reply)
	// TODO: handle this
	if err != nil {
		panic(err)
	}

	// deserialize the public key
	sig := new(bls.Signature)
	sig = sig.Deserialize(reply.Signature)

	// can be nil if the public key is invalid
	return sig
}

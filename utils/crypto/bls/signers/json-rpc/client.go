package jsonrpc

import (
	"bytes"
	"net/http"
	"net/url"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/gorilla/rpc/v2/json2"
)

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

func (client *Client) call(method string, params []interface{}, result interface{}) error {
	requestBody, err := json2.EncodeClientRequest(method, params)

	if err != nil {
		return err
	}

	resp, err := client.http.Post(client.url.String(), "application/json", bytes.NewBuffer(requestBody))

	if err != nil {
		return err
	}

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

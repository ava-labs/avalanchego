package jsonrpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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

// PublicKey returns the public key that corresponds to this secret
// key.
func (client *Client) call(method string, params interface{}, result interface{}) error {
	requestBody, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return err
	}

	resp, err := client.http.Post(client.url.String(), "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var response map[string]interface{}
	if err := json.Unmarshal(body, &response); err != nil {
		return err
	}

	if response["error"] != nil {
		return fmt.Errorf("JSON-RPC error: %v", response["error"])
	}

	resultBytes, err := json.Marshal(response["result"])
	if err != nil {
		return err
	}

	return json.Unmarshal(resultBytes, result)
}

func (c *Client) PublicKey() *bls.PublicKey {
	// request the public key from the json-rpc server
	reply := new(PublicKeyReply)
	err := c.call("Signer.PublicKey", PublicKeyArgs{}, reply)

	// TODO: handle this
	if err != nil {
		panic(err)
	}

	// deserialize the public key
	pk := new(bls.PublicKey)
	pk = pk.Deserialize(reply.PublicKey)

	// can be nil if the public key is invalid
	return pk
}

// Sign [msg] to authorize this message
func (c *Client) Sign(msg []byte) *bls.Signature {
	// request the public key from the json-rpc server
	reply := new(SignReply)
	err := c.call("Signer.Sign", SignArgs{msg}, reply)

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
	err := c.call("Signer.SignProofOfPossession", SignArgs{msg}, reply)

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

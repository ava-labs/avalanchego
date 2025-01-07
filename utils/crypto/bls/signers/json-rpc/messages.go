// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package jsonrpc

type PublicKeyArgs struct{}

type SignArgs struct {
	Msg []byte
}

type SignProofOfPossessionArgs struct {
	Msg []byte
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

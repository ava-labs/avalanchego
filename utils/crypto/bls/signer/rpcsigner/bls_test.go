// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpcsigner

import (
	"net"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"

	pb "github.com/ava-labs/avalanchego/proto/pb/signer"
)

func AggregateAndVerify(publicKeys []*bls.PublicKey, signatures []*bls.Signature, message []byte) (bool, error) {
	aggSig, err := bls.AggregateSignatures(signatures)
	if err != nil {
		return false, err
	}
	aggPK, err := bls.AggregatePublicKeys(publicKeys)
	if err != nil {
		return false, err
	}

	return bls.Verify(aggPK, aggSig, message), nil
}

func NewLocalSigner(require *require.Assertions) *localsigner.LocalSigner {
	sk, err := localsigner.New()
	require.NoError(err)
	return sk
}

type rpcSigner struct {
	*Client
	server *grpc.Server
}

func (r *rpcSigner) Stop() {
	r.Close()
	r.server.Stop()
}

func NewRpcSigner(require *require.Assertions) *rpcSigner {
	serverImpl, err := newServer()
	require.NoError(err)

	server := grpc.NewServer()

	pb.RegisterSignerServer(server, serverImpl)
	lis, err := net.Listen("tcp", ":0")
	require.NoError(err)

	go func() {
		if err := server.Serve(lis); err != nil {
			// Just log the error since we can't return it from the goroutine
			// The error is not critical as it will happen during shutdown
			_ = err
		}
	}()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		server.Stop()
		require.NoError(err)
		return nil
	}

	client, err := NewClient(conn)
	if err != nil {
		server.Stop()
		require.NoError(err)
		return nil
	}

	return &rpcSigner{
		Client: client,
		server: server,
	}
}

func collectN[T any](n int, fn func() T) []T {
	result := make([]T, n)
	for i := 0; i < n; i++ {
		result[i] = fn()
	}
	return result
}

func mapWithError[T any, U any](arr []T, fn func(T) (U, error)) ([]U, error) {
	result := make([]U, len(arr))
	for i, val := range arr {
		newVal, err := fn(val)
		if err != nil {
			return nil, err
		}
		result[i] = newVal
	}
	return result, nil
}

func TestVerifyValidSignature(t *testing.T) {
	require := require.New(t)
	signer := NewRpcSigner(require)
	defer signer.Close()

	msg := []byte("TestVerifyValidSignature local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid := bls.Verify(signer.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageSignature(t *testing.T) {
	require := require.New(t)
	signer := NewRpcSigner(require)
	defer signer.Close()

	msg := []byte("TestVerifyWrongMessageSignature local signer")
	wrongMsg := []byte("TestVerifyWrongMessageSignature local signer with wrong message")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid := bls.Verify(signer.PublicKey(), sig, wrongMsg)
	require.False(isValid)
}

func TestValidAggregation(t *testing.T) {
	require := require.New(t)
	keyPairs := collectN(2, func() bls.Signer {
		return NewLocalSigner(require)
	})

	keyPairs = append(keyPairs, NewLocalSigner(require))

	msg := []byte("TestValidAggregation local signer")

	sigs, err := mapWithError(keyPairs, func(s bls.Signer) (*bls.Signature, error) {
		return s.Sign(msg)
	})
	require.NoError(err)

	pks, _ := mapWithError(keyPairs, func(s bls.Signer) (*bls.PublicKey, error) {
		return s.PublicKey(), nil
	})

	isValid, err := AggregateAndVerify(pks, sigs, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestSingleKeyAggregation(t *testing.T) {
	require := require.New(t)
	signer := NewRpcSigner(require)

	pks := []*bls.PublicKey{signer.PublicKey()}

	msg := []byte("TestSingleKeyAggregation local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid, err := AggregateAndVerify(pks, []*bls.Signature{sig}, msg)
	require.NoError(err)
	require.True(isValid)
}

func TestVerifyValidProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewRpcSigner(require)

	msg := []byte("TestVerifyValidProofOfPossession local signer")

	sig, err := signer.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewRpcSigner(require)

	msg := []byte("TestVerifyWrongMessageProofOfPossession local signer")
	wrongMsg := []byte("TestVerifyWrongMessageProofOfPossession local signer with wrong message")

	sig, err := signer.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), sig, wrongMsg)
	require.False(isValid)
}

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

func NewLocalSigner(t *testing.T) *localsigner.LocalSigner {
	sk, err := localsigner.New()
	require.NoError(t, err)
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

func NewRPCSigner(require *require.Assertions) *rpcSigner {
	serverImpl, err := newServer()
	require.NoError(err)

	server := grpc.NewServer()

	pb.RegisterSignerServer(server, serverImpl)
	lis, err := net.Listen("tcp", "[::1]:0")
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
	signer := NewRPCSigner(require)
	defer signer.Close()

	msg := []byte("TestVerifyValidSignature local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	isValid := bls.Verify(signer.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageSignature(t *testing.T) {
	require := require.New(t)
	signer := NewRPCSigner(require)
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
		return NewLocalSigner(t)
	})

	keyPairs = append(keyPairs, NewLocalSigner(t))

	msg := []byte("TestValidAggregation local signer")

	sigs, err := mapWithError(keyPairs, func(s bls.Signer) (*bls.Signature, error) {
		return s.Sign(msg)
	})
	require.NoError(err)

	pks, _ := mapWithError(keyPairs, func(s bls.Signer) (*bls.PublicKey, error) {
		return s.PublicKey(), nil
	})

	aggSig, err := bls.AggregateSignatures(sigs)
	require.NoError(err)
	aggPK, err := bls.AggregatePublicKeys(pks)
	require.NoError(err)

	isValid := bls.Verify(aggPK, aggSig, msg)
	require.True(isValid)
}

func TestSingleKeyAggregation(t *testing.T) {
	require := require.New(t)
	signer := NewRPCSigner(require)

	pks := []*bls.PublicKey{signer.PublicKey()}

	msg := []byte("TestSingleKeyAggregation local signer")

	sig, err := signer.Sign(msg)
	require.NoError(err)

	aggSig, err := bls.AggregateSignatures([]*bls.Signature{sig})
	require.NoError(err)
	aggPK, err := bls.AggregatePublicKeys(pks)
	require.NoError(err)

	isValid := bls.Verify(aggPK, aggSig, msg)
	require.True(isValid)
}

func TestVerifyValidProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewRPCSigner(require)

	msg := []byte("TestVerifyValidProofOfPossession local signer")

	sig, err := signer.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), sig, msg)
	require.True(isValid)
}

func TestVerifyWrongMessageProofOfPossession(t *testing.T) {
	require := require.New(t)
	signer := NewRPCSigner(require)

	msg := []byte("TestVerifyWrongMessageProofOfPossession local signer")
	wrongMsg := []byte("TestVerifyWrongMessageProofOfPossession local signer with wrong message")

	sig, err := signer.SignProofOfPossession(msg)
	require.NoError(err)

	isValid := bls.VerifyProofOfPossession(signer.PublicKey(), sig, wrongMsg)
	require.False(isValid)
}

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/ava-labs/avalanchego/api/grpcclient"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/xsvm"
)

func TestFoo(t *testing.T) {
	t.Log("starting test")
	uri := "localhost:9650"
	chainID, err := ids.FromString("282TEeusjd6T8LjSSGnCR7KKFyMuwzsaYvZjGigxxEcRwcA3Dz")
	require.NoError(t, err)
	conn, err := grpc.NewClient(
		uri,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(
			grpcclient.PrefixChainIDUnaryClientInterceptor(chainID),
		),
		grpc.WithStreamInterceptor(
			grpcclient.PrefixChainIDStreamClientInterceptor(chainID),
		),
	)
	require.NoError(t, err)

	client := xsvm.NewPingClient(conn)

	t.Log("sending request")
	msg := "foobar"
	reply, err := client.Ping(context.Background(), &xsvm.PingRequest{
		Message: msg,
	})
	require.NoError(t, err)
	require.Equal(t, msg, reply.Message)
	t.Logf("success: %s", reply.Message)
	time.Sleep(time.Minute)
}

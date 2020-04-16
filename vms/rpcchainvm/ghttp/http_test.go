// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ghttp

import (
	"bytes"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"

	"github.com/ava-labs/gecko/utils/json"
	"github.com/ava-labs/gecko/vms/rpcchainvm/ghttp/proto"
)

const (
	bufSize = 1 << 20
)

type service struct{}

type Args struct{}

type Reply struct {
	Called bool
}

func (s *service) Call(r *http.Request, args *Args, reply *Reply) error {
	reply.Called = true
	return nil
}

func TestRPC(t *testing.T) {
	rpcServer := rpc.NewServer()
	codec := json.NewCodec()
	rpcServer.RegisterCodec(codec, "application/json")
	rpcServer.RegisterCodec(codec, "application/json;charset=UTF-8")
	rpcServer.RegisterService(&service{}, "service")

	listener := bufconn.Listen(bufSize)
	server := grpc.NewServer()
	proto.RegisterHTTPServer(server, NewServer(rpcServer))
	go func() {
		if err := server.Serve(listener); err != nil {
			log.Fatal(err)
		}
	}()

	dialer := grpc.WithContextDialer(
		func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		})

	ctx := context.Background()
	conn, err := grpc.DialContext(ctx, "", dialer, grpc.WithInsecure())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	rpcClient := NewClient(proto.NewHTTPClient(conn))

	req := &Args{}
	buf, err := json2.EncodeClientRequest("service.call", req)
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.NewBuffer(buf)
	r, err := http.NewRequest("POST", "http://localhost:8080/", body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	rpcClient.ServeHTTP(w, r)

	res := &Reply{}
	if err := json2.DecodeClientResponse(w.Body, res); err != nil {
		t.Fatal(err)
	}

	if !res.Called {
		t.Fatalf("Should have called the function")
	}
}

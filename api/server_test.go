// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gorilla/rpc/v2"
	"github.com/gorilla/rpc/v2/json2"

	"github.com/ava-labs/gecko/snow/engine/common"
	"github.com/ava-labs/gecko/utils/logging"
)

type Service struct{ called bool }

type Args struct{}

type Reply struct{}

func (s *Service) Call(_ *http.Request, args *Args, reply *Reply) error {
	s.called = true
	return nil
}

func TestCall(t *testing.T) {
	s := Server{}
	s.Initialize(logging.NoLog{}, logging.NoFactory{}, "localhost", 8080)

	serv := &Service{}
	newServer := rpc.NewServer()
	newServer.RegisterCodec(json2.NewCodec(), "application/json")
	newServer.RegisterCodec(json2.NewCodec(), "application/json;charset=UTF-8")
	newServer.RegisterService(serv, "test")

	if err := s.AddRoute(&common.HTTPHandler{Handler: newServer}, new(sync.RWMutex), "vm/lol", "", logging.NoLog{}); err != nil {
		t.Fatal(err)
	}

	buf, err := json2.EncodeClientRequest("test.Call", &Args{})
	if err != nil {
		t.Fatal(err)
	}

	writer := httptest.NewRecorder()
	body := bytes.NewBuffer(buf)
	headers := map[string]string{
		"Content-Type": "application/json",
	}
	s.Call(writer, "POST", "lol", "", body, headers)

	if !serv.called {
		t.Fatalf("Should have been called")
	}
}

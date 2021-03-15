package indexer

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type mockHTTPClient struct {
	Req   *http.Request
	Resp  *http.Response
	Error error
}

func (r *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	r.Req = req
	return r.Resp, r.Error
}

func TestClientUrl(t *testing.T) {
	cl := NewClient("http://localhost:1234/abc")

	mock := &mockHTTPClient{}
	cl.client = mock

	if cl.toURL(IndexTypeTransactions) != "http://localhost:1234/abc/tx" {
		t.Fatal("invalid toUrl")
	}
	if cl.toURL(IndexTypeVertices) != "http://localhost:1234/abc/vtx" {
		t.Fatal("invalid toUrl")
	}
}

type WrappedReaderCloser struct {
	reader *bufio.Reader
}

func (w *WrappedReaderCloser) Read(p []byte) (n int, err error) {
	return w.reader.Read(p)
}

func (w *WrappedReaderCloser) Close() error {
	return nil
}

type TestClientOutput struct {
	Out1 string
}

func TestClientSend(t *testing.T) {
	cl := NewClient("http://localhost:1234/abc")

	mock := &mockHTTPClient{}
	cl.client = mock

	type Args struct {
		Arg1 string
	}

	testingArgumentValue := fmt.Sprintf("%v", time.Now().String())

	outbody := buildTestOutput(&TestClientOutput{Out1: testingArgumentValue})

	args := &Args{Arg1: "arg1"}
	var output *TestClientOutput
	var outputif interface{} = &output

	mock.Resp = &http.Response{
		Body:          &WrappedReaderCloser{reader: bufio.NewReader(&outbody)},
		ContentLength: int64(outbody.Len()),
	}

	rc, err := cl.send(IndexTypeTransactions, "testMethod", args, &outputif)
	if rc != 0 || err != nil {
		t.Fatal("invalid send")
	}

	bodydata, _ := cl.copy(mock.Req.Body)
	cr := &clientRequest{
		RPC:    "2.0",
		Method: "index." + "testMethod",
		ID:     1,
		Params: [1]interface{}{args},
	}
	crdata, _ := json.Marshal(cr)
	if bodydata.String() != string(crdata) {
		t.Fatal("body request invalid")
	}

	if output.Out1 != testingArgumentValue {
		t.Fatal("output.Out1 invalid")
	}
}

func buildTestOutput(o *TestClientOutput) bytes.Buffer {
	clientResp, _ := json.Marshal(&o)
	jclientResp := json.RawMessage(clientResp)
	cresp := &clientResponse{
		RPC:    "2.0",
		ID:     0,
		Result: &jclientResp,
	}
	bits, _ := json.Marshal(&cresp)
	var outbody bytes.Buffer
	outbody.Write(bits)
	return outbody
}

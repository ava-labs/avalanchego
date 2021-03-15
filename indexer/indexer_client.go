package indexer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	contentType = "application/json"
)

type httpClient interface {
	Do(*http.Request) (*http.Response, error)
}

type realHTTPClient struct {
	client *http.Client
}

func (r *realHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return r.client.Do(req)
}

func newHTTPClient(client *http.Client) httpClient {
	return &realHTTPClient{client: client}
}

type Client struct {
	rawurl string
	client httpClient
}

// NewClient creates a client that uses the given RPC client.
func NewClient(rawurl string) *Client {
	return &Client{
		rawurl: rawurl,
		client: newHTTPClient(&http.Client{}),
	}
}

type clientRequest struct {
	RPC    string         `json:"jsonrpc"`
	Method string         `json:"method"`
	Params [1]interface{} `json:"params"`
	ID     uint64         `json:"id"`
}

type clientError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type clientResponse struct {
	RPC    string           `json:"jsonrpc"`
	ID     uint64           `json:"id"`
	Result *json.RawMessage `json:"result"`
	Error  *clientError     `json:"error"`
}

type IndexType uint32

func (t IndexType) String() string {
	switch t {
	case IndexTypeTransactions:
		return "tx"
	case IndexTypeVertices:
		return "vtx"
	}
	return ""
}

const (
	IndexTypeTransactions IndexType = 1
	IndexTypeVertices     IndexType = 2
)

func (c *Client) toURL(indexType IndexType) string {
	return fmt.Sprintf("%s/%s", c.rawurl, indexType.String())
}

func (c *Client) copy(reader io.Reader) (*bytes.Buffer, error) {
	var outb bytes.Buffer
	rbits := make([]byte, 10*1024)
	_, err := io.CopyBuffer(&outb, reader, rbits)
	if err != nil {
		return nil, err
	}
	return &outb, err
}

func (c *Client) getClientRequest(method string, args interface{}) (*bytes.Buffer, error) {
	cr := &clientRequest{
		RPC:    "2.0",
		Method: "index." + method,
		ID:     1,
		Params: [1]interface{}{args},
	}

	bits, err := json.Marshal(cr)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	_, err = buf.Write(bits)
	if err != nil {
		return nil, err
	}
	return &buf, nil
}

func (c *Client) getRequest(indexType IndexType, err error, buf io.Reader) (*http.Request, error) {
	req, err := http.NewRequest("POST", c.toURL(indexType), buf)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", contentType)
	return req, nil
}

func (c *Client) send(indexType IndexType, method string, args interface{}, output *interface{}) (int, error) {
	buf, err := c.getClientRequest(method, args)
	if err != nil {
		return 0, err
	}

	req, err := c.getRequest(indexType, err, buf)
	if err != nil {
		return 0, err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, err
	}

	outb, err := c.copy(resp.Body)
	if err != nil {
		return 0, err
	}
	err = resp.Body.Close()
	if err != nil {
		return 0, err
	}

	cresp := &clientResponse{}
	err = json.Unmarshal(outb.Bytes(), cresp)
	if err != nil {
		return 0, err
	}

	if cresp.Error != nil {
		return cresp.Error.Code, fmt.Errorf(cresp.Error.Message)
	}

	if cresp.Result != nil {
		err = json.Unmarshal(*cresp.Result, output)
		if err != nil {
			return 0, err
		}
	}

	return 0, nil
}

func (c *Client) GetContainerRange(args *GetContainerRange, indexType IndexType) ([]*FormattedContainer, int, error) {
	var response []*FormattedContainer
	var responceif interface{} = &response
	rc, err := c.send(indexType, "getContainerRange", &args, &responceif)
	return response, rc, err
}

func (c *Client) GetContainerByIndex(args *GetContainer, indexType IndexType) (*FormattedContainer, int, error) {
	var response *FormattedContainer
	var responceif interface{} = &response
	rc, err := c.send(indexType, "getContainerByIndex", &args, &responceif)
	return response, rc, err
}

func (c *Client) GetLastAccepted(args *GetLastAcceptedArgs, indexType IndexType) (*FormattedContainer, int, error) {
	var response *FormattedContainer
	var responceif interface{} = &response
	rc, err := c.send(indexType, "getLastAccepted", &args, &responceif)
	return response, rc, err
}

func (c *Client) GetIndex(args *GetIndexArgs, indexType IndexType) (*GetIndexResponse, int, error) {
	var response *GetIndexResponse
	var responceif interface{} = &response
	rc, err := c.send(indexType, "getIndex", &args, &responceif)
	return response, rc, err
}

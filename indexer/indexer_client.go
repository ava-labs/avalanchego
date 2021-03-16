package indexer

import (
	"fmt"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

type Client struct {
	requester rpc.EndpointRequester
}

// NewClient creates a client.
func NewClient(uri string, indexType fmt.Stringer, requestTimeout time.Duration) *Client {
	return &Client{
		requester: rpc.NewEndpointRequester(uri, fmt.Sprintf("%s/%s", "ext/index", indexType.String()), "index", requestTimeout),
	}
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

func (c *Client) send(method string, args interface{}, output *interface{}) error {
	return c.requester.SendRequest(method, args, output)
}

func (c *Client) GetContainerRange(args *GetContainerRange) ([]*FormattedContainer, error) {
	var response []*FormattedContainer
	var responceif interface{} = &response
	err := c.send("getContainerRange", &args, &responceif)
	return response, err
}

func (c *Client) GetContainerByIndex(args *GetContainer) (*FormattedContainer, error) {
	var response *FormattedContainer
	var responceif interface{} = &response
	err := c.send("getContainerByIndex", &args, &responceif)
	return response, err
}

func (c *Client) GetLastAccepted(args *GetLastAcceptedArgs) (*FormattedContainer, error) {
	var response *FormattedContainer
	var responceif interface{} = &response
	err := c.send("getLastAccepted", &args, &responceif)
	return response, err
}

func (c *Client) GetIndex(args *GetIndexArgs) (*GetIndexResponse, error) {
	var response *GetIndexResponse
	var responceif interface{} = &response
	err := c.send("getIndex", &args, &responceif)
	return response, err
}

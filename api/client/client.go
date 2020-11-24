package client

import (
	"time"

	"github.com/ava-labs/avalanche-testing/avalanche_client/apis/avm"
	"github.com/ava-labs/avalanche-testing/avalanche_client/apis/platform"
	"github.com/ava-labs/avalanchego/api/admin"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/api/info"
	"github.com/ava-labs/avalanchego/api/ipcs"
	"github.com/ava-labs/avalanchego/api/keystore"
)

const (
	XChain = "X"
)

type Client struct {
	admin    *admin.Client
	xChain   *avm.Client
	health   *health.Client
	info     *info.Client
	ipcs     *ipcs.Client
	keystore *keystore.Client
	platform *platform.Client
}

// Returns a Client for interacting with the P Chain endpoint
func NewClient(uri string, requestTimeout time.Duration) *Client {
	return &Client{
		admin:    admin.NewClient(uri, requestTimeout),
		xChain:   avm.NewClient(uri, XChain, requestTimeout),
		health:   health.NewClient(uri, requestTimeout),
		info:     info.NewClient(uri, requestTimeout),
		ipcs:     ipcs.NewClient(uri, requestTimeout),
		keystore: keystore.NewClient(uri, requestTimeout),
		platform: platform.NewClient(uri, requestTimeout),
	}
}

func (c *Client) PChainAPI() *platform.Client {
	return c.platform
}

func (c *Client) XChainAPI() *avm.Client {
	return c.xChain
}

func (c *Client) InfoAPI() *info.Client {
	return c.info
}

func (c *Client) HealthAPI() *health.Client {
	return c.health
}

func (c *Client) IpcsAPI() *ipcs.Client {
	return c.ipcs
}

func (c *Client) KeystoreAPI() *keystore.Client {
	return c.keystore
}

func (c *Client) AdminAPI() *admin.Client {
	return c.admin
}

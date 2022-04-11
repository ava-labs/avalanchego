// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	cjson "github.com/ava-labs/avalanchego/utils/json"
	"github.com/ava-labs/avalanchego/utils/rpc"
	"github.com/ethereum/go-ethereum/log"
)

// Interface compliance
var _ Client = (*client)(nil)

// Client interface for interacting with EVM [chain]
type Client interface {
	IssueTx(ctx context.Context, txBytes []byte) (ids.ID, error)
	GetAtomicTxStatus(ctx context.Context, txID ids.ID) (Status, error)
	GetAtomicTx(ctx context.Context, txID ids.ID) ([]byte, error)
	GetAtomicUTXOs(ctx context.Context, addrs []string, sourceChain string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error)
	ListAddresses(ctx context.Context, userPass api.UserPass) ([]string, error)
	ExportKey(ctx context.Context, userPass api.UserPass, addr string) (string, string, error)
	ImportKey(ctx context.Context, userPass api.UserPass, privateKey string) (string, error)
	Import(ctx context.Context, userPass api.UserPass, to string, sourceChain string) (ids.ID, error)
	ExportAVAX(ctx context.Context, userPass api.UserPass, amount uint64, to string) (ids.ID, error)
	Export(ctx context.Context, userPass api.UserPass, amount uint64, to string, assetID string) (ids.ID, error)
	StartCPUProfiler(ctx context.Context) (bool, error)
	StopCPUProfiler(ctx context.Context) (bool, error)
	MemoryProfile(ctx context.Context) (bool, error)
	LockProfile(ctx context.Context) (bool, error)
	SetLogLevel(ctx context.Context, level log.Lvl) (bool, error)
	GetVMConfig(ctx context.Context) (*Config, error)
}

// Client implementation for interacting with EVM [chain]
type client struct {
	requester      rpc.EndpointRequester
	adminRequester rpc.EndpointRequester
}

// NewClient returns a Client for interacting with EVM [chain]
func NewClient(uri, chain string) Client {
	return &client{
		requester:      rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/bc/%s/avax", chain), "avax"),
		adminRequester: rpc.NewEndpointRequester(uri, fmt.Sprintf("/ext/bc/%s/admin", chain), "admin"),
	}
}

// NewCChainClient returns a Client for interacting with the C Chain
func NewCChainClient(uri string) Client {
	return NewClient(uri, "C")
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *client) IssueTx(ctx context.Context, txBytes []byte) (ids.ID, error) {
	res := &api.JSONTxID{}
	txStr, err := formatting.EncodeWithChecksum(formatting.Hex, txBytes)
	if err != nil {
		return res.TxID, fmt.Errorf("problem hex encoding bytes: %w", err)
	}
	err = c.requester.SendRequest(ctx, "issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

// GetAtomicTxStatus returns the status of [txID]
func (c *client) GetAtomicTxStatus(ctx context.Context, txID ids.ID) (Status, error) {
	res := &GetAtomicTxStatusReply{}
	err := c.requester.SendRequest(ctx, "getAtomicTxStatus", &api.JSONTxID{
		TxID: txID,
	}, res)
	return res.Status, err
}

// GetAtomicTx returns the byte representation of [txID]
func (c *client) GetAtomicTx(ctx context.Context, txID ids.ID) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest(ctx, "getAtomicTx", &api.GetTxArgs{
		TxID:     txID,
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, err
	}

	return formatting.Decode(formatting.Hex, res.Tx)
}

// GetAtomicUTXOs returns the byte representation of the atomic UTXOs controlled by [addresses]
// from [sourceChain]
func (c *client) GetAtomicUTXOs(ctx context.Context, addrs []string, sourceChain string, limit uint32, startAddress, startUTXOID string) ([][]byte, api.Index, error) {
	res := &api.GetUTXOsReply{}
	err := c.requester.SendRequest(ctx, "getUTXOs", &api.GetUTXOsArgs{
		Addresses:   addrs,
		SourceChain: sourceChain,
		Limit:       cjson.Uint32(limit),
		StartIndex: api.Index{
			Address: startAddress,
			UTXO:    startUTXOID,
		},
		Encoding: formatting.Hex,
	}, res)
	if err != nil {
		return nil, api.Index{}, err
	}

	utxos := make([][]byte, len(res.UTXOs))
	for i, utxo := range res.UTXOs {
		b, err := formatting.Decode(formatting.Hex, utxo)
		if err != nil {
			return nil, api.Index{}, err
		}
		utxos[i] = b
	}
	return utxos, res.EndIndex, nil
}

// ListAddresses returns all addresses on this chain controlled by [user]
func (c *client) ListAddresses(ctx context.Context, user api.UserPass) ([]string, error) {
	res := &api.JSONAddresses{}
	err := c.requester.SendRequest(ctx, "listAddresses", &user, res)
	return res.Addresses, err
}

// ExportKey returns the private key corresponding to [addr] controlled by [user]
// in both Avalanche standard format and hex format
func (c *client) ExportKey(ctx context.Context, user api.UserPass, addr string) (string, string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest(ctx, "exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addr,
	}, res)
	return res.PrivateKey, res.PrivateKeyHex, err
}

// ImportKey imports [privateKey] to [user]
func (c *client) ImportKey(ctx context.Context, user api.UserPass, privateKey string) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

// Import sends an import transaction to import funds from [sourceChain] and
// returns the ID of the newly created transaction
func (c *client) Import(ctx context.Context, user api.UserPass, to, sourceChain string) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "import", &ImportArgs{
		UserPass:    user,
		To:          to,
		SourceChain: sourceChain,
	}, res)
	return res.TxID, err
}

// ExportAVAX sends AVAX from this chain to the address specified by [to].
// Returns the ID of the newly created atomic transaction
func (c *client) ExportAVAX(
	ctx context.Context,
	user api.UserPass,
	amount uint64,
	to string,
) (ids.ID, error) {
	return c.Export(ctx, user, amount, to, "AVAX")
}

// Export sends an asset from this chain to the P/C-Chain.
// After this tx is accepted, the AVAX must be imported to the P/C-chain with an importTx.
// Returns the ID of the newly created atomic transaction
func (c *client) Export(
	ctx context.Context,
	user api.UserPass,
	amount uint64,
	to string,
	assetID string,
) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "export", &ExportArgs{
		ExportAVAXArgs: ExportAVAXArgs{
			UserPass: user,
			Amount:   cjson.Uint64(amount),
			To:       to,
		},
		AssetID: assetID,
	}, res)
	return res.TxID, err
}

func (c *client) StartCPUProfiler(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest(ctx, "startCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *client) StopCPUProfiler(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest(ctx, "stopCPUProfiler", struct{}{}, res)
	return res.Success, err
}

func (c *client) MemoryProfile(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest(ctx, "memoryProfile", struct{}{}, res)
	return res.Success, err
}

func (c *client) LockProfile(ctx context.Context) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest(ctx, "lockProfile", struct{}{}, res)
	return res.Success, err
}

// SetLogLevel dynamically sets the log level for the C Chain
func (c *client) SetLogLevel(ctx context.Context, level log.Lvl) (bool, error) {
	res := &api.SuccessResponse{}
	err := c.adminRequester.SendRequest(ctx, "setLogLevel", &SetLogLevelArgs{
		Level: level.String(),
	}, res)
	return res.Success, err
}

// GetVMConfig returns the current config of the VM
func (c *client) GetVMConfig(ctx context.Context) (*Config, error) {
	res := &ConfigReply{}
	err := c.adminRequester.SendRequest(ctx, "getVMConfig", struct{}{}, res)
	return res.Config, err
}

// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/api"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/crypto/secp256k1"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/rpc"

	cjson "github.com/ava-labs/avalanchego/utils/json"
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
	ExportKey(ctx context.Context, userPass api.UserPass, addr string) (*secp256k1.PrivateKey, string, error)
	ImportKey(ctx context.Context, userPass api.UserPass, privateKey *secp256k1.PrivateKey) (string, error)
	Import(ctx context.Context, userPass api.UserPass, to string, sourceChain string) (ids.ID, error)
	ExportAVAX(ctx context.Context, userPass api.UserPass, amount uint64, to string) (ids.ID, error)
	Export(ctx context.Context, userPass api.UserPass, amount uint64, to string, assetID string) (ids.ID, error)
	StartCPUProfiler(ctx context.Context) error
	StopCPUProfiler(ctx context.Context) error
	MemoryProfile(ctx context.Context) error
	LockProfile(ctx context.Context) error
	SetLogLevel(ctx context.Context, level log.Lvl) error
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
		requester:      rpc.NewEndpointRequester(fmt.Sprintf("%s/ext/bc/%s/avax", uri, chain)),
		adminRequester: rpc.NewEndpointRequester(fmt.Sprintf("%s/ext/bc/%s/admin", uri, chain)),
	}
}

// NewCChainClient returns a Client for interacting with the C Chain
func NewCChainClient(uri string) Client {
	return NewClient(uri, "C")
}

// IssueTx issues a transaction to a node and returns the TxID
func (c *client) IssueTx(ctx context.Context, txBytes []byte) (ids.ID, error) {
	res := &api.JSONTxID{}
	txStr, err := formatting.Encode(formatting.Hex, txBytes)
	if err != nil {
		return res.TxID, fmt.Errorf("problem hex encoding bytes: %w", err)
	}
	err = c.requester.SendRequest(ctx, "avax.issueTx", &api.FormattedTx{
		Tx:       txStr,
		Encoding: formatting.Hex,
	}, res)
	return res.TxID, err
}

// GetAtomicTxStatus returns the status of [txID]
func (c *client) GetAtomicTxStatus(ctx context.Context, txID ids.ID) (Status, error) {
	res := &GetAtomicTxStatusReply{}
	err := c.requester.SendRequest(ctx, "avax.getAtomicTxStatus", &api.JSONTxID{
		TxID: txID,
	}, res)
	return res.Status, err
}

// GetAtomicTx returns the byte representation of [txID]
func (c *client) GetAtomicTx(ctx context.Context, txID ids.ID) ([]byte, error) {
	res := &api.FormattedTx{}
	err := c.requester.SendRequest(ctx, "avax.getAtomicTx", &api.GetTxArgs{
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
	err := c.requester.SendRequest(ctx, "avax.getUTXOs", &api.GetUTXOsArgs{
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
	err := c.requester.SendRequest(ctx, "avax.listAddresses", &user, res)
	return res.Addresses, err
}

// ExportKey returns the private key corresponding to [addr] controlled by [user]
// in both Avalanche standard format and hex format
func (c *client) ExportKey(ctx context.Context, user api.UserPass, addr string) (*secp256k1.PrivateKey, string, error) {
	res := &ExportKeyReply{}
	err := c.requester.SendRequest(ctx, "avax.exportKey", &ExportKeyArgs{
		UserPass: user,
		Address:  addr,
	}, res)
	return res.PrivateKey, res.PrivateKeyHex, err
}

// ImportKey imports [privateKey] to [user]
func (c *client) ImportKey(ctx context.Context, user api.UserPass, privateKey *secp256k1.PrivateKey) (string, error) {
	res := &api.JSONAddress{}
	err := c.requester.SendRequest(ctx, "avax.importKey", &ImportKeyArgs{
		UserPass:   user,
		PrivateKey: privateKey,
	}, res)
	return res.Address, err
}

// Import sends an import transaction to import funds from [sourceChain] and
// returns the ID of the newly created transaction
func (c *client) Import(ctx context.Context, user api.UserPass, to, sourceChain string) (ids.ID, error) {
	res := &api.JSONTxID{}
	err := c.requester.SendRequest(ctx, "avax.import", &ImportArgs{
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
	err := c.requester.SendRequest(ctx, "avax.export", &ExportArgs{
		ExportAVAXArgs: ExportAVAXArgs{
			UserPass: user,
			Amount:   cjson.Uint64(amount),
			To:       to,
		},
		AssetID: assetID,
	}, res)
	return res.TxID, err
}

func (c *client) StartCPUProfiler(ctx context.Context) error {
	return c.adminRequester.SendRequest(ctx, "admin.startCPUProfiler", struct{}{}, &api.EmptyReply{})
}

func (c *client) StopCPUProfiler(ctx context.Context) error {
	return c.adminRequester.SendRequest(ctx, "admin.stopCPUProfiler", struct{}{}, &api.EmptyReply{})
}

func (c *client) MemoryProfile(ctx context.Context) error {
	return c.adminRequester.SendRequest(ctx, "admin.memoryProfile", struct{}{}, &api.EmptyReply{})
}

func (c *client) LockProfile(ctx context.Context) error {
	return c.adminRequester.SendRequest(ctx, "admin.lockProfile", struct{}{}, &api.EmptyReply{})
}

// SetLogLevel dynamically sets the log level for the C Chain
func (c *client) SetLogLevel(ctx context.Context, level log.Lvl) error {
	return c.adminRequester.SendRequest(ctx, "admin.setLogLevel", &SetLogLevelArgs{
		Level: level.String(),
	}, &api.EmptyReply{})
}

// GetVMConfig returns the current config of the VM
func (c *client) GetVMConfig(ctx context.Context) (*Config, error) {
	res := &ConfigReply{}
	err := c.adminRequester.SendRequest(ctx, "admin.getVMConfig", struct{}{}, res)
	return res.Config, err
}

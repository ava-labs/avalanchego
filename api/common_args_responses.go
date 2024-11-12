// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"encoding/json"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"

	avajson "github.com/ava-labs/avalanchego/utils/json"
)

// This file contains structs used in arguments and responses in services

// EmptyReply indicates that an api doesn't have a response to return.
type EmptyReply struct{}

// JSONTxID contains the ID of a transaction
type JSONTxID struct {
	TxID ids.ID `json:"txID"`
}

// UserPass contains a username and a password
type UserPass struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// JSONAddress contains an address
type JSONAddress struct {
	Address string `json:"address"`
}

// JSONAddresses contains a list of address
type JSONAddresses struct {
	Addresses []string `json:"addresses"`
}

// JSONChangeAddr is the address change is sent to, if any
type JSONChangeAddr struct {
	ChangeAddr string `json:"changeAddr"`
}

// JSONTxIDChangeAddr is a tx ID and change address
type JSONTxIDChangeAddr struct {
	JSONTxID
	JSONChangeAddr
}

// JSONFromAddrs is a list of addresses to send funds from
type JSONFromAddrs struct {
	From []string `json:"from"`
}

// JSONSpendHeader is 3 arguments to a method that spends (including those with tx fees)
// 1) The username/password
// 2) The addresses used in the method
// 3) The address to send change to
type JSONSpendHeader struct {
	UserPass
	JSONFromAddrs
	JSONChangeAddr
}

// GetBlockArgs is the parameters supplied to the GetBlock API
type GetBlockArgs struct {
	BlockID  ids.ID              `json:"blockID"`
	Encoding formatting.Encoding `json:"encoding"`
}

// GetBlockByHeightArgs is the parameters supplied to the GetBlockByHeight API
type GetBlockByHeightArgs struct {
	Height   avajson.Uint64      `json:"height"`
	Encoding formatting.Encoding `json:"encoding"`
}

// GetBlockResponse is the response object for the GetBlock API.
type GetBlockResponse struct {
	Block json.RawMessage `json:"block"`
	// If GetBlockResponse.Encoding is formatting.Hex, GetBlockResponse.Block is
	// the string representation of the block under hex encoding.
	// If GetBlockResponse.Encoding is formatting.JSON, GetBlockResponse.Block
	// is the actual block returned as a JSON.
	Encoding formatting.Encoding `json:"encoding"`
}

type GetHeightResponse struct {
	Height avajson.Uint64 `json:"height"`
}

// FormattedBlock defines a JSON formatted struct containing a block in Hex
// format
type FormattedBlock struct {
	Block    string              `json:"block"`
	Encoding formatting.Encoding `json:"encoding"`
}

type GetTxArgs struct {
	TxID     ids.ID              `json:"txID"`
	Encoding formatting.Encoding `json:"encoding"`
}

// GetTxReply defines an object containing a single [Tx] object along with Encoding
type GetTxReply struct {
	// If [GetTxArgs.Encoding] is [Hex], [Tx] is the string representation of
	// the tx under hex encoding.
	// If [GetTxArgs.Encoding] is [JSON], [Tx] is the actual tx, which will be
	// returned as JSON to the caller.
	Tx       json.RawMessage     `json:"tx"`
	Encoding formatting.Encoding `json:"encoding"`
}

// FormattedTx defines a JSON formatted struct containing a Tx as a string
type FormattedTx struct {
	Tx       string              `json:"tx"`
	Encoding formatting.Encoding `json:"encoding"`
}

// Index is an address and an associated UTXO.
// Marks a starting or stopping point when fetching UTXOs. Used for pagination.
type Index struct {
	Address string `json:"address"` // The address as a string
	UTXO    string `json:"utxo"`    // The UTXO ID as a string
}

// GetUTXOsArgs are arguments for passing into GetUTXOs.
// Gets the UTXOs that reference at least one address in [Addresses].
// Returns at most [limit] addresses.
// If specified, [SourceChain] is the chain where the atomic UTXOs were exported from. If empty,
// or the Chain ID of this VM is specified, then GetUTXOs fetches the native UTXOs.
// If [limit] == 0 or > [maxUTXOsToFetch], fetches up to [maxUTXOsToFetch].
// [StartIndex] defines where to start fetching UTXOs (for pagination.)
// UTXOs fetched are from addresses equal to or greater than [StartIndex.Address]
// For address [StartIndex.Address], only UTXOs with IDs greater than [StartIndex.UTXO] will be returned.
// If [StartIndex] is omitted, gets all UTXOs.
// If GetUTXOs is called multiple times, with our without [StartIndex], it is not guaranteed
// that returned UTXOs are unique. That is, the same UTXO may appear in the response of multiple calls.
type GetUTXOsArgs struct {
	Addresses   []string            `json:"addresses"`
	SourceChain string              `json:"sourceChain"`
	Limit       avajson.Uint32      `json:"limit"`
	StartIndex  Index               `json:"startIndex"`
	Encoding    formatting.Encoding `json:"encoding"`
}

// GetUTXOsReply defines the GetUTXOs replies returned from the API
type GetUTXOsReply struct {
	// Number of UTXOs returned
	NumFetched avajson.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []string `json:"utxos"`
	// The last UTXO that was returned, and the address it corresponds to.
	// Used for pagination. To get the rest of the UTXOs, call GetUTXOs
	// again and set [StartIndex] to this value.
	EndIndex Index `json:"endIndex"`
	// Encoding specifies the encoding format the UTXOs are returned in
	Encoding formatting.Encoding `json:"encoding"`
}

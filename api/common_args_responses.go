// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package api

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/utils/json"
)

// This file contains structs used in arguments and responses in services

// SuccessResponse indicates success of an API call
type SuccessResponse struct {
	Success bool `json:"success"`
}

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

type GetTxArgs struct {
	TxID     ids.ID              `json:"txID"`
	Encoding formatting.Encoding `json:"encoding"`
}

// GetTxReply defines an object containing a single [Tx] object along with Encoding
type GetTxReply struct {
	// If [GetTxArgs.Encoding] is [Hex] or [CB58], [Tx] is the string
	// representation of the tx under that encoding.
	// If [GetTxArgs.Encoding] is [JSON], [Tx] is the actual tx, which will be
	// returned as JSON to the caller.
	Tx       interface{}         `json:"tx"`
	Encoding formatting.Encoding `json:"encoding"`
}

// FormattedTx defines a JSON formatted struct containing a Tx in CB58 format
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
	Limit       json.Uint32         `json:"limit"`
	StartIndex  Index               `json:"startIndex"`
	Encoding    formatting.Encoding `json:"encoding"`
}

// GetUTXOsReply defines the GetUTXOs replies returned from the API
type GetUTXOsReply struct {
	// Number of UTXOs returned
	NumFetched json.Uint64 `json:"numFetched"`
	// The UTXOs
	UTXOs []string `json:"utxos"`
	// The last UTXO that was returned, and the address it corresponds to.
	// Used for pagination. To get the rest of the UTXOs, call GetUTXOs
	// again and set [StartIndex] to this value.
	EndIndex Index `json:"endIndex"`
	// Encoding specifies the encoding format the UTXOs are returned in
	Encoding formatting.Encoding `json:"encoding"`
}

package vmargs

import (
	"github.com/ava-labs/avalanchego/utils/json"
	cjson "github.com/ava-labs/avalanchego/utils/json"
)

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
	Addresses   []string    `json:"addresses"`
	SourceChain string      `json:"sourceChain"`
	Limit       json.Uint32 `json:"limit"`
	StartIndex  Index       `json:"startIndex"`
	Encoding    string      `json:"encoding"`
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
	Encoding string `json:"encoding"`
}

// BuildGenesisArgs are arguments for BuildGenesis
type BuildGenesisArgs struct {
	NetworkID   cjson.Uint32               `json:"networkID"`
	GenesisData map[string]AssetDefinition `json:"genesisData"`
	Encoding    string                     `json:"encoding"`
}

// AssetDefinition ...
type AssetDefinition struct {
	Name         string                   `json:"name"`
	Symbol       string                   `json:"symbol"`
	Denomination cjson.Uint8              `json:"denomination"`
	InitialState map[string][]interface{} `json:"initialState"`
	Memo         string                   `json:"memo"`
}

// BuildGenesisReply is the reply from BuildGenesis
type BuildGenesisReply struct {
	Bytes    string `json:"bytes"`
	Encoding string `json:"encoding"`
}

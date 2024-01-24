// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"time"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/cmd/flaghelpers"
)

const (
	IPPortKey      = "ip"
	PeerLimitKey   = "peer-limit"
	ConcurrencyKey = "concurrency"
	UriKey         = "uri"
	NetworkIDKey   = "network-id"
	ChainIDKey     = "chain-id"
	SubnetIDKey    = "subnet-id"
	DeadlineKey    = "deadline"
	OutputFileKey  = "output-file"
	QueryTypeKey   = "query-type"
)

func BuildViper(args []string) (*viper.Viper, error) {
	return flaghelpers.BuildViper("network", func(fs *pflag.FlagSet) {
		fs.StringSlice(IPPortKey, nil, "Specify the IP of the node to send a request to. If no IPs are specified, queries all validator IPs.")
		fs.Int(PeerLimitKey, 0, "Specify a cap on the number of peers that will be queried. Defaults to 0, which performs all queries.")
		fs.Int(ConcurrencyKey, 10, "Specify the number of concurrent test to use.")
		fs.String(UriKey, "https://api.avax.network", "Specify the endpoint to send standard Avalanche API requests.")
		fs.Uint32(NetworkIDKey, 1, "NetworkID of the nodes to query.")
		fs.String(SubnetIDKey, "11111111111111111111111111111111LpoYY", "SubnetID to use to specify the weights of requested nodes.")
		fs.String(ChainIDKey, "2q9e4r6Mu3U68nU1fYjgbR6JvwrRx36CohpAX5UQxse55x1Q5", "ChainID to send queries.")
		fs.String(QueryTypeKey, "pull_query", "Specify the request type to send.")
		fs.Duration(DeadlineKey, 3*time.Second, "Specify deadline to receive a response from a peer.")
		fs.String(OutputFileKey, "", "Specify the location to write results to a CSV file.")
	}, args)
}

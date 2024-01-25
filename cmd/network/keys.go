// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"github.com/spf13/pflag"
	"github.com/spf13/viper"

	"github.com/ava-labs/avalanchego/cmd/flaghelpers"
)

const (
	LogLevelKey     = "log-level"
	IPPortKey       = "ip"
	PeerLimitKey    = "peer-limit"
	ConcurrencyKey  = "concurrency"
	UriKey          = "uri"
	NetworkIDKey    = "network-id"
	SubnetIDKey     = "subnet-id"
	VersionRegexKey = "version-regex"
	OutputFileKey   = "output-file"
)

func BuildViper(args []string) (*viper.Viper, error) {
	return flaghelpers.BuildViper("network", func(fs *pflag.FlagSet) {
		fs.String(LogLevelKey, "info", "Specify the log level")
		fs.StringSlice(IPPortKey, nil, "Specify the IP of the node to send a request to. If no IPs are specified, queries all validator IPs.")
		fs.Int(PeerLimitKey, 0, "Specify a cap on the number of peers that will be queried. Defaults to 0, which performs all queries.")
		fs.Int(ConcurrencyKey, 10, "Specify the number of concurrent test to use.")
		fs.String(UriKey, "https://api.avax.network", "Specify the endpoint to send standard Avalanche API requests.")
		fs.Uint32(NetworkIDKey, 1, "NetworkID of the nodes to query.")
		fs.String(SubnetIDKey, "11111111111111111111111111111111LpoYY", "SubnetID to use to specify the weights of requested nodes.")
		fs.String(VersionRegexKey, "", "Specify a regex to filter the versions of nodes that should be queried.")
		fs.String(OutputFileKey, "", "Specify the location to write results to a CSV file.")
	}, args)
}

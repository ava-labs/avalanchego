// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package main

import (
	"context"
	"log"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/platformvm"
	"github.com/ava-labs/avalanchego/wallet/subnet/primary"
)

func main() {
	c := platformvm.NewClient(primary.LocalAPIURI)
	vdrs, err := c.GetValidatorsAt(context.Background(), constants.PrimaryNetworkID, 0)
	log.Fatal(vdrs, err)
}

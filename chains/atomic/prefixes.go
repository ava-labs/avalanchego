// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package atomic

import (
	"bytes"

	"github.com/chain4travel/caminogo/database"
	"github.com/chain4travel/caminogo/database/prefixdb"
	"github.com/chain4travel/caminogo/ids"
)

var (
	inboundSmallerValuePrefix = []byte{0}
	inboundSmallerIndexPrefix = []byte{1}
	inboundLargerValuePrefix  = []byte{2}
	inboundLargerIndexPrefix  = []byte{3}

	// inbound and outbound have their smaller and larger values swapped
	inbound = prefixes{
		smallerValuePrefix: inboundSmallerValuePrefix,
		smallerIndexPrefix: inboundSmallerIndexPrefix,
		largerValuePrefix:  inboundLargerValuePrefix,
		largerIndexPrefix:  inboundLargerIndexPrefix,
	}
	outbound = prefixes{
		smallerValuePrefix: inboundLargerValuePrefix,
		smallerIndexPrefix: inboundLargerIndexPrefix,
		largerValuePrefix:  inboundSmallerValuePrefix,
		largerIndexPrefix:  inboundSmallerIndexPrefix,
	}
)

type prefixes struct {
	smallerValuePrefix []byte
	smallerIndexPrefix []byte
	largerValuePrefix  []byte
	largerIndexPrefix  []byte
}

func (p *prefixes) getValueDB(myChainID, peerChainID ids.ID, db database.Database) database.Database {
	if bytes.Compare(myChainID[:], peerChainID[:]) == -1 {
		return prefixdb.New(p.smallerValuePrefix, db)
	}
	return prefixdb.New(p.largerValuePrefix, db)
}

func (p *prefixes) getValueAndIndexDB(myChainID, peerChainID ids.ID, db database.Database) (database.Database, database.Database) {
	var valueDB, indexDB database.Database
	if bytes.Compare(myChainID[:], peerChainID[:]) == -1 {
		valueDB = prefixdb.New(p.smallerValuePrefix, db)
		indexDB = prefixdb.New(p.smallerIndexPrefix, db)
	} else {
		valueDB = prefixdb.New(p.largerValuePrefix, db)
		indexDB = prefixdb.New(p.largerIndexPrefix, db)
	}
	return valueDB, indexDB
}

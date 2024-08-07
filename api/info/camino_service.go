// Copyright (C) 2022-2024, Chain4Travel AG. All rights reserved.
// See the file LICENSE for licensing terms.

package info

import "net/http"

// GetGenesisBytesReply contains the response metadata for GetGenesisBytes
type GetGenesisBytesReply struct {
	GenesisBytes []byte `json:"genesisBytes"`
}

// GetGenesisBytes returns genesis bytes
func (i *Info) GetGenesisBytes(_ *http.Request, _ *struct{}, reply *GetGenesisBytesReply) error {
	i.log.Debug("Info: GetGenesisBytes called")
	reply.GenesisBytes = i.GenesisBytes
	return nil
}

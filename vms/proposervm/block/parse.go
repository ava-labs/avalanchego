// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto/x509"
	"errors"
	"time"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

var errWrongVersion = errors.New("wrong version")

func Parse(bytes []byte) (Block, error) {
	block := StatelessPostForkBlock{
		id:    hashing.ComputeHash256Array(bytes),
		bytes: bytes,
	}
	parsedVersion, err := c.Unmarshal(bytes, &block)
	if err != nil {
		return nil, err
	}
	if parsedVersion != version {
		return nil, errWrongVersion
	}

	block.timestamp = time.Unix(block.StatelessBlock.Timestamp, 0)
	cert, err := x509.ParseCertificate(block.StatelessBlock.Certificate)
	if err != nil {
		return nil, err
	}
	block.cert = cert
	block.proposer = hashing.ComputeHash160Array(hashing.ComputeHash256(cert.Raw))
	return &block, nil
}

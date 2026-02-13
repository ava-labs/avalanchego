// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package genesis

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/ava-labs/avalanchego/utils/formatting"
	"github.com/ava-labs/avalanchego/vms/example/xsvm/genesis"
)

var errUnknownEncoding = errors.New("unknown encoding")

func Command() *cobra.Command {
	c := &cobra.Command{
		Use:   "genesis",
		Short: "Creates a chain's genesis and prints it to stdout",
		RunE:  genesisFunc,
	}
	flags := c.Flags()
	AddFlags(flags)
	return c
}

func genesisFunc(c *cobra.Command, args []string) error {
	flags := c.Flags()
	config, err := ParseFlags(flags, args)
	if err != nil {
		return err
	}

	genesisBytes, err := genesis.Codec.Marshal(genesis.CodecVersion, config.Genesis)
	if err != nil {
		return err
	}

	switch config.Encoding {
	case binaryEncoding:
		_, err = os.Stdout.Write(genesisBytes)
		return err
	case hexEncoding:
		encoded, err := formatting.Encode(formatting.Hex, genesisBytes)
		if err != nil {
			return err
		}
		_, err = fmt.Println(encoded)
		return err
	default:
		return fmt.Errorf("%w: %q", errUnknownEncoding, config.Encoding)
	}
}

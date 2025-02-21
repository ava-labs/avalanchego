// The flagdoc binary generates documentation of AvalancheGo flags.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/ava-labs/avalanchego/config"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	cmd := &cobra.Command{
		Use: `AvalancheGo`, // shown as ## level header
		Short: `This is shown under the title

and can be multiline`,
		Long: `This is shown under the "Synopsis" header

and can be multiline`,
	}
	*cmd.Flags() = *config.BuildFlagSet()
	return doc.GenMarkdown(cmd, out)
}

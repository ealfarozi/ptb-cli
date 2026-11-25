// Command ptb is the PaperToBillion CLI.
package main

import (
	"os"

	"github.com/ealfarozi/ptb-cli/cmd"
)

func main() { os.Exit(cmd.Execute()) }

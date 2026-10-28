// Command exits exits before the handshake, with a reason on stderr.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, `configuration: unknown filter "zz"`)
	os.Exit(3)
}

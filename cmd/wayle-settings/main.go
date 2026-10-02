// Command wayle-settings is the settings GUI (crates/wayle-settings):
// every change a runtime override, saved to runtime.toml as it lands.
package main

import (
	"fmt"
	"os"

	"github.com/stubbedev/wayle/shell/settings"
)

func main() {
	if err := settings.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

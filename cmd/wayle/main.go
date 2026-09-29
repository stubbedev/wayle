// Command wayle is the Go rewrite's entry point. Subcommands port
// over one by one; anything not ported yet exits with an error naming
// it instead of silently doing nothing.
package main

import (
	"fmt"
	"os"

	"github.com/stubbedev/wayle/shell/bar"
)

const usage = `wayle (Go rewrite)

Usage:
  wayle shell        run the shell (bar only for now)

Not ported yet: audio, config, icons, launch, lock, media, notify,
panel, power, recorder, screenshot, systray, toast, wallpaper, widget.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "shell":
		err = bar.Run()
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		err = fmt.Errorf("%q is not ported to the Go shell yet", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "wayle:", err)
		os.Exit(1)
	}
}

// Command schemadoc regenerates config/schemadoc_gen.go; run it through
// `go generate ./config`.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/stubbedev/wayle/config/internal/schemadoc"
)

func main() {
	out, err := schemadoc.Generate(".", "config")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(".", schemadoc.OutputName), out, 0o644); err != nil { //nolint:gosec // a checked-in source file
		log.Fatal(err)
	}
}

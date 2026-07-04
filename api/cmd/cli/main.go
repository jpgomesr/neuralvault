// Command neuralvault (also built as nv) is a minimal CLI for exercising the
// NeuralVault pipeline end to end: ingesting a document and querying it back
// via semantic search. It talks exclusively to the NeuralVault HTTP API.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	prog := filepath.Base(os.Args[0])

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %s <ingest|query> [flags] ...\n", prog) //nolint:errcheck
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "ingest":
		err = runIngest(prog, os.Args[2:])
	case "query":
		err = runQuery(prog, os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1]) //nolint:errcheck
		fmt.Fprintf(os.Stderr, "usage: %s <ingest|query> [flags] ...\n", prog) //nolint:errcheck
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) //nolint:errcheck
		os.Exit(1)
	}
}

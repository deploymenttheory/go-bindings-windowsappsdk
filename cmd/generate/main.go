// Command generate drives the go-bindings-windowsappsdk pipeline:
//
//	generate fetch-metadata    download the Windows App SDK winmds from NuGet
//	generate fetch-bootstrap   download the redistributable bootstrapper + headers
//	generate ingest            project the winmds into per-namespace .wasdkmeta.json files
//	generate validate          structural integrity checks over the metadata
//	generate resolve           run every type reference through the typemap
//	generate bindings          emit the Go bindings from the metadata (self-cleaning)
//	generate list              list the ingested namespaces with construct counts
//
// The stages are separate commands rather than one because they have different
// costs and different reasons to run: fetching touches the network and changes a
// pin, ingest is pure and its output is committed, and validate is what CI runs
// to prove the committed output is coherent.
package main

import (
	"fmt"
	"os"
)

// modulePath is this module's import path root.
const modulePath = "github.com/deploymenttheory/go-bindings-windowsappsdk"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "fetch-metadata":
		err = runFetchMetadata(os.Args[2:])
	case "fetch-bootstrap":
		err = runFetchBootstrap(os.Args[2:])
	case "ingest":
		err = runIngest(os.Args[2:])
	case "validate":
		err = runValidate(os.Args[2:])
	case "resolve":
		err = runResolve(os.Args[2:])
	case "bindings":
		err = runBindings(os.Args[2:])
	case "list":
		err = runList(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: generate <command> [flags]

commands:
  fetch-metadata   download the Windows App SDK winmds into metadata/winmd
  fetch-bootstrap  download the redistributable bootstrapper into metadata/bootstrap
  ingest           project the winmds into per-namespace .wasdkmeta.json files
  validate         structural integrity checks over the metadata
  resolve          run every type reference through the typemap and report
  bindings         emit the Go bindings from the metadata (self-cleaning)
  list             list the ingested namespaces with construct counts`)
}

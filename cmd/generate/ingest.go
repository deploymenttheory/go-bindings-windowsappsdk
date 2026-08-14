package main

// The ingest stage: winmds in, one committed JSON file per namespace out.
//
// Everything downstream of it is independent of ECMA-335, and the output is
// committed so that a metadata change is a reviewable diff rather than something
// only visible in the generated Go.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/ingest"
	winmd "github.com/deploymenttheory/go-winmd/pkg/winmd"
	"github.com/deploymenttheory/go-winmd/pkg/nuget"
)

// defaultWinmdDir holds the committed winmds and their PROVENANCE.json.
func defaultWinmdDir() string { return filepath.Join("metadata", "winmd") }

// defaultMetadataDir holds the committed IR.
func defaultMetadataDir() string { return filepath.Join("metadata", "wasdk") }

// openSources opens every winmd listed in the directory's PROVENANCE.json.
//
// The provenance is the pin, and therefore the source of truth for which files
// participate: it records the component and version each winmd came from, and
// components version independently of the meta-package. Globbing the directory
// instead would silently ingest a file someone had dropped in by hand.
func openSources(winmdDir string) ([]ingest.Source, error) {
	provenancePath := filepath.Join(winmdDir, "PROVENANCE.json")
	records, err := nuget.ReadProvenance(provenancePath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w (run 'generate fetch-metadata')", provenancePath, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s lists no winmds", provenancePath)
	}
	sources := make([]ingest.Source, 0, len(records))
	for _, record := range records {
		name := filepath.Base(filepath.FromSlash(record.File))
		file, err := winmd.Open(filepath.Join(winmdDir, name))
		if err != nil {
			return nil, fmt.Errorf("%s (listed in PROVENANCE.json): %w", name, err)
		}
		sources = append(sources, ingest.Source{Name: name, Version: record.Version, File: file})
	}
	return sources, nil
}

func runIngest(args []string) error {
	flags := flag.NewFlagSet("ingest", flag.ExitOnError)
	winmdDir := flags.String("winmd-dir", defaultWinmdDir(), "directory of the committed winmds + PROVENANCE.json")
	outDir := flags.String("out", defaultMetadataDir(), "output directory for .wasdkmeta.json files")
	winrtMetadata := flags.String("winrt-metadata", "", "override the Windows.* metadata directory (default: the pinned go-bindings-winrt module)")
	namespaceFilter := flags.String("namespace", "", "comma-separated namespace filter (full names); empty = all")
	verbose := flags.Bool("v", false, "print every diagnostic")
	if err := flags.Parse(args); err != nil {
		return err
	}

	sources, err := openSources(*winmdDir)
	if err != nil {
		return err
	}
	externalSet, err := external.Load(*winrtMetadata)
	if err != nil {
		return err
	}
	fmt.Printf("Windows.* universe: %d types from %s %s\n",
		externalSet.Len(), external.ModulePath, externalSet.Version)

	ingester, err := ingest.New(sources, externalSet)
	if err != nil {
		return err
	}
	namespaces, err := ingester.Ingest()
	if err != nil {
		return err
	}

	filter := map[string]bool{}
	for _, name := range strings.Split(*namespaceFilter, ",") {
		if name = strings.TrimSpace(name); name != "" {
			filter[name] = true
		}
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		return err
	}
	// A filtered run must not leave a stale file behind claiming to be current;
	// an unfiltered one owns the directory outright.
	if len(filter) == 0 {
		if err := pruneMetadata(*outDir, namespaces); err != nil {
			return err
		}
	}
	written := 0
	for _, meta := range namespaces {
		if len(filter) > 0 && !filter[meta.Namespace] {
			continue
		}
		if err := wasdkmeta.Write(*outDir, meta); err != nil {
			return err
		}
		written++
	}

	if *verbose {
		for _, diagnostic := range ingester.Diagnostics {
			fmt.Fprintln(os.Stderr, "diagnostic:", diagnostic)
		}
	}
	fmt.Printf("ingested %d namespaces → %s (%d diagnostics)\n", written, *outDir, len(ingester.Diagnostics))
	for _, line := range ingest.DiagnosticSummary(ingester.Diagnostics) {
		fmt.Println(line)
	}
	return nil
}

// pruneMetadata removes committed namespace files this run did not produce, so
// a namespace that disappears from the winmds disappears from the tree too
// rather than lingering as output nothing generates any more.
func pruneMetadata(dir string, namespaces []*wasdkmeta.NamespaceMeta) error {
	current := make(map[string]bool, len(namespaces))
	for _, meta := range namespaces {
		current[wasdkmeta.FileName(meta.Namespace)] = true
	}
	existing, err := filepath.Glob(filepath.Join(dir, "*"+wasdkmeta.Extension))
	if err != nil {
		return err
	}
	for _, path := range existing {
		if current[filepath.Base(path)] {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		fmt.Printf("pruned %s (no longer in the winmds)\n", filepath.Base(path))
	}
	return nil
}

func runList(args []string) error {
	flags := flag.NewFlagSet("list", flag.ExitOnError)
	metadataDir := flags.String("metadata", defaultMetadataDir(), "directory of .wasdkmeta.json files")
	if err := flags.Parse(args); err != nil {
		return err
	}
	namespaces, err := wasdkmeta.ReadAll(*metadataDir)
	if err != nil {
		return err
	}
	if len(namespaces) == 0 {
		return fmt.Errorf("no %s files in %s (run 'generate ingest')", wasdkmeta.Extension, *metadataDir)
	}
	var totals [5]int
	for _, meta := range namespaces {
		fmt.Printf("%-58s %4d ifaces %4d classes %4d enums %4d structs %4d delegates\n",
			meta.Namespace, len(meta.Interfaces), len(meta.Classes), len(meta.Enums),
			len(meta.Structs), len(meta.Delegates))
		totals[0] += len(meta.Interfaces)
		totals[1] += len(meta.Classes)
		totals[2] += len(meta.Enums)
		totals[3] += len(meta.Structs)
		totals[4] += len(meta.Delegates)
	}
	fmt.Printf("%-58s %4d ifaces %4d classes %4d enums %4d structs %4d delegates\n",
		fmt.Sprintf("total (%d namespaces)", len(namespaces)),
		totals[0], totals[1], totals[2], totals[3], totals[4])
	return nil
}

package main

// The bindings stage: committed metadata in, Go out.
//
// Self-cleaning. A generated file from an earlier run that this run does not
// rewrite is pruned, so a renamed or removed construct leaves nothing behind.
// Only files carrying the DO-NOT-EDIT header are ever touched.

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	emitwinui "github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/diagnostics"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

// defaultBindingsDir is the generated tree's root.
func defaultBindingsDir() string { return filepath.Join("bindings", "winui") }

// defaultRootsFile pins which namespaces are generated.
func defaultRootsFile() string { return filepath.Join("metadata", "emit-roots.txt") }

func runBindings(args []string) error {
	flags := flag.NewFlagSet("bindings", flag.ExitOnError)
	metadataDir := flags.String("metadata", defaultMetadataDir(), "directory of .wasdkmeta.json files")
	outDir := flags.String("out", defaultBindingsDir(), "bindings output root")
	rootsFile := flags.String("roots", defaultRootsFile(),
		"committed root-namespace list, read when --namespace is not given (a missing file emits everything loaded)")
	namespaceFilter := flags.String("namespace", "",
		"comma-separated root namespaces; emits them plus their referenced-namespace closure, overriding --roots")
	winrtMetadata := flags.String("winrt-metadata", "", "override the Windows.* metadata directory")
	verbose := flags.Bool("v", false, "print every diagnostic")
	writeBaseline := flags.String("diagnostics", "", "write the diagnostics baseline to this path")
	checkBaseline := flags.String("diagnostics-baseline", "", "fail if any diagnostic is not in this committed baseline")
	if err := flags.Parse(args); err != nil {
		return err
	}

	externalSet, err := external.Load(*winrtMetadata)
	if err != nil {
		return err
	}
	registry, err := pipeline.Load(*metadataDir, externalSet)
	if err != nil {
		return err
	}

	roots := strings.Split(*namespaceFilter, ",")
	if *namespaceFilter == "" {
		if roots, err = pipeline.ReadRootsFile(*rootsFile); err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			roots = nil // no override and no committed list: emit everything loaded
		}
	}
	filter := map[string]bool{}
	for _, name := range roots {
		if name = strings.TrimSpace(name); name != "" {
			filter[name] = true
		}
	}

	generator := emitwinui.New(registry, modulePath, *outDir)
	// A run driven by the committed roots covers the whole pinned surface, so stale
	// files anywhere under the output root are stale. Only an ad-hoc --namespace run
	// prunes conservatively, since it deliberately emits a subset.
	written, err := generator.EmitAll(filter, *namespaceFilter == "")
	if err != nil {
		return err
	}
	diags := generator.Diagnostics
	if *verbose {
		for _, diagnostic := range diags {
			fmt.Fprintln(os.Stderr, "diagnostic:", diagnostic)
		}
	}
	if merged := generator.Clusters().Merged(); len(merged) > 0 {
		for _, representative := range merged {
			members := generator.Clusters().Members(representative)
			fmt.Printf("merged %d mutually recursive namespaces into one package: %s\n",
				len(members), representative)
		}
	}
	if blocked := generator.Blocked(); len(blocked) > 0 {
		total := 0
		for _, targets := range blocked {
			total += len(targets)
		}
		fmt.Printf("severed %d import edges across %d namespaces (references over them degrade)\n",
			total, len(blocked))
	}
	fmt.Printf("emitted %d packages → %s (%d diagnostics)\n", written, *outDir, len(diags))
	for _, line := range summarize(countByKey(diags)) {
		fmt.Println(line)
	}

	if *writeBaseline != "" {
		if err := diagnostics.WriteBaseline(*writeBaseline, diags); err != nil {
			return err
		}
		fmt.Printf("wrote the diagnostics baseline → %s\n", *writeBaseline)
	}
	if *checkBaseline != "" {
		newEntries, err := diagnostics.CheckBaseline(*checkBaseline, diags)
		if err != nil {
			return err
		}
		if len(newEntries) > 0 {
			for _, entry := range newEntries {
				fmt.Fprintln(os.Stderr, "new diagnostic:", entry)
			}
			return fmt.Errorf("%d diagnostics beyond the baseline %s "+
				"(fix them, or rewrite the baseline with --diagnostics after reviewing)",
				len(newEntries), *checkBaseline)
		}
		fmt.Println("diagnostics within baseline")
	}
	return nil
}

// countByKey groups "key: detail" diagnostics by their key.
func countByKey(diagnostics []string) map[string]int {
	counts := map[string]int{}
	for _, diagnostic := range diagnostics {
		key, _, _ := strings.Cut(diagnostic, ":")
		counts[key]++
	}
	return counts
}

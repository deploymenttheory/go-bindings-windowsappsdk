package main

// The resolve stage: run every type reference through the typemap and report what
// the emitter would be able to produce, without producing it.
//
// It exists because the resolution layer is where the decisions are, and the
// emitter is only rendering. Being able to ask "does every reference in the tree
// map to a Go type, and do the imports each package needs have distinct aliases"
// before any Go is written means the emitter can be judged on its output rather
// than on whether it resolved things correctly.

import (
	"flag"
	"fmt"
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

func runResolve(args []string) error {
	flags := flag.NewFlagSet("resolve", flag.ExitOnError)
	metadataDir := flags.String("metadata", defaultMetadataDir(), "directory of .wasdkmeta.json files")
	winrtMetadata := flags.String("winrt-metadata", "", "override the Windows.* metadata directory")
	namespaceFilter := flags.String("namespace", "", "report only this namespace")
	showImports := flags.Bool("imports", false, "list each package's imports with their aliases")
	verbose := flags.Bool("v", false, "print every degradation")
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
	clusters := pipeline.ComputeClusters(registry)
	blocked := pipeline.ComputeBlockedImports(registry, clusters)
	mapper := &typemap.Mapper{
		Registry:   registry,
		ModulePath: modulePath,
		Clusters:   clusters,
		Blocked:    blocked,
	}

	report := &resolution{mapper: mapper, verbose: *verbose}
	for _, meta := range registry.Namespaces {
		if *namespaceFilter != "" && meta.Namespace != *namespaceFilter {
			continue
		}
		report.resolveNamespace(meta, *showImports)
	}

	if merged := clusters.Merged(); len(merged) > 0 {
		fmt.Println("namespaces merged into one package (mutually recursive):")
		for _, representative := range merged {
			members := clusters.Members(representative)
			fmt.Printf("  %s  (%d namespaces)\n", representative, len(members))
			for _, member := range members {
				if member != representative {
					fmt.Printf("      %s\n", member)
				}
			}
		}
		fmt.Println()
	}
	if len(blocked) > 0 {
		fmt.Println("severed import edges (references across these degrade):")
		for _, src := range sortedKeys(blocked) {
			for _, dst := range sortedKeys(blocked[src]) {
				fmt.Printf("  %s → %s\n", src, dst)
			}
		}
		fmt.Println()
	}

	fmt.Printf("resolved %d references: %d to a Go type, %d degraded\n",
		report.total, report.resolved, report.degraded)
	for _, line := range summarize(report.reasons) {
		fmt.Println(line)
	}
	// Two of those counts are upper bounds, and saying so is the difference
	// between a useful report and a misleading one.
	//
	// A closed generic instantiation and a delegate parameter are both resolved
	// through a callback that the EMITTER supplies: it monomorphizes the
	// instantiation, or grounds the handler, into the consuming package and hands
	// back the local type name. Nothing here can do that — there is no package
	// being built to put them in — so every such reference degrades. Most will
	// resolve once the emitter wires those seams.
	if report.reasons["generic-member-skipped"] > 0 || report.reasons["delegate-param-skipped"] > 0 {
		fmt.Printf("\nnote: generic-member-skipped and delegate-param-skipped are upper bounds.\n" +
			"      Both resolve through emitter-supplied callbacks that monomorphize or ground\n" +
			"      the type into the consuming package, and there is no package here to put it in.\n")
	}
	if len(report.aliasClashes) > 0 {
		sort.Strings(report.aliasClashes)
		for _, clash := range report.aliasClashes {
			fmt.Println("error:", clash)
		}
		return fmt.Errorf("%d import alias collisions", len(report.aliasClashes))
	}
	fmt.Println("no import alias collisions")
	return nil
}

// resolution accumulates the outcome of resolving every reference.
type resolution struct {
	mapper  *typemap.Mapper
	verbose bool

	total, resolved, degraded int
	reasons                   map[string]int
	aliasClashes              []string
}

func (r *resolution) resolveNamespace(meta *wasdkmeta.NamespaceMeta, showImports bool) {
	// One import set per package, which is what the emitted file will carry, so
	// a collision here is a collision there.
	imports := typemap.ImportSet{}
	ctx := typemap.Context{Namespace: meta.Namespace}

	// Aliases are keys of the set, so a clash cannot be observed by insertion
	// alone: the second write silently replaces the first. Track path → alias
	// as well, and compare.
	aliasOf := map[string]string{}

	wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
		// Only whole references are counted; the arguments of a generic
		// instantiation are resolved as part of grounding it, not on their own.
		if ref.Kind != "ApiRef" && ref.Kind != "GenericInst" {
			return
		}
		r.total++
		before := len(imports)
		result := r.mapper.GoType(ref, ctx, imports)
		if result.Kind == typemap.KindUnsupported {
			r.degraded++
			if r.reasons == nil {
				r.reasons = map[string]int{}
			}
			key, _, _ := strings.Cut(result.Reason, ":")
			r.reasons[key]++
			if r.verbose {
				fmt.Printf("  [%s] %s\n", meta.Namespace, result.Reason)
			}
			return
		}
		r.resolved++
		if len(imports) == before {
			return
		}
		for alias, entry := range imports {
			if previous, seen := aliasOf[entry.Path]; seen && previous != alias {
				r.aliasClashes = append(r.aliasClashes, fmt.Sprintf(
					"[%s] %s is imported as both %q and %q", meta.Namespace, entry.Path, previous, alias))
			}
			aliasOf[entry.Path] = alias
		}
	})

	// The collision that matters: one alias, two paths. This is what stripping
	// the namespace roots produces for Microsoft.UI.Xaml.Interop and
	// Windows.UI.Xaml.Interop, and it is a compile error in every package that
	// imports both.
	pathOf := map[string]string{}
	for alias, entry := range imports {
		if previous, seen := pathOf[alias]; seen && previous != entry.Path {
			r.aliasClashes = append(r.aliasClashes, fmt.Sprintf(
				"[%s] alias %q points at both %s and %s", meta.Namespace, alias, previous, entry.Path))
		}
		pathOf[alias] = entry.Path
	}

	if showImports && len(imports) > 0 {
		fmt.Printf("%s (%d imports)\n", meta.Namespace, len(imports))
		for _, alias := range sortedKeys(imports) {
			fmt.Printf("  %-28s %s\n", alias, imports[alias].Path)
		}
	}
}

// summarize renders a key → count map as sorted, aligned lines.
func summarize(counts map[string]int) []string {
	lines := make([]string, 0, len(counts))
	for _, key := range sortedKeys(counts) {
		lines = append(lines, fmt.Sprintf("  %-28s %d", key, counts[key]))
	}
	return lines
}

// Package emitwinui emits the Windows App SDK bindings: one Go package per
// Microsoft.* namespace under bindings/winui/, with interfaces dispatching through
// syscall.SyscallN and runtime classes embedding their default interface.
//
// The pipeline is gather (this package's builders, which resolve everything
// through the typemap) → view (pure data) → render (templates that never decide).
package emitwinui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/render"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/shared/fileasm"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// Generator emits the bindings tree for a loaded Registry.
type Generator struct {
	registry *pipeline.Registry
	clusters *pipeline.Clusters
	mapper   *typemap.Mapper
	// outDir is the bindings/winui output root.
	outDir string

	// claimedNames tracks package-level identifiers in the namespace being
	// emitted, preventing collisions between types, enum members, IID vars and
	// constructors. Go has one package-level namespace for all of them.
	claimedNames map[string]bool
	// typeNames pre-claims every type name before any value name, so an enum
	// member or IID var can never steal a name a type needs.
	typeNames map[string]bool

	// Per-namespace generic-instantiation state: pinstByName maps a mangled name
	// to the grounded instantiation, pinstIID to its derived pinterface IID, and
	// pinstQueue is the worklist drained to a fixed point (an instantiation's own
	// methods can request further instantiations).
	pinstByName map[string]*wasdkmeta.TypeRef
	pinstIID    map[string]string
	pinstQueue  []string

	// Per-namespace delegate-grounding state: pdelByName dedups by handler type
	// name, pdelModels accumulates the render models, and pdelImports collects
	// the delegates file's import edges.
	pdelByName  map[string]*wasdkmeta.TypeRef
	pdelModels  []view.DelegateModel
	pdelImports typemap.ImportSet

	// ifaceMethods records each built interface's emitted method surface by
	// MethodDef index. The factory-constructor gather consults it so a
	// package-level wrapper always mirrors the generated interface method
	// exactly, rather than re-deriving a signature that could differ.
	ifaceMethods map[string][]emittedMethod

	// writtenFiles records every path this run produced, so stale generated files
	// from earlier runs can be pruned afterwards.
	writtenFiles map[string]bool

	// referenced collects the namespaces emitted code actually imports — the
	// transitive-closure worklist. Only imports that survive pruning count, so a
	// skipped member never drags a namespace in.
	referenced map[string]bool

	// Diagnostics collects every degradation and skip as "key: detail".
	Diagnostics []string
}

// New builds a Generator. Import cycles among the local namespaces are computed up
// front; references along severed edges degrade instead of importing.
func New(registry *pipeline.Registry, modulePath, outDir string) *Generator {
	// Clusters first: mutually recursive namespaces are merged into single packages,
	// which makes the package graph acyclic, which is why the blocked set that follows
	// comes back empty.
	clusters := pipeline.ComputeClusters(registry)
	return &Generator{
		registry: registry,
		clusters: clusters,
		mapper: &typemap.Mapper{
			Registry:   registry,
			ModulePath: modulePath,
			Clusters:   clusters,
			Blocked:    pipeline.ComputeBlockedImports(registry, clusters),
		},
		outDir: outDir,
	}
}

// Clusters exposes the namespace-to-package mapping, for reporting.
func (g *Generator) Clusters() *pipeline.Clusters { return g.clusters }

// Blocked exposes the severed import edges, for reporting.
func (g *Generator) Blocked() map[string]map[string]bool { return g.mapper.Blocked }

// EmitAll generates every loaded namespace, or — when filter is non-empty — the filter
// set plus the transitive closure of namespaces its EMITTED members reference. The
// closure is not optional: a generated package that imports one which was not generated
// does not compile.
//
// fullSweep prunes stale generated files across the WHOLE output tree rather than only
// inside the directories this run wrote. It must be set whenever the run covers the
// complete pinned surface, because a change to how namespaces map onto packages MOVES
// packages — and the files left at the old paths still compile, so nothing else would
// notice them.
func (g *Generator) EmitAll(filter map[string]bool, fullSweep bool) (int, error) {
	g.writtenFiles = map[string]bool{}
	g.referenced = map[string]bool{}
	emitted := map[string]bool{}

	// The worklist is in PACKAGES, not namespaces: a cluster's members are emitted
	// together or not at all, since they reference each other without imports.
	pending := make([]string, 0, len(g.registry.Namespaces))
	seed := func(namespace string) {
		if g.registry.ByNamespace[namespace] == nil {
			return
		}
		pending = append(pending, g.clusters.PackageOf(namespace))
	}
	if len(filter) > 0 {
		for namespace := range filter {
			if g.registry.ByNamespace[namespace] == nil {
				return 0, fmt.Errorf(
					"root namespace %s is not in the loaded metadata (re-run ingest without a filter)", namespace)
			}
			seed(namespace)
		}
	} else {
		for _, meta := range g.registry.Namespaces {
			seed(meta.Namespace)
		}
	}
	sort.Strings(pending)

	for len(pending) > 0 {
		pkg := pending[0]
		pending = pending[1:]
		if emitted[pkg] {
			continue
		}
		emitted[pkg] = true
		if err := g.emitPackage(pkg); err != nil {
			return len(emitted), fmt.Errorf("emitting %s: %w", pkg, err)
		}
		var discovered []string
		for referenced := range g.referenced {
			if target := g.clusters.PackageOf(referenced); !emitted[target] {
				discovered = append(discovered, target)
			}
		}
		sort.Strings(discovered)
		pending = append(pending, discovered...)
	}

	// Remove generated files from earlier runs this run did not rewrite: renamed
	// constructs, removed namespaces, and packages that moved.
	if err := g.pruneStale(fullSweep || len(filter) == 0); err != nil {
		return len(emitted), err
	}
	sort.Strings(g.Diagnostics)
	return len(emitted), nil
}

// pruneStale deletes generated .go files not written by this run, then removes
// directories left empty. Only files carrying the DO-NOT-EDIT header are ever
// touched, so a hand-written file in the output tree survives.
func (g *Generator) pruneStale(fullSweep bool) error {
	if _, err := os.Stat(g.outDir); err != nil {
		return nil // nothing emitted yet
	}
	emittedDirs := map[string]bool{}
	for path := range g.writtenFiles {
		emittedDirs[filepath.Dir(path)] = true
	}
	var visited []string
	err := filepath.WalkDir(g.outDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			visited = append(visited, path)
			return nil
		}
		if !strings.HasSuffix(path, ".go") || g.writtenFiles[path] {
			return nil
		}
		if !fullSweep && !emittedDirs[filepath.Dir(path)] {
			return nil
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if !strings.HasPrefix(string(content), fileasm.Header) {
			return nil // never touch hand-written files
		}
		return os.Remove(path)
	})
	if err != nil {
		return err
	}
	// Deepest-first: removing empty leaves may empty their parents.
	sort.Sort(sort.Reverse(sort.StringSlice(visited)))
	for _, dir := range visited {
		if entries, readErr := os.ReadDir(dir); readErr == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
	return nil
}

// emitPackage writes one Go package: doc.go plus the per-construct files, each only
// when non-empty.
//
// A package carries every namespace in its cluster. Mutually recursive namespaces are
// emitted together because Go's package is its unit of mutual recursion — see
// pipeline.ComputeClusters — so the fourteen Microsoft.UI.Xaml.* namespaces become one
// package, exactly as they are one assembly in the SDK itself.
//
// Every member's constructs are built before the generic-instantiation worklist is
// drained, because an instantiation requested while building the last member still has
// to land in the same file.
func (g *Generator) emitPackage(pkg string) error {
	members := g.clusters.Members(pkg)
	metas := make([]*wasdkmeta.NamespaceMeta, 0, len(members))
	for _, namespace := range members {
		if meta := g.registry.ByNamespace[namespace]; meta != nil {
			metas = append(metas, meta)
		}
	}
	if len(metas) == 0 {
		return fmt.Errorf("package %s has no loaded namespaces (re-run ingest without a filter)", pkg)
	}

	g.preparePackageClaims(metas)
	packageName := naming.PackageName(pkg)
	packageDir := filepath.Join(g.outDir, filepath.FromSlash(naming.PackagePath(pkg)))

	// Enums: String() uses fmt. writeFile prunes the import when the package has no
	// enums, and the body when there is nothing to write.
	enumImports := typemap.ImportSet{"fmt": {Path: "fmt"}}
	var enumBody strings.Builder
	for _, meta := range metas {
		for _, model := range g.buildEnumModels(meta) {
			if err := renderInto(&enumBody, render.Enum, model); err != nil {
				return err
			}
		}
	}
	if err := g.writeFile(packageDir, packageName+"_enums.go", packageName, enumImports, enumBody.String()); err != nil {
		return err
	}

	structImports := typemap.ImportSet{}
	var structBody strings.Builder
	for _, meta := range metas {
		for _, model := range g.buildStructModels(meta, structImports) {
			if err := renderInto(&structBody, render.Struct, model); err != nil {
				return err
			}
		}
	}
	if err := g.writeFile(packageDir, packageName+"_structs.go", packageName, structImports, structBody.String()); err != nil {
		return err
	}

	interfaceImports := typemap.ImportSet{}
	var interfaceBody strings.Builder
	for _, meta := range metas {
		for _, model := range g.buildInterfaceModels(meta, interfaceImports) {
			if err := renderInto(&interfaceBody, render.Interface, model); err != nil {
				return err
			}
		}
	}
	if err := g.writeFile(packageDir, packageName+"_interfaces.go", packageName, interfaceImports, interfaceBody.String()); err != nil {
		return err
	}

	classImports := typemap.ImportSet{}
	var classBody strings.Builder
	for _, meta := range metas {
		for _, model := range g.buildClassModels(meta, classImports) {
			if err := renderInto(&classBody, render.Class, model); err != nil {
				return err
			}
		}
	}
	if err := g.writeFile(packageDir, packageName+"_classes.go", packageName, classImports, classBody.String()); err != nil {
		return err
	}

	// Generic instantiations requested by everything above, plus the transitive ones
	// their synthesized methods surface. Drained only after every member has run, and
	// resolved in the representative's namespace — which is the same package, so
	// qualification is identical whichever member asked.
	pinterfaceImports := typemap.ImportSet{}
	var pinterfaceBody strings.Builder
	for _, model := range g.buildPinterfaceModels(metas[0], pinterfaceImports) {
		if err := renderInto(&pinterfaceBody, render.Interface, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_pinterfaces.go", packageName, pinterfaceImports, pinterfaceBody.String()); err != nil {
		return err
	}

	// Delegate handlers grounded by the event accessors and delegate parameters above.
	// The models were built eagerly at request time; sort for determinism, since
	// requests arrive in map-iteration order.
	sort.Slice(g.pdelModels, func(i, j int) bool { return g.pdelModels[i].TypeName < g.pdelModels[j].TypeName })
	var delegateBody strings.Builder
	for _, model := range g.pdelModels {
		if err := renderInto(&delegateBody, render.Delegate, model); err != nil {
			return err
		}
	}
	if err := g.writeFile(packageDir, packageName+"_delegates.go", packageName, g.pdelImports, delegateBody.String()); err != nil {
		return err
	}

	// Delegate TypeDefs are not emitted into their home namespace: consumers ground
	// their own copies on demand instead. Record one diagnostic each so the absence is
	// accounted for rather than silent.
	for _, meta := range metas {
		for _, name := range sortedKeys(meta.Delegates) {
			g.diag("delegate-type-skipped", "%s.%s", meta.Namespace, name)
		}
	}

	// The package comment must sit above the package clause, which fileasm's scaffold
	// does not model, so doc.go is written directly.
	docPath := filepath.Join(packageDir, "doc.go")
	g.writtenFiles[docPath] = true
	return writeRawFile(docPath, []byte(g.packageDoc(packageName, pkg, members)))
}

// packageDoc renders doc.go. A multi-namespace package says which namespaces it
// carries and why they are together, because a reader looking for
// Microsoft.UI.Xaml.Controls needs to know it is here.
func (g *Generator) packageDoc(packageName, pkg string, members []string) string {
	var doc strings.Builder
	fmt.Fprintf(&doc, "%s\n\n//go:build %s\n\n", fileasm.Header, fileasm.GeneratedBuildTag)
	if len(members) == 1 {
		fmt.Fprintf(&doc, "// Package %s binds the %s API surface of the Windows App SDK.\n", packageName, pkg)
		fmt.Fprintf(&doc, "package %s\n", packageName)
		return doc.String()
	}
	fmt.Fprintf(&doc, "// Package %s binds the %s API surface of the Windows App SDK,\n", packageName, pkg)
	fmt.Fprintf(&doc, "// including every namespace beneath it that references it back:\n//\n")
	for _, member := range members {
		fmt.Fprintf(&doc, "//   - %s\n", member)
	}
	doc.WriteString("//\n" +
		"// They share one package because they are mutually recursive, and a Go package is\n" +
		"// the unit of mutual recursion — the same role the assembly plays in the SDK\n" +
		"// itself, which ships all of these together. Splitting them would require severing\n" +
		"// reference cycles, and the members lost to that are the ones that matter:\n" +
		"// UIElement's pointer, keyboard and manipulation events all take argument types\n" +
		"// declared in Microsoft.UI.Xaml.Input.\n")
	fmt.Fprintf(&doc, "package %s\n", packageName)
	return doc.String()
}

// renderInto appends one rendered construct to the file body.
func renderInto[T any](body *strings.Builder, renderFunc func(T) (string, error), model T) error {
	block, err := renderFunc(model)
	if err != nil {
		return err
	}
	body.WriteString(block)
	return nil
}

// fixedAliases are imports whose alias generated bodies reference by a fixed
// name rather than through a resolution. Detected in the body rather than
// tracked, because the alternative is threading an ImportSet through every
// lowering helper that might emit one.
func (g *Generator) fixedAliases() map[string]string {
	return map[string]string{
		"unsafe":    "unsafe",
		"syscall":   "syscall",
		"math":      "math",
		"win32":     typemap.Win32RuntimeImport,
		"syswinrt":  typemap.SysWinRTImport,
		"systemcom": typemap.SystemComImport,
		"winrt":     g.mapper.RuntimeImportPath(),
	}
}

// writeFile prunes unused imports — a resolution may have recorded one that a
// later skip made unnecessary — records the namespaces the surviving imports
// reference (the closure input), and assembles the file. An empty body produces
// no file at all.
func (g *Generator) writeFile(dir, fileName, packageName string, imports typemap.ImportSet, body string) error {
	if strings.TrimSpace(body) == "" {
		return nil
	}
	// Usage is detected against code only: a doc comment mentions qualified type
	// names without using them, and an import kept for a comment would not
	// compile.
	code := stripComments(body)
	pruned := map[string]string{}
	for alias, entry := range imports {
		if !referencesAlias(code, alias) {
			continue
		}
		pruned[alias] = entry.Path
		if entry.Namespace != "" {
			g.referenced[entry.Namespace] = true
		}
	}
	for alias, path := range g.fixedAliases() {
		if referencesAlias(code, alias) {
			pruned[alias] = path
		}
	}
	path := filepath.Join(dir, fileName)
	g.writtenFiles[path] = true
	return fileasm.WriteGoFile(path, fileasm.File{
		PackageName: packageName,
		BuildTag:    fileasm.GeneratedBuildTag,
		Imports:     pruned,
		Body:        body,
	})
}

// referencesAlias reports whether code uses the import alias as a package
// qualifier (`alias.`), requiring a word boundary so a shorter alias is not
// falsely matched inside a longer one — "ui." inside "wrtui." would be.
func referencesAlias(code, alias string) bool {
	needle := alias + "."
	for from := 0; ; {
		index := strings.Index(code[from:], needle)
		if index < 0 {
			return false
		}
		position := from + index
		if position == 0 || !isIdentByte(code[position-1]) {
			return true
		}
		from = position + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// stripComments removes //-comment text so import-usage scans see only code.
func stripComments(body string) string {
	lines := strings.Split(body, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if index := strings.Index(line, "//"); index >= 0 {
			line = line[:index]
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// writeRawFile writes pre-formatted content, creating parent directories.
func writeRawFile(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}

// preparePackageClaims resets the per-package state and pre-claims every top-level
// type name across every namespace the package carries, so a type always wins a
// type-versus-value collision.
//
// Claims are package-wide, not namespace-wide, because Go has one package-level scope.
// Merging the fourteen XAML namespaces put 2,937 type names in one scope with zero
// collisions — the namespaces partition the names cleanly — but the claim machinery is
// what would catch it if a future SDK stopped being so tidy.
func (g *Generator) preparePackageClaims(metas []*wasdkmeta.NamespaceMeta) {
	g.claimedNames = map[string]bool{}
	g.typeNames = map[string]bool{}
	g.pinstByName = map[string]*wasdkmeta.TypeRef{}
	g.pinstIID = map[string]string{}
	g.pinstQueue = nil
	g.pdelByName = map[string]*wasdkmeta.TypeRef{}
	g.pdelModels = nil
	g.pdelImports = typemap.ImportSet{}
	g.ifaceMethods = map[string][]emittedMethod{}

	claimType := func(name string) {
		exported := naming.Export(name)
		if !g.claimedNames[exported] {
			g.claimedNames[exported] = true
			g.typeNames[exported] = true
		}
	}
	for _, meta := range metas {
		for _, name := range sortedKeys(meta.Enums) {
			claimType(name)
		}
		for _, name := range sortedKeys(meta.Structs) {
			claimType(name)
		}
		for _, name := range sortedKeys(meta.Interfaces) {
			claimType(name)
		}
		for _, name := range sortedKeys(meta.Classes) {
			// A class that can never emit a type — statics-only, no default interface,
			// where the accessors are the whole projection — must not hold a claim. A
			// statics-only class X with statics interface IX would otherwise block its
			// own X() accessor.
			if meta.Classes[name].DefaultInterface == nil {
				continue
			}
			claimType(name)
		}
	}
}

// claimTypeName consumes a pre-claimed type name; false when the type lost its
// pre-claim to an earlier same-named type.
func (g *Generator) claimTypeName(name string) bool {
	if !g.typeNames[name] {
		return false
	}
	delete(g.typeNames, name) // consumed: a second same-named type is a duplicate
	return true
}

// claimName reserves a package-level identifier for a value — an enum member, an
// IID var, a constructor; false when it is already taken.
func (g *Generator) claimName(name string) bool {
	if g.claimedNames[name] {
		return false
	}
	g.claimedNames[name] = true
	return true
}

// resolveContext builds the typemap context for the namespace being emitted,
// wiring the demand-driven instantiation seam. RequestDelegate is wired only for
// parameter resolution, by the caller that needs it.
func (g *Generator) resolveContext(namespace string) typemap.Context {
	return typemap.Context{Namespace: namespace, RequestInstantiation: g.requestInstantiation}
}

// diag records one "key: detail" diagnostic.
func (g *Generator) diag(key, format string, args ...any) {
	g.Diagnostics = append(g.Diagnostics, key+": "+fmt.Sprintf(format, args...))
}

// sortedKeys returns a map's keys in sorted order. Determinism: the committed tree
// has to reproduce byte for byte, and every one of these maps would otherwise
// iterate differently each run.
func sortedKeys[M ~map[string]V, V any](m M) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// guidLiteral renders a canonical GUID string as a win32.GUID composite literal
// with lowercase hex.
func guidLiteral(guid string) (string, error) {
	parts := strings.Split(guid, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 ||
		len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		return "", fmt.Errorf("malformed GUID %q", guid)
	}
	data1, err1 := strconv.ParseUint(parts[0], 16, 32)
	data2, err2 := strconv.ParseUint(parts[1], 16, 16)
	data3, err3 := strconv.ParseUint(parts[2], 16, 16)
	if err1 != nil || err2 != nil || err3 != nil {
		return "", fmt.Errorf("malformed GUID %q", guid)
	}
	tail := parts[3] + parts[4]
	var data4 [8]string
	for i := range 8 {
		byteValue, err := strconv.ParseUint(tail[i*2:i*2+2], 16, 8)
		if err != nil {
			return "", fmt.Errorf("malformed GUID %q", guid)
		}
		data4[i] = fmt.Sprintf("0x%02x", byteValue)
	}
	return fmt.Sprintf("win32.GUID{Data1: 0x%08x, Data2: 0x%04x, Data3: 0x%04x, Data4: [8]byte{%s}}",
		data1, data2, data3, strings.Join(data4[:], ", ")), nil
}

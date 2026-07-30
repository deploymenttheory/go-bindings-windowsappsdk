// Package ingest projects the committed Windows App SDK winmds into the
// wasdkmeta IR: one NamespaceMeta per Microsoft.* namespace.
//
// Classification runs over every file before any projection, because the
// winmds cross-reference each other freely and a TypeRef gives no hint whether
// its target is local. Microsoft.UI.Xaml.winmd references
// Microsoft.UI.Dispatching, .Composition, .Input, .Windowing and .Text, all of
// which are defined in OTHER winmds in this repository — read one file at a
// time they are indistinguishable from Windows.* references, and projecting a
// sibling as foreign would be silent.
//
// The classification index is seeded with go-bindings-winrt's Windows.*
// universe first, so the two kinds of external reference stay distinguishable:
//
//   - Sibling winmds are NOT external. They are this module's own output.
//   - Windows.* IS external, and resolves into go-bindings-winrt.
//
// Anything in neither — Microsoft.Web.WebView2.Core is the only case in the
// committed set — resolves to nothing at all, and gets a distinct diagnostic so
// that members using it are skipped deliberately rather than quietly turned
// into an opaque word.
package ingest

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
	winmd "github.com/deploymenttheory/go-winmd"
)

// Source is one winmd to ingest.
type Source struct {
	// Name identifies the file in error messages and diagnostics
	// (e.g. "Microsoft.UI.Xaml.winmd").
	Name string
	// Version is the component package version it came from, per
	// PROVENANCE.json. Components version independently of the
	// Microsoft.WindowsAppSDK meta-package.
	Version string
	// File is the parsed winmd.
	File *winmd.File
}

// KnownForeignNamespaces are namespaces that are neither this module's output
// nor go-bindings-winrt's, and have no Go equivalent at all.
//
// Microsoft.Web.WebView2.Core is referenced by the XAML metadata (WebView2's
// XAML wrapper hands out Core objects) but ships in its own NuGet package with
// its own winmd, outside the Windows App SDK. Listing it here rather than
// letting it fall through as an unresolved reference is what keeps the
// distinction visible: an unresolved reference is a bug in the pin, whereas this
// is a permanent absence, and the members using it must be skipped with a reason
// that says so.
var KnownForeignNamespaces = map[string]string{
	"Microsoft.Web.WebView2.Core": "ships in the Microsoft.Web.WebView2 package, which has no Go bindings",
}

// Ingester projects a set of winmds into NamespaceMeta values.
type Ingester struct {
	sources  []Source
	external *external.Set

	// kindIndex classifies every type reachable from these winmds by full
	// name ("Namespace.Name") → construct kind ("Enum", "Struct",
	// "Interface", "Class", "Delegate"). It holds the union of the local
	// TypeDefs and go-bindings-winrt's Windows.* universe.
	kindIndex map[string]string

	// localNamespaces are the namespaces these winmds define, which is what
	// makes a reference local rather than external.
	localNamespaces map[string]bool

	// sourceOf maps a namespace to the winmd that first defined a type in it.
	sourceOf map[string]Source

	// Diagnostics collects non-fatal projection notes as "key: detail"
	// strings (e.g. "unresolved-typeref: Microsoft.Foo.IBar").
	Diagnostics []string
}

// New builds an Ingester and runs the classification pass.
//
// A duplicate full type name across winmds is a hard error: the components
// partition the surface, so a collision means a mispinned file set — two
// components shipping overlapping metadata — and silently keeping one would
// produce bindings for a surface that does not exist.
func New(sources []Source, externalSet *external.Set) (*Ingester, error) {
	in := &Ingester{
		sources:         sources,
		external:        externalSet,
		kindIndex:       map[string]string{},
		localNamespaces: map[string]bool{},
		sourceOf:        map[string]Source{},
	}

	// Seed with the external universe first. Local definitions overwrite
	// nothing, because a local winmd defining a Windows.* type is rejected
	// below.
	if externalSet != nil {
		maps.Copy(in.kindIndex, externalSet.Kinds())
	}

	definedIn := map[string]string{}
	for _, source := range sources {
		tables := &source.File.Tables
		for typeDefRow := range tables.TypeDefs {
			typeDef := &tables.TypeDefs[typeDefRow]
			// Skip the <Module> pseudo-type only: [ExclusiveTo] interfaces are
			// marked NotPublic throughout the XAML metadata but ARE the ABI
			// surface — every runtime class is reached through them.
			if typeDef.Namespace == "" {
				continue
			}
			kind := classifyTypeDef(source.File, typeDef)
			if kind == "" {
				continue // attribute definitions are not TypeRef targets
			}
			fullName := typeDef.Namespace + "." + typeDef.Name
			if previous, seen := definedIn[fullName]; seen {
				return nil, fmt.Errorf("ingest: type %s is defined in both %s and %s", fullName, previous, source.Name)
			}
			if externalSet != nil && externalSet.Defines(typeDef.Namespace) {
				return nil, fmt.Errorf("ingest: %s defines %s, which belongs to %s "+
					"(a local winmd must never shadow the Windows.* surface)",
					source.Name, fullName, external.ModulePath)
			}
			definedIn[fullName] = source.Name
			in.kindIndex[fullName] = kind
			if !in.localNamespaces[typeDef.Namespace] {
				in.localNamespaces[typeDef.Namespace] = true
				in.sourceOf[typeDef.Namespace] = source
			}
		}
	}
	return in, nil
}

// KindIndex exposes the union full-name → construct-kind classification.
func (in *Ingester) KindIndex() map[string]string { return in.kindIndex }

// LocalNamespaces returns the namespaces these winmds define, sorted.
func (in *Ingester) LocalNamespaces() []string {
	names := make([]string, 0, len(in.localNamespaces))
	for name := range in.localNamespaces {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Ingest projects every namespace from every source, merged by namespace and
// sorted by namespace name.
func (in *Ingester) Ingest() ([]*wasdkmeta.NamespaceMeta, error) {
	byNamespace := map[string]*wasdkmeta.NamespaceMeta{}
	for _, source := range in.sources {
		projector := newFileProjector(in, source)
		if err := projector.project(byNamespace); err != nil {
			return nil, err
		}
	}
	namespaces := make([]*wasdkmeta.NamespaceMeta, 0, len(byNamespace))
	for _, meta := range byNamespace {
		namespaces = append(namespaces, meta)
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Namespace < namespaces[j].Namespace })
	return namespaces, nil
}

// isLocal reports whether the namespace is defined by the ingested winmds.
func (in *Ingester) isLocal(namespace string) bool { return in.localNamespaces[namespace] }

// isExternal reports whether the namespace resolves into go-bindings-winrt.
func (in *Ingester) isExternal(namespace string) bool {
	return in.external != nil && in.external.Defines(namespace)
}

// diag records one "key: detail" diagnostic.
func (in *Ingester) diag(key, format string, args ...any) {
	in.Diagnostics = append(in.Diagnostics, key+": "+fmt.Sprintf(format, args...))
}

// DiagnosticSummary counts diagnostics by key, sorted by key.
func DiagnosticSummary(diagnostics []string) []string {
	counts := map[string]int{}
	for _, diagnostic := range diagnostics {
		key, _, _ := strings.Cut(diagnostic, ":")
		counts[key]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("  %-28s %d", key, counts[key]))
	}
	return lines
}

// classifyTypeDef determines a TypeDef's construct kind. Interfaces carry the
// flag; everything else classifies by its resolved Extends name — the mscorlib
// marker types (System.Enum/ValueType/MulticastDelegate/Attribute/Object) are
// type-system signals, never resolved as real types. An empty result marks an
// attribute definition (skipped as a type; its instances are consumed).
func classifyTypeDef(file *winmd.File, typeDef *winmd.TypeDefRow) string {
	if typeDef.Flags&winmd.TypeAttrInterface != 0 {
		return "Interface"
	}
	switch namespace, name := extendsOf(file, typeDef); namespace + "." + name {
	case "System.Enum":
		return "Enum"
	case "System.ValueType":
		return "Struct"
	case "System.MulticastDelegate":
		return "Delegate"
	case "System.Attribute":
		return "" // attribute definitions are not part of the API surface
	}
	// Extends System.Object (non-composable) or another runtime class
	// (composable) — which nearly every XAML class does.
	return "Class"
}

// extendsOf resolves a TypeDef's Extends coded index to (namespace, name).
func extendsOf(file *winmd.File, typeDef *winmd.TypeDefRow) (string, string) {
	tables := &file.Tables
	switch typeDef.Extends.Table {
	case winmd.TableTypeRef:
		if typeDef.Extends.Row != 0 && int(typeDef.Extends.Row) <= len(tables.TypeRefs) {
			ref := &tables.TypeRefs[typeDef.Extends.Row-1]
			return ref.Namespace, ref.Name
		}
	case winmd.TableTypeDef:
		if typeDef.Extends.Row != 0 && int(typeDef.Extends.Row) <= len(tables.TypeDefs) {
			def := &tables.TypeDefs[typeDef.Extends.Row-1]
			return def.Namespace, def.Name
		}
	}
	return "", ""
}

func typeDefTarget(row uint32) winmd.CodedIndex {
	return winmd.CodedIndex{Table: winmd.TableTypeDef, Row: row}
}

package typemap

// What go-bindings-winrt actually emitted, as opposed to what its metadata says it
// could have.
//
// This module resolves a Windows.* reference to a name in one of that module's
// packages — wrtuixamlinterop.TypeName — on the strength of the type appearing in its
// committed IR. That is the wrong evidence. The IR says what the type IS; only the
// emitted source says whether there is a Go declaration to name. The two differ
// exactly where that module degraded a member, and it degrades for the same reasons
// this one does.
//
// Windows.UI.Xaml.Interop.TypeName is the case that proved it. It is
// { HSTRING Name; TypeKind Kind; }, and go-bindings-winrt v0.4.0 skips it —
// "struct-field-skipped: has unrepresentable fields" in its own baseline. Its sibling
// TypeKind, an enum, IS emitted. So an assumption that held for every external type
// referenced up to that point held because none of them had been skipped, not because
// the assumption was sound.
//
// The check reads the dependency's emitted .go files. That is a coupling to its output
// layout, and the same coupling ImportPathFor already has when it builds an import
// path from a namespace — so it is consistent rather than new, and it answers the
// question by looking at the artefact that decides it.

import (
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
)

// typeDeclaration matches a package-level type declaration in generated Go. The
// emitter writes one type per line with no grouped declarations, so a line-anchored
// match is exact rather than approximate.
var typeDeclaration = regexp.MustCompile(`(?m)^type ([A-Za-z_]\w*) `)

// externalDeclarations caches, per namespace, the type names go-bindings-winrt
// declares in the package carrying it. A nil map value means the package directory
// does not exist, which is itself an answer: the namespace was never emitted.
type externalDeclarations struct {
	once  sync.Once
	root  string
	mu    sync.Mutex
	cache map[string]map[string]bool
}

// ExternalDeclares reports whether go-bindings-winrt declares a Go type for an
// external namespace member.
//
// Conservative on any failure to read: a false says "degrade the member", which costs
// a binding, while a wrong true emits a reference to an undeclared name and breaks the
// build of every consumer.
func (m *Mapper) ExternalDeclares(namespace, name string) bool {
	if m.externalDecls == nil {
		m.externalDecls = &externalDeclarations{}
	}
	decls := m.externalDecls
	decls.once.Do(func() {
		decls.cache = map[string]map[string]bool{}
		if m.Registry != nil && m.Registry.External != nil {
			decls.root = m.Registry.External.Root
		}
	})
	if decls.root == "" {
		// No module root — an explicitly-supplied metadata directory, as the tests use.
		// Fall back to the metadata's own answer rather than degrading everything.
		return true
	}

	decls.mu.Lock()
	defer decls.mu.Unlock()
	names, scanned := decls.cache[namespace]
	if !scanned {
		names = scanPackage(filepath.Join(decls.root, "bindings", "winrt",
			filepath.FromSlash(naming.ExternalPackagePath(namespace))))
		decls.cache[namespace] = names
	}
	return names[naming.Export(name)]
}

// scanPackage collects the package-level type names declared in one directory of
// generated Go. A missing directory yields an empty set, not an error: it means the
// namespace has no emitted package, and every reference into it must degrade.
func scanPackage(dir string) map[string]bool {
	names := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return names
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		for _, match := range typeDeclaration.FindAllSubmatch(content, -1) {
			names[string(match[1])] = true
		}
	}
	return names
}

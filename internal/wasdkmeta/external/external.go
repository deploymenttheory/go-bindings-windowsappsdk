// Package external loads the Windows.* type universe out of go-bindings-winrt.
//
// Windows App SDK metadata references Windows.* types constantly — 35
// namespaces and several hundred references from the committed winmds — and
// none of them are projected here. They resolve into the packages
// go-bindings-winrt already generated, so this module has to know two things
// about each: that it exists, and what shape it is.
//
// Both come from that module's own committed IR, read out of the Go module
// cache. That makes go.mod the version pin: the metadata this repository
// generates against is, by construction, exactly the metadata that produced the
// bindings it imports. Reading the generated Go source instead would mean
// parsing it, and hard-coding a type list would mean a second thing to update
// on every dependency bump.
//
// A worry that turns out not to apply: matching contract versions. WinRT
// contracts only ever add, so a newer contract always resolves an older
// assembly's references. What does need checking is that every reference
// resolves at all, which is what Validate is for.
package external

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// ModulePath is the module whose Windows.* bindings this one resolves against.
const ModulePath = "github.com/deploymenttheory/go-bindings-winrt"

// BindingsImportRoot is the import path prefix of its generated packages; a
// namespace's package path is appended to it.
const BindingsImportRoot = ModulePath + "/bindings/winrt"

// metadataSubdir is where the module keeps its committed IR.
const metadataSubdir = "metadata/winrt"

// fileExtension is that module's IR suffix. Its files are read, never written,
// so the name is fixed by them rather than by this package.
const fileExtension = ".winrtmeta.json"

// schemaVersion is the IR version go-bindings-winrt writes. It is pinned
// separately from wasdkmeta.CurrentSchemaVersion because the two evolve
// independently: this module bumping its own schema must not stop it reading
// that module's files.
//
// A mismatch here means the pinned go-bindings-winrt changed its IR format, and
// the fix is to look at what changed rather than to widen the check.
const schemaVersion = 1

// Set is the loaded Windows.* universe.
type Set struct {
	// Version is the resolved go-bindings-winrt module version ("v0.4.0"), or
	// "(local)" when the directory was given explicitly.
	Version string
	// Dir is the directory the metadata was read from.
	Dir string
	// Root is the module directory, of which Dir is a subdirectory. Needed because
	// the metadata says what that module COULD emit and only its emitted source says
	// what it did — see typemap.Mapper.ExternalDeclares.
	Root string

	byNamespace map[string]*wasdkmeta.NamespaceMeta
	kinds       map[string]string
	interfaces  map[string]*wasdkmeta.Interface
	classes     map[string]*wasdkmeta.Class
	structs     map[string]*wasdkmeta.Struct
	delegates   map[string]*wasdkmeta.Delegate
	enums       map[string]*wasdkmeta.Enum
}

// Locate returns the metadata directory inside the pinned go-bindings-winrt
// module.
//
// `go list -m` is asked rather than the module cache path being assembled by
// hand, because the answer differs with GOMODCACHE, with a replace directive
// and with a vendor directory, and getting it wrong would silently resolve
// against the wrong version of the bindings.
func Locate() (dir, version string, err error) {
	dir, _, version, err = locate()
	return dir, version, err
}

// locate also returns the module root, which Locate's two callers outside this
// package do not need.
func locate() (dir, root, version string, err error) {
	output, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}\t{{.Dir}}", ModulePath).Output()
	if err != nil {
		return "", "", "", fmt.Errorf("external: locating %s (run 'go mod download'): %w", ModulePath, err)
	}
	version, moduleDir, found := strings.Cut(strings.TrimSpace(string(output)), "\t")
	if !found || moduleDir == "" {
		return "", "", "", fmt.Errorf("external: %s has no module directory (is it vendored?)", ModulePath)
	}
	return filepath.Join(moduleDir, filepath.FromSlash(metadataSubdir)), moduleDir, version, nil
}

// Load reads the Windows.* metadata. An empty dir locates the pinned module.
func Load(dir string) (*Set, error) {
	version, root := "(local)", ""
	if dir == "" {
		located, locatedRoot, resolved, err := locate()
		if err != nil {
			return nil, err
		}
		dir, root, version = located, locatedRoot, resolved
	}

	paths, err := filepath.Glob(filepath.Join(dir, "*"+fileExtension))
	if err != nil {
		return nil, fmt.Errorf("external: %w", err)
	}
	if len(paths) == 0 {
		if _, statErr := os.Stat(dir); statErr != nil {
			return nil, fmt.Errorf("external: %s does not exist: %w", dir, statErr)
		}
		return nil, fmt.Errorf("external: no %s files in %s", fileExtension, dir)
	}
	sort.Strings(paths)

	set := &Set{
		Version:     version,
		Dir:         dir,
		Root:        root,
		byNamespace: map[string]*wasdkmeta.NamespaceMeta{},
		kinds:       map[string]string{},
		interfaces:  map[string]*wasdkmeta.Interface{},
		classes:     map[string]*wasdkmeta.Class{},
		structs:     map[string]*wasdkmeta.Struct{},
		delegates:   map[string]*wasdkmeta.Delegate{},
		enums:       map[string]*wasdkmeta.Enum{},
	}
	for _, path := range paths {
		meta, err := wasdkmeta.Decode(path, schemaVersion)
		if err != nil {
			return nil, fmt.Errorf("external: %s pins %s IR v%d: %w",
				ModulePath, version, schemaVersion, err)
		}
		set.index(meta)
	}
	return set, nil
}

// index records one namespace's types under their full names.
func (s *Set) index(meta *wasdkmeta.NamespaceMeta) {
	s.byNamespace[meta.Namespace] = meta
	qualify := func(name string) string { return meta.Namespace + "." + name }

	for name := range meta.Enums {
		definition := meta.Enums[name]
		s.enums[qualify(name)] = &definition
		s.kinds[qualify(name)] = "Enum"
	}
	for name := range meta.Structs {
		definition := meta.Structs[name]
		s.structs[qualify(name)] = &definition
		s.kinds[qualify(name)] = "Struct"
	}
	for name := range meta.Interfaces {
		definition := meta.Interfaces[name]
		s.interfaces[qualify(name)] = &definition
		s.kinds[qualify(name)] = "Interface"
	}
	for name := range meta.Classes {
		definition := meta.Classes[name]
		s.classes[qualify(name)] = &definition
		s.kinds[qualify(name)] = "Class"
	}
	for name := range meta.Delegates {
		definition := meta.Delegates[name]
		s.delegates[qualify(name)] = &definition
		s.kinds[qualify(name)] = "Delegate"
	}
}

// Kind returns a type's construct kind ("Enum", "Struct", "Interface",
// "Class", "Delegate"), or "" when the set does not define it.
func (s *Set) Kind(fullName string) string { return s.kinds[fullName] }

// Defines reports whether the namespace is one this set carries.
func (s *Set) Defines(namespace string) bool {
	_, ok := s.byNamespace[namespace]
	return ok
}

// Kinds exposes the full-name → construct-kind index, for seeding ingest's
// classification pass.
func (s *Set) Kinds() map[string]string { return s.kinds }

// Interface resolves an interface reference, or nil.
func (s *Set) Interface(namespace, name string) *wasdkmeta.Interface {
	return s.interfaces[namespace+"."+name]
}

// Class resolves a runtime-class reference, or nil.
func (s *Set) Class(namespace, name string) *wasdkmeta.Class {
	return s.classes[namespace+"."+name]
}

// Struct resolves a struct reference, or nil.
func (s *Set) Struct(namespace, name string) *wasdkmeta.Struct {
	return s.structs[namespace+"."+name]
}

// Delegate resolves a delegate reference, or nil.
func (s *Set) Delegate(namespace, name string) *wasdkmeta.Delegate {
	return s.delegates[namespace+"."+name]
}

// Enum resolves an enum reference, or nil.
func (s *Set) Enum(namespace, name string) *wasdkmeta.Enum {
	return s.enums[namespace+"."+name]
}

// Namespaces returns every namespace in the set, sorted.
func (s *Set) Namespaces() []string {
	names := make([]string, 0, len(s.byNamespace))
	for name := range s.byNamespace {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Meta returns one namespace's metadata, or nil.
func (s *Set) Meta(namespace string) *wasdkmeta.NamespaceMeta {
	return s.byNamespace[namespace]
}

// Len reports how many types the set defines.
func (s *Set) Len() int { return len(s.kinds) }

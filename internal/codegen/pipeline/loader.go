// Package pipeline loads the ingested IR into a Registry and drives the
// emitters.
package pipeline

import (
	"fmt"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

// Registry is the cross-namespace resolution index.
//
// It resolves over both halves of the type universe. The local half is what this
// module emits; the external half is go-bindings-winrt's, read from the pinned
// module. Lookups check local first and fall through, so a caller asks "what is
// this type" without deciding which module owns it — while IsExternal answers the
// separate question of which package to import, which is the only place the
// distinction actually matters.
type Registry struct {
	// Namespaces are the LOCAL namespaces, sorted, and the emit worklist.
	Namespaces []*wasdkmeta.NamespaceMeta

	// ByNamespace maps a local namespace to its meta.
	ByNamespace map[string]*wasdkmeta.NamespaceMeta

	// External is the Windows.* universe, or nil when it was not loaded.
	External *external.Set

	enums      map[string]*wasdkmeta.Enum
	structs    map[string]*wasdkmeta.Struct
	interfaces map[string]*wasdkmeta.Interface
	classes    map[string]*wasdkmeta.Class
	delegates  map[string]*wasdkmeta.Delegate
}

// qualified builds the "Namespace.Name" index key.
func qualified(namespace, name string) string { return namespace + "." + name }

// Load reads the committed IR from dir and pairs it with the external universe.
func Load(dir string, externalSet *external.Set) (*Registry, error) {
	namespaces, err := wasdkmeta.ReadAll(dir)
	if err != nil {
		return nil, err
	}
	if len(namespaces) == 0 {
		return nil, fmt.Errorf("pipeline: no %s files in %s (run 'generate ingest')", wasdkmeta.Extension, dir)
	}
	registry := &Registry{
		Namespaces:  namespaces,
		ByNamespace: make(map[string]*wasdkmeta.NamespaceMeta, len(namespaces)),
		External:    externalSet,
		enums:       map[string]*wasdkmeta.Enum{},
		structs:     map[string]*wasdkmeta.Struct{},
		interfaces:  map[string]*wasdkmeta.Interface{},
		classes:     map[string]*wasdkmeta.Class{},
		delegates:   map[string]*wasdkmeta.Delegate{},
	}
	for _, meta := range namespaces {
		registry.ByNamespace[meta.Namespace] = meta
		for name := range meta.Enums {
			definition := meta.Enums[name]
			registry.enums[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Structs {
			definition := meta.Structs[name]
			registry.structs[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Interfaces {
			definition := meta.Interfaces[name]
			registry.interfaces[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Classes {
			definition := meta.Classes[name]
			registry.classes[qualified(meta.Namespace, name)] = &definition
		}
		for name := range meta.Delegates {
			definition := meta.Delegates[name]
			registry.delegates[qualified(meta.Namespace, name)] = &definition
		}
	}
	return registry, nil
}

// IsLocal reports whether this module emits the namespace.
func (r *Registry) IsLocal(namespace string) bool {
	_, ok := r.ByNamespace[namespace]
	return ok
}

// IsExternal reports whether the namespace resolves into go-bindings-winrt.
func (r *Registry) IsExternal(namespace string) bool {
	return r.External != nil && r.External.Defines(namespace)
}

// Interface resolves an interface reference from either half, or nil.
func (r *Registry) Interface(namespace, name string) *wasdkmeta.Interface {
	if definition := r.interfaces[qualified(namespace, name)]; definition != nil {
		return definition
	}
	if r.External != nil {
		return r.External.Interface(namespace, name)
	}
	return nil
}

// Class resolves a runtime-class reference from either half, or nil.
func (r *Registry) Class(namespace, name string) *wasdkmeta.Class {
	if definition := r.classes[qualified(namespace, name)]; definition != nil {
		return definition
	}
	if r.External != nil {
		return r.External.Class(namespace, name)
	}
	return nil
}

// Struct resolves a struct reference from either half, or nil.
func (r *Registry) Struct(namespace, name string) *wasdkmeta.Struct {
	if definition := r.structs[qualified(namespace, name)]; definition != nil {
		return definition
	}
	if r.External != nil {
		return r.External.Struct(namespace, name)
	}
	return nil
}

// Delegate resolves a delegate reference from either half, or nil.
func (r *Registry) Delegate(namespace, name string) *wasdkmeta.Delegate {
	if definition := r.delegates[qualified(namespace, name)]; definition != nil {
		return definition
	}
	if r.External != nil {
		return r.External.Delegate(namespace, name)
	}
	return nil
}

// Enum resolves an enum reference from either half, or nil.
func (r *Registry) Enum(namespace, name string) *wasdkmeta.Enum {
	if definition := r.enums[qualified(namespace, name)]; definition != nil {
		return definition
	}
	if r.External != nil {
		return r.External.Enum(namespace, name)
	}
	return nil
}

// EnumBase resolves an enum's Go base type, or "".
func (r *Registry) EnumBase(namespace, name string) string {
	if enum := r.Enum(namespace, name); enum != nil {
		return enum.BaseType
	}
	return ""
}

// Package metaquery answers questions about winmd types: what is this
// interface's IID, and which vtable slot is this method at.
//
// It exists because both cmd/inspect and the hand-written spike need the same
// two answers, and because taking them from the metadata is better than
// writing GUIDs down by hand. A slot that moves, or an IID that changes with
// an SDK bump, then shows up as a failure that names the type rather than as a
// call into the wrong function.
package metaquery

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	winmd "github.com/deploymenttheory/go-winmd/pkg/winmd"
)

// Kind classifies a TypeDef the way a WinRT projection has to.
type Kind string

const (
	KindInterface Kind = "interface"
	KindClass     Kind = "class"
	KindEnum      Kind = "enum"
	KindStruct    Kind = "struct"
	KindDelegate  Kind = "delegate"
	// KindOther covers attribute definitions and <Module>, which are metadata
	// plumbing rather than API.
	KindOther Kind = "other"
)

// Type is one resolved TypeDef.
type Type struct {
	Namespace string
	Name      string
	Kind      Kind
	// IID is the type's GUID, empty when it declares none. Interfaces and
	// delegates always have one; runtime classes do not.
	IID string
	// Methods are in MethodDef order, which is what vtable slots follow.
	Methods []Method
	// Attributes are the type's custom attribute names, with the "Attribute"
	// suffix removed.
	Attributes []string
	// Source is the winmd it came from.
	Source string
}

// FullName is the metadata name, namespace included.
func (t Type) FullName() string {
	if t.Namespace == "" {
		return t.Name
	}
	return t.Namespace + "." + t.Name
}

// Method is one method, with the vtable slot it dispatches through.
type Method struct {
	Name string
	// Slot is the vtable slot the method dispatches through, or NoSlot when it
	// has none. See slotsFor for how the two kinds that have vtables number
	// them differently.
	Slot int
}

// NoSlot marks a method that is not reached through a vtable at all: a runtime
// class's members (the class has no vtable of its own — it is reached through
// its interfaces) and a delegate's .ctor (metadata plumbing, never called).
const NoSlot = -1

// HasSlot reports whether the method is dispatched through a vtable slot.
func (m Method) HasSlot() bool { return m.Slot != NoSlot }

// interfaceFirstSlot is where an interface's own methods begin: IInspectable
// occupies slots 0-5 (IUnknown's three, then GetIids, GetRuntimeClassName and
// GetTrustLevel).
const interfaceFirstSlot = 6

// delegateInvokeSlot is where a delegate's Invoke sits. Delegates derive from
// IUnknown, NOT IInspectable, so the vtable is {QueryInterface, AddRef,
// Release, Invoke} and Invoke is the fourth word — not the seventh. Numbering a
// delegate as though it were an interface points three slots past the end of a
// four-word vtable, which is why this is spelled out rather than derived.
const delegateInvokeSlot = 3

// slotsFor assigns the vtable slot of each method in a type, in MethodDef
// order — which is vtable order for the kinds that have a vtable.
func slotsFor(kind Kind, names []string) []Method {
	methods := make([]Method, len(names))
	for i, name := range names {
		methods[i] = Method{Name: name, Slot: NoSlot}
		switch kind {
		case KindInterface:
			methods[i].Slot = interfaceFirstSlot + i
		case KindDelegate:
			// A delegate declares .ctor and Invoke; only Invoke is dispatched.
			if name == "Invoke" {
				methods[i].Slot = delegateInvokeSlot
			}
		}
	}
	return methods
}

// Set is a queryable collection of types loaded from one or more winmds.
type Set struct {
	byFullName map[string]Type
	namespaces map[string]int
	// external counts references to namespaces no loaded file defines.
	external map[string]int
	sources  []string
}

// Load reads every winmd in paths.
func Load(paths ...string) (*Set, error) {
	set := &Set{
		byFullName: map[string]Type{},
		namespaces: map[string]int{},
		external:   map[string]int{},
	}
	files := make([]*winmd.File, 0, len(paths))
	for _, path := range paths {
		file, err := winmd.Open(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		files = append(files, file)
		set.sources = append(set.sources, filepath.Base(path))
		set.collect(file, filepath.Base(path))
	}
	// External references can only be decided once every file's definitions
	// are known: a namespace one winmd references is routinely defined by
	// another in the same set.
	for _, file := range files {
		for i := range file.Tables.TypeRefs {
			namespace := file.Tables.TypeRefs[i].Namespace
			if namespace == "" {
				continue
			}
			if _, defined := set.namespaces[namespace]; defined {
				continue
			}
			set.external[namespace]++
		}
	}
	return set, nil
}

// LoadDir reads every winmd in a directory.
func LoadDir(dir string) (*Set, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.winmd"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no winmds in %s", dir)
	}
	sort.Strings(matches)
	return Load(matches...)
}

func (s *Set) collect(file *winmd.File, source string) {
	for i := range file.Tables.TypeDefs {
		row := &file.Tables.TypeDefs[i]
		kind := classify(file, row)
		if kind == KindOther {
			continue
		}
		resolved := Type{
			Namespace: row.Namespace,
			Name:      row.Name,
			Kind:      kind,
			Source:    source,
		}
		index := winmd.CodedIndex{Table: winmd.TableTypeDef, Row: uint32(i + 1)}
		attributes := file.AttributesFor(index)
		for _, attribute := range attributes {
			if attribute.Name == "GuidAttribute" {
				if guid, ok := decodeGUID(attribute); ok {
					resolved.IID = guid
				}
				continue
			}
			resolved.Attributes = append(resolved.Attributes, strings.TrimSuffix(attribute.Name, "Attribute"))
		}
		sort.Strings(resolved.Attributes)

		if first, end := row.MethodFirst, row.MethodEnd; first != 0 && end > first {
			names := make([]string, 0, end-first)
			for index := first; index < end; index++ {
				names = append(names, file.Tables.Methods[index-1].Name)
			}
			resolved.Methods = slotsFor(kind, names)
		}
		if resolved.Namespace != "" {
			s.namespaces[resolved.Namespace]++
		}
		s.byFullName[strings.ToLower(resolved.FullName())] = resolved
	}
}

// Type looks a type up by its full metadata name, case-insensitively.
func (s *Set) Type(fullName string) (Type, bool) {
	resolved, ok := s.byFullName[strings.ToLower(fullName)]
	return resolved, ok
}

// IID returns an interface's GUID. It fails rather than returning a zero GUID,
// because a zero IID passed to QueryInterface produces a confusing failure a
// long way from the cause.
func (s *Set) IID(fullName string) (string, error) {
	resolved, ok := s.Type(fullName)
	if !ok {
		return "", fmt.Errorf("metaquery: type %s not found in %v", fullName, s.sources)
	}
	if resolved.IID == "" {
		return "", fmt.Errorf("metaquery: %s declares no IID (kind %s)", fullName, resolved.Kind)
	}
	return resolved.IID, nil
}

// Slot returns the vtable slot a method dispatches through.
//
// Overloaded methods share a metadata name, so the first match wins; callers
// needing a specific overload should use SlotsNamed.
func (s *Set) Slot(fullName, method string) (int, error) {
	slots, err := s.SlotsNamed(fullName, method)
	if err != nil {
		return 0, err
	}
	return slots[0], nil
}

// SlotsNamed returns every slot for a method name, in declaration order.
//
// It fails rather than returning NoSlot: a runtime class's members and a
// delegate's .ctor are not dispatched through a vtable at all, and a negative
// index used as one indexes off the front of the vtable array.
func (s *Set) SlotsNamed(fullName, method string) ([]int, error) {
	resolved, ok := s.Type(fullName)
	if !ok {
		return nil, fmt.Errorf("metaquery: type %s not found in %v", fullName, s.sources)
	}
	var slots []int
	var found bool
	for _, candidate := range resolved.Methods {
		if !strings.EqualFold(candidate.Name, method) {
			continue
		}
		found = true
		if candidate.HasSlot() {
			slots = append(slots, candidate.Slot)
		}
	}
	switch {
	case !found:
		return nil, fmt.Errorf("metaquery: %s has no method %s (has: %s)",
			fullName, method, strings.Join(methodNames(resolved), ", "))
	case len(slots) == 0:
		return nil, fmt.Errorf("metaquery: %s.%s has no vtable slot (kind %s)",
			fullName, method, resolved.Kind)
	}
	return slots, nil
}

// Namespaces reports how many API types each defined namespace holds.
func (s *Set) Namespaces() map[string]int { return s.namespaces }

// External reports how many references each undefined namespace received.
func (s *Set) External() map[string]int { return s.external }

// Types returns every loaded type, ordered by full name.
func (s *Set) Types() []Type {
	all := make([]Type, 0, len(s.byFullName))
	for _, resolved := range s.byFullName {
		all = append(all, resolved)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].FullName() < all[j].FullName() })
	return all
}

func methodNames(resolved Type) []string {
	names := make([]string, 0, len(resolved.Methods))
	for _, method := range resolved.Methods {
		names = append(names, method.Name)
	}
	return names
}

// decodeGUID reads a WinRT GuidAttribute, whose eleven fixed arguments are the
// GUID's fields: one uint32, two uint16s, then eight bytes.
func decodeGUID(attribute winmd.CustomAttr) (string, bool) {
	if len(attribute.Fixed) != 11 {
		return "", false
	}
	parts := make([]uint64, 11)
	for i, value := range attribute.Fixed {
		converted, ok := toUint(value)
		if !ok {
			return "", false
		}
		parts[i] = converted
	}
	return fmt.Sprintf("%08x-%04x-%04x-%02x%02x-%02x%02x%02x%02x%02x%02x",
		parts[0], parts[1], parts[2], parts[3], parts[4],
		parts[5], parts[6], parts[7], parts[8], parts[9], parts[10]), true
}

func toUint(value any) (uint64, bool) {
	switch typed := value.(type) {
	case uint8:
		return uint64(typed), true
	case uint16:
		return uint64(typed), true
	case uint32:
		return uint64(typed), true
	case uint64:
		return typed, true
	case int8:
		return uint64(uint8(typed)), true
	case int16:
		return uint64(uint16(typed)), true
	case int32:
		return uint64(uint32(typed)), true
	case int64:
		return uint64(typed), true
	}
	return 0, false
}

// classify sorts a TypeDef by its flags and by what it extends.
func classify(file *winmd.File, row *winmd.TypeDefRow) Kind {
	if row.Name == "<Module>" {
		return KindOther
	}
	if row.Flags&winmd.TypeAttrInterface != 0 {
		return KindInterface
	}
	switch baseName(file, row.Extends) {
	case "System.Enum":
		return KindEnum
	case "System.ValueType":
		return KindStruct
	case "System.MulticastDelegate":
		return KindDelegate
	case "System.Attribute":
		return KindOther
	}
	return KindClass
}

func baseName(file *winmd.File, extends winmd.CodedIndex) string {
	if extends.IsNull() {
		return ""
	}
	switch extends.Table {
	case winmd.TableTypeRef:
		if int(extends.Row) <= len(file.Tables.TypeRefs) {
			ref := &file.Tables.TypeRefs[extends.Row-1]
			return ref.Namespace + "." + ref.Name
		}
	case winmd.TableTypeDef:
		if int(extends.Row) <= len(file.Tables.TypeDefs) {
			def := &file.Tables.TypeDefs[extends.Row-1]
			return def.Namespace + "." + def.Name
		}
	}
	return ""
}

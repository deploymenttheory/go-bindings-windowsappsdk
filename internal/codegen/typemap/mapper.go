// Package typemap converts IR TypeRefs into Go types under the WinRT projection
// rules. It is the ONLY place type decisions live: the emit gather layer consumes
// Resolved values and never inspects TypeRefs itself. Cross-namespace references
// populate the caller's ImportSet as a side effect; anything the ABI lowering
// cannot represent comes back as KindUnsupported with the diagnostic key and
// detail in Reason, and the gather layer degrades the member.
package typemap

import (
	"fmt"
	"maps"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/ingest"
)

// Win32ModulePath hosts the shared ABI foundation: the win32 runtime
// (HRESULT/GUID/IUnknown) and the generated system/winrt package (HSTRING,
// IInspectable, the Ro* activation functions).
const Win32ModulePath = "github.com/deploymenttheory/go-bindings-win32"

// Win32RuntimeImport is the hand-written win32 runtime package (alias win32).
const Win32RuntimeImport = Win32ModulePath + "/bindings/runtime/win32"

// SysWinRTImport is the generated WinRT system package (alias syswinrt).
const SysWinRTImport = Win32ModulePath + "/bindings/win32/system/winrt"

// SystemComImport is the generated COM system package (alias systemcom). Needed only
// for CoTaskMemFree: a returned conformant array is allocated by the callee and freed
// by the caller, which is the one place generated code owns native memory.
const SystemComImport = Win32ModulePath + "/bindings/win32/system/com"

// WinRTRuntimeImport is go-bindings-winrt's hand-written runtime: activation,
// QueryInterface, strings, Go-implemented delegates, collections, Await.
//
// This module does not reimplement any of it. The alias is winrt, matching the
// package name, so generated bodies read the same as that module's own.
const WinRTRuntimeImport = external.ModulePath + "/bindings/runtime/winrt"

// Import is one import edge: the Go import path plus the namespace it carries
// ("" for runtime and support imports). The generator computes the transitive
// emit closure from the Namespace fields of imports that survive pruning, so only
// namespaces referenced by EMITTED members are chased — and external imports
// carry no namespace, because nothing here emits them.
type Import struct {
	Path      string
	Namespace string
}

// ImportSet accumulates alias → Import pairs as resolution progresses.
type ImportSet map[string]Import

// Merge copies every entry of other into the set (used to commit a per-member
// scratch set once the member is known to be emitted).
func (s ImportSet) Merge(other ImportSet) {
	maps.Copy(s, other)
}

// Kind classifies a resolved Go type for ABI lowering decisions.
type Kind uint8

const (
	KindVoid         Kind = iota // no value (returns only)
	KindScalar                   // integer scalar (incl. Char16 → uint16)
	KindFloat                    // float32/float64; crosses SyscallN as a bit pattern (layout.go)
	KindBool                     // Go bool; one byte at the WinRT ABI
	KindString                   // Go string; syswinrt.HSTRING at the ABI
	KindGUID                     // win32.GUID by value
	KindEnum                     // named enum type (int32/uint32 backed)
	KindStruct                   // value struct
	KindObjectPtr                // *syswinrt.IInspectable
	KindInterfacePtr             // interface pointer (*IFoo)
	KindDelegatePtr              // grounded delegate handler pointer (*FooHandler)
	KindArray                    // WinRT conformant array; Elem carries the element
	KindUnsupported              // member must degrade; see Reason
)

// Resolved is the pure-data result of one type resolution.
type Resolved struct {
	// GoType is the rendered Go type ("string", "*IButton",
	// "wrtfoundation.Rect"). Empty for KindVoid and KindUnsupported.
	GoType string
	Kind   Kind
	// Reason carries the diagnostic "key: detail" when Kind is KindUnsupported.
	Reason string
	// Note is a non-fatal remark the gather layer surfaces in the generated doc
	// comment (e.g. a class reference projected as IInspectable).
	Note string
	// StructNamespace/StructName identify a KindStruct target so the gather
	// layer can apply the by-value flattening rule.
	StructNamespace, StructName string
	// Elem is the element's resolution when Kind is KindArray. The lowering has
	// to branch on the ELEMENT's kind — a []byte and a []*IFoo travel the same
	// two ABI words but differ in what the caller owns afterwards — so the
	// element cannot be reduced to a type name here.
	Elem *Resolved
}

// Context carries per-resolution state.
type Context struct {
	// Namespace is the full namespace being emitted; references into it stay
	// unqualified. Blocked-edge decisions key off this namespace.
	Namespace string

	// RequestInstantiation is the gather layer's demand-driven seam for closed
	// generic INTERFACE instantiations: the callback registers the
	// instantiation for emission into the consuming package and returns its
	// package-local Go type name. A nil callback, or ok == false, degrades the
	// member.
	RequestInstantiation func(ref *wasdkmeta.TypeRef) (string, bool)

	// RequestDelegate is the gather layer's delegate-grounding seam for method
	// PARAMETERS: the callback grounds a delegate into a package-local handler
	// type and returns its Go type name. It is wired ONLY for parameter
	// resolution; delegate RETURNS resolve without it and keep degrading.
	RequestDelegate func(ref *wasdkmeta.TypeRef) (string, bool)
}

// Mapper resolves TypeRefs against the loaded Registry.
type Mapper struct {
	Registry *pipeline.Registry
	// ModulePath is this module's import path root.
	ModulePath string

	// Blocked marks severed cross-namespace edges (import-cycle breaks):
	// Blocked[src][dst] forces references from src to dst to degrade instead of
	// importing.
	Blocked map[string]map[string]bool

	// structEmittable memoizes the per-struct emittability decision.
	structEmittable map[string]bool

	// structLayout memoizes the per-struct amd64 layout (see layout.go).
	structLayout map[string]layoutResult
}

// externalTypes are types that are NEVER emitted by either module: they already
// exist in the shared ABI foundation, or they flatten to a primitive.
// Re-emitting them would fork the identity every signature depends on.
//
// These are go-bindings-winrt's own special cases, and they have to be the same
// ones: EventRegistrationToken is what every add_/remove_ pair exchanges, so a
// second definition of it would make this module's events incompatible with that
// module's.
var externalTypes = map[string]Resolved{
	"Windows.Foundation.EventRegistrationToken": {
		GoType: "syswinrt.EventRegistrationToken", Kind: KindStruct,
		StructNamespace: "Windows.Foundation", StructName: "EventRegistrationToken",
	},
	"Windows.Foundation.HResult": {GoType: "int32", Kind: KindScalar},
}

// IsExternalType reports whether the full type name routes to the shared ABI
// foundation instead of a generated package.
func IsExternalType(namespace, name string) bool {
	_, ok := externalTypes[namespace+"."+name]
	return ok
}

// nativeScalars maps IR Native names to Go scalar types.
var nativeScalars = map[string]string{
	"Char16": "uint16",
	"I1":     "int8",
	"U1":     "byte",
	"I2":     "int16",
	"U2":     "uint16",
	"I4":     "int32",
	"U4":     "uint32",
	"I8":     "int64",
	"U8":     "uint64",
}

// GoType resolves one TypeRef. Cross-namespace references are qualified with the
// owning package alias and recorded in imports.
func (m *Mapper) GoType(ref *wasdkmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	switch ref.Kind {
	case "Native":
		return m.resolveNative(ref, imports)
	case "ApiRef":
		return m.resolveApiRef(ref, ctx, imports)
	case "GenericInst":
		return m.resolveGenericInst(ref, ctx)
	case "GenericParamRef":
		return unsupported("generic-member-skipped", "generic type parameter")
	case "Array":
		return m.resolveArray(ref, ctx, imports)
	}
	return unsupported("unknown-typeref-kind", "%q", ref.Kind)
}

// resolveArray maps a WinRT conformant array to a Go slice.
//
// A conformant array crosses the ABI as two words, a count and a data pointer, and
// carries no length in metadata — the count is synthesized from the slice. The Go
// slice is therefore a direct view of the same memory, which only works when the
// element's Go representation is byte-identical to its ABI form.
//
// Admitted: scalars, floats, enums, GUIDs, emittable structs, and interface, class
// and Object pointers. Together these are 92% of the arrays in the committed
// metadata.
//
// Refused: HSTRING and Bool elements, which need per-element conversion (an HSTRING
// is a handle, not a string, and a WinRT boolean is one byte with no guarantee it is
// 0 or 1), and nested arrays. Emitting a direct view over those would be a silent
// memcpy of the wrong thing, which is worse than an absent member.
func (m *Mapper) resolveArray(ref *wasdkmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	if ref.Elem == nil {
		return unsupported("array-element-skipped", "array with no element type")
	}
	// A scratch set: an element that turns out to be inadmissible must not leave
	// its import behind on a member that is then skipped.
	scratch := ImportSet{}
	elem := m.GoType(ref.Elem, ctx, scratch)
	switch elem.Kind {
	case KindScalar, KindFloat, KindEnum, KindGUID, KindStruct,
		KindInterfacePtr, KindObjectPtr:
	case KindUnsupported:
		return unsupported("array-element-skipped", "element: %s", elem.Reason)
	default:
		return unsupported("array-element-skipped",
			"%s elements need per-element conversion", elem.GoType)
	}
	imports.Merge(scratch)
	return Resolved{
		GoType: "[]" + elem.GoType,
		Kind:   KindArray,
		Elem:   &elem,
		Note:   elem.Note,
	}
}

func (m *Mapper) resolveNative(ref *wasdkmeta.TypeRef, imports ImportSet) Resolved {
	switch ref.Name {
	case "Void":
		return Resolved{Kind: KindVoid}
	case "Bool":
		return Resolved{GoType: "bool", Kind: KindBool}
	case "F32":
		return Resolved{GoType: "float32", Kind: KindFloat}
	case "F64":
		return Resolved{GoType: "float64", Kind: KindFloat}
	case "HString":
		return Resolved{GoType: "string", Kind: KindString}
	case "Guid":
		imports["win32"] = Import{Path: Win32RuntimeImport}
		return Resolved{GoType: "win32.GUID", Kind: KindGUID}
	case "Object":
		imports["syswinrt"] = Import{Path: SysWinRTImport}
		return Resolved{GoType: "*syswinrt.IInspectable", Kind: KindObjectPtr}
	}
	if goType, ok := nativeScalars[ref.Name]; ok {
		return Resolved{GoType: goType, Kind: KindScalar}
	}
	return unsupported("unknown-native-type", "%q", ref.Name)
}

// resolveGenericInst maps a closed generic instantiation to the concrete
// (monomorphized) type the gather layer emits into the consuming package —
// package-local, so no import is recorded, and that holds whether the open type
// is local or go-bindings-winrt's. IVector`1<Foo> becomes an IVectorOfFoo here
// rather than a reference into that module, because a Go type parameterized at
// the ABI level does not exist: each consumer needs its own monomorphization.
// Distinct Go types, identical ABI, same IID.
//
// Cross-namespace ARGUMENT references are resolved (and blocked-edge checked)
// when the instantiation's synthesized methods are lowered, so the open type's
// namespace is never imported and blocked edges do not apply here.
func (m *Mapper) resolveGenericInst(ref *wasdkmeta.TypeRef, ctx Context) Resolved {
	if ctx.RequestInstantiation != nil && m.Registry.Interface(ref.Namespace, ref.Name) != nil {
		name, ok := ctx.RequestInstantiation(ref)
		if !ok {
			return unsupported("generic-member-skipped", "parameterized type %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{GoType: "*" + name, Kind: KindInterfacePtr}
	}
	if ctx.RequestDelegate != nil && m.Registry.Delegate(ref.Namespace, ref.Name) != nil {
		name, ok := ctx.RequestDelegate(ref)
		if !ok {
			return unsupported("generic-member-skipped", "parameterized type %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{GoType: "*" + name, Kind: KindDelegatePtr}
	}
	return unsupported("generic-member-skipped", "parameterized type %s.%s", ref.Namespace, ref.Name)
}

func (m *Mapper) resolveApiRef(ref *wasdkmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	if resolved, ok := externalTypes[ref.Namespace+"."+ref.Name]; ok {
		if resolved.GoType == "syswinrt.EventRegistrationToken" {
			imports["syswinrt"] = Import{Path: SysWinRTImport}
		}
		return resolved
	}
	// A namespace with no Go equivalent at all. Diagnosed distinctly, because a
	// member skipped for this reason is skipped permanently — unlike a blocked
	// import edge or a generic, which could be fixed here.
	if reason, foreign := ingest.KnownForeignNamespaces[ref.Namespace]; foreign {
		return unsupported("foreign-type-skipped", "%s.%s: %s", ref.Namespace, ref.Name, reason)
	}
	if ref.Namespace != ctx.Namespace && m.Blocked[ctx.Namespace][ref.Namespace] {
		return unsupported("import-cycle-skipped", "reference to %s.%s crosses a severed import edge", ref.Namespace, ref.Name)
	}

	switch ref.TargetKind {
	case "Enum":
		return Resolved{GoType: m.qualifiedName(ref, ctx, imports), Kind: KindEnum}
	case "Struct":
		if !m.StructEmittable(ref.Namespace, ref.Name) {
			return unsupported("skipped-struct-ref", "reference to skipped struct %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{
			GoType:          m.qualifiedName(ref, ctx, imports),
			Kind:            KindStruct,
			StructNamespace: ref.Namespace,
			StructName:      ref.Name,
		}
	case "Interface":
		definition := m.Registry.Interface(ref.Namespace, ref.Name)
		if definition == nil {
			return unsupported("unresolved-typeref", "%s.%s", ref.Namespace, ref.Name)
		}
		// A generic interface has no Go type of its own in either module; only
		// closed instantiations of it are emitted.
		if definition.Arity > 0 {
			return unsupported("generic-member-skipped", "generic interface %s.%s", ref.Namespace, ref.Name)
		}
		return Resolved{GoType: "*" + m.qualifiedName(ref, ctx, imports), Kind: KindInterfacePtr}
	case "Class":
		return m.resolveClassRef(ref, ctx, imports)
	case "Delegate":
		if ctx.RequestDelegate != nil {
			if name, ok := ctx.RequestDelegate(ref); ok {
				return Resolved{GoType: "*" + name, Kind: KindDelegatePtr}
			}
		}
		return unsupported("delegate-param-skipped", "delegate %s.%s", ref.Namespace, ref.Name)
	case "":
		return unsupported("unresolved-typeref", "%s.%s", ref.Namespace, ref.Name)
	}
	return unsupported("unknown-target-kind", "%s.%s (%q)", ref.Namespace, ref.Name, ref.TargetKind)
}

// resolveClassRef lowers a runtime-class reference: a class in a signature means
// its default interface at the ABI, composable classes included (composition is
// instantiate-only, so the reference is still just the default interface
// pointer). When no emittable default interface is reachable — a statics-only
// class, a generic default interface, a severed import edge — the reference
// degrades to the raw IInspectable with an explanatory note rather than being
// dropped, because an IInspectable can still be passed along and queried.
func (m *Mapper) resolveClassRef(ref *wasdkmeta.TypeRef, ctx Context, imports ImportSet) Resolved {
	class := m.Registry.Class(ref.Namespace, ref.Name)
	if class == nil {
		return unsupported("unresolved-typeref", "%s.%s", ref.Namespace, ref.Name)
	}
	if class.DefaultInterface != nil && class.DefaultInterface.Kind == "ApiRef" {
		// The default interface is resolved in the CLASS's namespace, not the
		// referring one: an external class's default interface is named relative
		// to the external module, and the IR for it carries no External flag of
		// its own because that module never needed one.
		target := *class.DefaultInterface
		if ref.External {
			target.External = true
		}
		if resolved := m.GoType(&target, ctx, imports); resolved.Kind == KindInterfacePtr {
			return resolved
		}
	}
	imports["syswinrt"] = Import{Path: SysWinRTImport}
	return Resolved{
		GoType: "*syswinrt.IInspectable",
		Kind:   KindObjectPtr,
		Note:   fmt.Sprintf("class %s.%s is projected as IInspectable (no emittable default interface is reachable here)", ref.Namespace, ref.Name),
	}
}

// StructEmittable reports whether a struct's definition can be emitted: every
// field must resolve to a representable value shape. References to unemittable
// structs must degrade rather than name an undefined type.
//
// External structs go through the same predicate. That is deliberate: it decides
// whether go-bindings-winrt emitted a Go type for it, and it is that module's own
// rule, applied to that module's own metadata. Reproducing it is what stops a
// generated file naming a type that was never written.
func (m *Mapper) StructEmittable(namespace, name string) bool {
	key := namespace + "." + name
	if IsExternalType(namespace, name) {
		return true // never emitted here, but always representable
	}
	if verdict, seen := m.structEmittable[key]; seen {
		return verdict
	}
	definition := m.Registry.Struct(namespace, name)
	if definition == nil {
		return false
	}
	if m.structEmittable == nil {
		m.structEmittable = map[string]bool{}
	}
	// Optimistic seed breaks (metadata-invalid) reference cycles.
	m.structEmittable[key] = true
	verdict := true
	for i := range definition.Fields {
		scratch := ImportSet{}
		resolved := m.GoType(&definition.Fields[i].Type, Context{Namespace: namespace}, scratch)
		switch resolved.Kind {
		case KindScalar, KindFloat, KindBool, KindGUID, KindEnum, KindStruct:
			// Value shapes embed fine; Bool fields are one byte at the ABI,
			// which Go bool matches.
		default:
			verdict = false
		}
	}
	m.structEmittable[key] = verdict
	return verdict
}

// ImportPathFor returns the Go import path of the package carrying a namespace,
// in whichever module owns it.
func (m *Mapper) ImportPathFor(namespace string) string {
	if m.Registry.IsExternal(namespace) {
		return external.BindingsImportRoot + "/" + naming.ExternalPackagePath(namespace)
	}
	return m.ModulePath + "/bindings/winui/" + naming.PackagePath(namespace)
}

// AliasFor returns the import alias for a namespace. External aliases carry a
// prefix, because stripping the roots makes Microsoft.UI.Xaml.Interop and
// Windows.UI.Xaml.Interop the same identifier.
func (m *Mapper) AliasFor(namespace string) string {
	if m.Registry.IsExternal(namespace) {
		return naming.ExternalImportAlias(namespace)
	}
	return naming.ImportAlias(namespace)
}

// RuntimeImportPath returns the hand-written WinRT runtime this module reuses
// (alias winrt). Activation, QueryInterface, HSTRING and delegates all come from
// go-bindings-winrt; none of it is reimplemented here.
func (m *Mapper) RuntimeImportPath() string { return WinRTRuntimeImport }

// qualifiedName renders a reference's Name qualified by the owning package
// (recording the import) unless it lives in the namespace being emitted.
func (m *Mapper) qualifiedName(ref *wasdkmeta.TypeRef, ctx Context, imports ImportSet) string {
	name := naming.Export(ref.Name)
	if ref.Namespace == "" || ref.Namespace == ctx.Namespace {
		return name
	}
	alias := m.AliasFor(ref.Namespace)
	entry := Import{Path: m.ImportPathFor(ref.Namespace)}
	// Only local namespaces feed the emit closure. Chasing an external one would
	// ask this module to generate a package go-bindings-winrt already ships.
	if !m.Registry.IsExternal(ref.Namespace) {
		entry.Namespace = ref.Namespace
	}
	imports[alias] = entry
	return alias + "." + name
}

// unsupported builds the degradation result with its diagnostic reason.
func unsupported(key, format string, args ...any) Resolved {
	return Resolved{Kind: KindUnsupported, Reason: key + ": " + fmt.Sprintf(format, args...)}
}

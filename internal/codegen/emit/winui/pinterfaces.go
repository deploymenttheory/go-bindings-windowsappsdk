package emitwinui

// Generic-instantiation emission (pinterfaces).
//
// A closed generic INTERFACE instantiation referenced by an otherwise-emittable
// member does not have to degrade it. The gather layer monomorphizes the open
// interface's IR under the instantiation's arguments and emits a concrete Go type
// — its IID derived by the pinterface engine — into the CONSUMING package, deduped
// per package by mangled name.
//
// Instantiations are deliberately NOT emitted into the open type's home namespace.
// Arguments flow from arbitrary consumer namespaces into the collections
// namespaces, so emitting in that direction manufactures import cycles; and here
// the open type usually lives in go-bindings-winrt, which this module cannot add
// files to at all. The cost is duplication: two packages using the same
// instantiation each get their own copy — distinct Go types, identical ABI (same
// vtable slots, same derived IID), interoperable at the pointer level through
// QueryInterface.
//
// Discovery is demand-driven and transitively closed. Substituting arguments into
// an open interface's methods may surface further instantiations — IIterable<T>.First
// yields IIterator<T>, IVector<T>.GetView yields IVectorView<T> — which are queued
// and emitted into the same package until a fixed point.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// nativeMangles maps the IR's Native kinds to their instantiation-name atoms: the
// WinRT projected primitive names, not the Go ones, so a mangled name reads the
// same way it would in any other projection.
var nativeMangles = map[string]string{
	"HString": "String",
	"Bool":    "Bool",
	"Char16":  "Char16",
	"U1":      "UInt8",
	"I2":      "Int16",
	"U2":      "UInt16",
	"I4":      "Int32",
	"U4":      "UInt32",
	"I8":      "Int64",
	"U8":      "UInt64",
	"F32":     "Single",
	"F64":     "Double",
	"Guid":    "Guid",
	"Object":  "Object",
}

// instantiationName mangles a closed generic instantiation into its package-local
// Go type name: the open type's name with the backtick arity stripped, then "Of"
// plus each argument's mangled name joined by "And".
//
//	IVectorView`1<String>                        → IVectorViewOfString
//	TypedEventHandler`2<Object, RoutedEventArgs> → TypedEventHandlerOfObjectAndRoutedEventArgs
func instantiationName(ref *wasdkmeta.TypeRef) (string, error) {
	if ref.Kind != "GenericInst" {
		return "", fmt.Errorf("%s.%s is not a generic instantiation", ref.Namespace, ref.Name)
	}
	base := ref.Name
	if i := strings.IndexByte(base, '`'); i >= 0 {
		base = base[:i]
	}
	if len(ref.Args) == 0 {
		return "", fmt.Errorf("instantiation %s.%s has no type arguments", ref.Namespace, ref.Name)
	}
	argNames := make([]string, len(ref.Args))
	for i := range ref.Args {
		argName, err := argumentMangle(&ref.Args[i])
		if err != nil {
			return "", err
		}
		argNames[i] = argName
	}
	return naming.Export(base) + "Of" + strings.Join(argNames, "And"), nil
}

// argumentMangle names one instantiation argument.
func argumentMangle(ref *wasdkmeta.TypeRef) (string, error) {
	switch ref.Kind {
	case "Native":
		if atom, ok := nativeMangles[ref.Name]; ok {
			return atom, nil
		}
		return "", fmt.Errorf("native kind %q has no instantiation-name form", ref.Name)
	case "ApiRef":
		return naming.Export(ref.Name), nil
	case "GenericInst":
		return instantiationName(ref)
	}
	return "", fmt.Errorf("generic argument kind %q has no instantiation-name form", ref.Kind)
}

// cloneRef deep-copies a TypeRef.
func cloneRef(ref *wasdkmeta.TypeRef) wasdkmeta.TypeRef {
	out := *ref
	if len(ref.Args) > 0 {
		out.Args = make([]wasdkmeta.TypeRef, len(ref.Args))
		for i := range ref.Args {
			out.Args[i] = cloneRef(&ref.Args[i])
		}
	}
	if ref.Elem != nil {
		elem := cloneRef(ref.Elem)
		out.Elem = &elem
	}
	return out
}

// sameType reports whether two type references name the same type.
//
// Deliberately NOT reflect.DeepEqual. Two fields on a TypeRef record where a
// definition was found rather than which type it is:
//
//   - External says whether the definition lives in go-bindings-winrt, and its value
//     depends on WHOSE metadata the reference was read from. The same
//     IAsyncOperation`1<Bool> is External:true read from this module's JSON and
//     External:false when it surfaces through go-bindings-winrt's own IR for
//     AsyncOperationCompletedHandler`1.Invoke, because from inside that module
//     Windows.Foundation is local.
//   - TargetKind says whether the target is an interface, class or delegate, which
//     for a fixed namespace and name cannot differ.
//
// Neither can distinguish two different types, so comparing them rejects
// instantiations that are in fact identical. That is what the dedupe guard did: it
// refused 72 collection and async instantiations, and every member naming one
// degraded. Identity is the kind, the namespace, the name, the array element and the
// generic arguments, compared the same way all the way down.
func sameType(a, b *wasdkmeta.TypeRef) bool {
	if a.Kind != b.Kind || a.Namespace != b.Namespace || a.Name != b.Name || a.Index != b.Index {
		return false
	}
	if (a.Elem == nil) != (b.Elem == nil) {
		return false
	}
	if a.Elem != nil && !sameType(a.Elem, b.Elem) {
		return false
	}
	if len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if !sameType(&a.Args[i], &b.Args[i]) {
			return false
		}
	}
	return true
}

// substituteRef deep-copies ref with every GenericParamRef of index i replaced by
// args[i]. Substitution recurses through nested instantiation arguments and array
// elements, so arity-2 and nested-generic shapes ground correctly.
func substituteRef(ref *wasdkmeta.TypeRef, args []wasdkmeta.TypeRef) wasdkmeta.TypeRef {
	if ref.Kind == "GenericParamRef" && int(ref.Index) < len(args) {
		return cloneRef(&args[ref.Index])
	}
	out := *ref
	if len(ref.Args) > 0 {
		out.Args = make([]wasdkmeta.TypeRef, len(ref.Args))
		for i := range ref.Args {
			out.Args[i] = substituteRef(&ref.Args[i], args)
		}
	}
	if ref.Elem != nil {
		elem := substituteRef(ref.Elem, args)
		out.Elem = &elem
	}
	return out
}

// substituteMethod deep-copies a method with every generic parameter replaced under
// args. A nil args is a plain deep copy, and any leftover GenericParamRef degrades
// downstream rather than being guessed at.
func substituteMethod(method *wasdkmeta.Method, args []wasdkmeta.TypeRef) wasdkmeta.Method {
	out := wasdkmeta.Method{
		Name:              method.Name,
		Overload:          method.Overload,
		IsDefaultOverload: method.IsDefaultOverload,
	}
	for i := range method.Params {
		param := method.Params[i]
		param.Type = substituteRef(&param.Type, args)
		out.Params = append(out.Params, param)
	}
	if method.Return != nil {
		returnType := substituteRef(method.Return, args)
		out.Return = &returnType
	}
	return out
}

// instantiateInterface grounds an open interface's IR under the given type
// arguments. Methods stay in MethodDef order so vtable slots are preserved;
// properties, events and requires are deep-copied with every generic parameter
// substituted. GUID is left empty — the caller assigns the derived pinterface IID.
func instantiateInterface(open *wasdkmeta.Interface, args []wasdkmeta.TypeRef) *wasdkmeta.Interface {
	inst := &wasdkmeta.Interface{ExclusiveTo: open.ExclusiveTo}
	for i := range open.Requires {
		inst.Requires = append(inst.Requires, substituteRef(&open.Requires[i], args))
	}
	for i := range open.Methods {
		inst.Methods = append(inst.Methods, substituteMethod(&open.Methods[i], args))
	}
	for i := range open.Properties {
		property := open.Properties[i]
		property.Type = substituteRef(&property.Type, args)
		inst.Properties = append(inst.Properties, property)
	}
	for i := range open.Events {
		event := open.Events[i]
		event.Type = substituteRef(&event.Type, args)
		inst.Events = append(inst.Events, event)
	}
	return inst
}

// requestInstantiation is the typemap's RequestInstantiation callback for the
// namespace being emitted. It validates that the instantiation can be named and
// grounded, registers it — deduped by mangled name — for emission into the current
// package, and returns the package-local type name. ok == false degrades the
// requesting member.
func (g *Generator) requestInstantiation(ref *wasdkmeta.TypeRef) (string, bool) {
	mangled, err := instantiationName(ref)
	if err != nil {
		return "", false
	}
	if existing, seen := g.pinstByName[mangled]; seen {
		// The same mangled name must mean the same instantiation. Argument names
		// drop their namespaces, so two same-named arguments from different
		// namespaces would otherwise silently alias two distinct IIDs onto one Go
		// type — which compiles, and then fails at QueryInterface.
		if !sameType(existing, ref) {
			g.diag("instantiation-name-aliased", "%s and %s both mangle to %s",
				refDisplay(existing), refDisplay(ref), mangled)
			return "", false
		}
		return mangled, true
	}
	// The name and its IID var must both be free in the package: a metadata type
	// of the same name wins, and the requesting member degrades.
	if g.claimedNames[mangled] || g.claimedNames["IID_"+mangled] {
		return "", false
	}
	iid, err := pinterface.InstanceIID(ref, g.registry)
	if err != nil {
		return "", false // ungroundable: an unresolved argument, a missing [Guid]
	}
	g.claimedNames[mangled] = true
	clone := cloneRef(ref)
	g.pinstByName[mangled] = &clone
	g.pinstIID[mangled] = iid
	g.pinstQueue = append(g.pinstQueue, mangled)
	return mangled, true
}

// buildPinterfaceModels drains the instantiation worklist for the namespace being
// emitted. Building an instantiation's methods may request further instantiations —
// the typemap callback appends to the queue — so the loop runs to a fixed point,
// and the models are then sorted by mangled name for deterministic file content.
func (g *Generator) buildPinterfaceModels(meta *wasdkmeta.NamespaceMeta, imports typemap.ImportSet) []view.InterfaceModel {
	var models []view.InterfaceModel
	for len(g.pinstQueue) > 0 {
		mangled := g.pinstQueue[0]
		g.pinstQueue = g.pinstQueue[1:]
		inst := g.pinstByName[mangled]
		open := g.registry.Interface(inst.Namespace, inst.Name)
		if open == nil {
			// requestInstantiation resolved it, so this cannot happen from the
			// same registry — say so rather than panicking on a nil.
			g.diag("pinterface-open-type-lost", "%s.%s", inst.Namespace, inst.Name)
			continue
		}
		definition := instantiateInterface(open, inst.Args)
		definition.GUID = g.pinstIID[mangled]
		models = append(models, g.buildInterface(meta, refDisplay(inst), mangled, definition, imports))
	}
	sort.Slice(models, func(i, j int) bool { return models[i].TypeName < models[j].TypeName })
	return models
}

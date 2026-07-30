package emitwinui

// Event emission, and the delegate grounding it rests on.
//
// An interface's add_/remove_ accessors occupy vtable slots like any other method;
// the Event IR entry pairs them by MethodDef name and carries the delegate type. An
// emittable event becomes Add<Event> (taking a typed handler, returning the
// EventRegistrationToken) and Remove<Event> (taking the token), and the delegate is
// grounded into a package-local handler type: a typed wrapper over the runtime's
// Go-implemented Delegate whose constructor adapts the raw Invoke ABI words to
// typed callback arguments.
//
// This is the whole point of the exercise for WinUI. Button.Click, Window.Closed
// and every other interaction arrives as a delegate invocation, so a projection
// that could not ground one would be a library you can build a window with and then
// not respond to.
//
// Delegates ground on demand from two places — an EVENT accessor, and a method
// PARAMETER that references one — through the same rules. Methods RETURNING a
// delegate still degrade: handing a native delegate back to Go has no useful
// meaning, since there is no Go callback behind it. Delegate TypeDefs are not
// emitted into their home namespaces at all; like pinterfaces, two packages using
// the same delegate each get their own handler copy: distinct Go types, identical
// ABI, same IID.

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pinterface"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// maxDelegateParams is how many raw ABI words the delegate runtime's shared
// callback trampolines cover. syscall.NewCallback allocations are
// process-permanent, so go-bindings-winrt keeps one trampoline per vtable-slot
// shape rather than one per delegate instance — which bounds the arity.
const maxDelegateParams = 3

// eventUnloweable builds the degradation for an event whose delegate cannot be
// represented.
func eventUnloweable(format string, args ...any) *skip {
	return &skip{key: "event-delegate-unloweable", detail: fmt.Sprintf(format, args...)}
}

// buildAddAccessor lowers an add_<Event> accessor to
// Add<Event>(handler *<Handler>) (syswinrt.EventRegistrationToken, error),
// grounding the event's delegate type on demand. event is nil when no Event IR
// entry pairs the accessor.
func (g *Generator) buildAddAccessor(meta *wasdkmeta.NamespaceMeta, interfaceGoName string, method *wasdkmeta.Method, slot int, event *wasdkmeta.Event) (view.MethodModel, *skip) {
	if event == nil {
		return view.MethodModel{}, &skip{key: "event-skipped", detail: "no event metadata pairs this accessor"}
	}
	if len(method.Params) != 1 || method.Return == nil {
		return view.MethodModel{}, &skip{key: "event-skipped",
			detail: fmt.Sprintf("accessor %s has an unexpected shape", method.Name)}
	}
	handlerType, skipped := g.requestEventDelegate(meta, &event.Type)
	if skipped != nil {
		return view.MethodModel{}, skipped
	}
	goName := "Add" + naming.Export(event.Name)
	return view.MethodModel{
		GoName:     goName,
		Slot:       slot,
		ParamStr:   "handler *" + handlerType,
		ReturnSig:  "(syswinrt.EventRegistrationToken, error)",
		ReturnKind: view.RetValue,
		// The token is a native-written out-param, so it is heap-allocated through
		// new + winrt.OutParam. Registration ALWAYS reenters Go — the runtime
		// QueryInterfaces and AddRefs the Go-implemented handler being registered —
		// which makes this the clearest case of the out-param invariant, not a
		// speculative one. See buildMethod in interfaces.go.
		ResultDecl: "result := new(syswinrt.EventRegistrationToken)",
		ResultExpr: "*result",
		ZeroReturn: "syswinrt.EventRegistrationToken{}",
		ArgExprs:   []string{"handler.Ptr()", "uintptr(winrt.OutParam(unsafe.Pointer(result)))"},
		CommentLines: []string{
			fmt.Sprintf("%s (event add %s) dispatches through %s's vtable slot %d.",
				goName, method.Name, interfaceGoName, slot),
			"The handler stays registered (and referenced by the runtime) until the",
			fmt.Sprintf("returned token is passed to Remove%s.", naming.Export(event.Name)),
		},
	}, nil
}

// buildRemoveAccessor lowers a remove_<Event> accessor to
// Remove<Event>(token syswinrt.EventRegistrationToken) error.
//
// It does not need the delegate type, but the add accessor's grounding has already
// happened by the time this runs for the same event, so an unloweable event skips
// both accessors — and each leaves its own slot comment.
func (g *Generator) buildRemoveAccessor(interfaceGoName string, method *wasdkmeta.Method, slot int, event *wasdkmeta.Event) (view.MethodModel, *skip) {
	if event == nil {
		return view.MethodModel{}, &skip{key: "event-skipped", detail: "no event metadata pairs this accessor"}
	}
	if len(method.Params) != 1 || method.Return != nil {
		return view.MethodModel{}, &skip{key: "event-skipped",
			detail: fmt.Sprintf("accessor %s has an unexpected shape", method.Name)}
	}
	// The handler type is not named in this signature, but if the delegate could
	// not be grounded there is no Add to pair with, so the token this would take
	// could never have been obtained.
	if _, grounded := g.pdelByName[handlerNameFor(&event.Type)]; !grounded {
		return view.MethodModel{}, eventUnloweable("%s has no grounded handler", event.Name)
	}
	goName := "Remove" + naming.Export(event.Name)
	return view.MethodModel{
		GoName:     goName,
		Slot:       slot,
		ParamStr:   "token syswinrt.EventRegistrationToken",
		ReturnSig:  "error",
		ReturnKind: view.RetError,
		ArgExprs:   []string{"uintptr(token.Value)"},
		CommentLines: []string{
			fmt.Sprintf("%s (event remove %s) dispatches through %s's vtable slot %d,",
				goName, method.Name, interfaceGoName, slot),
			fmt.Sprintf("unregistering the %s handler the token was returned for.", event.Name),
		},
	}, nil
}

// handlerNameFor is the handler type name a delegate reference would ground to,
// without grounding it. Used to check whether the add accessor already succeeded.
func handlerNameFor(ref *wasdkmeta.TypeRef) string {
	switch ref.Kind {
	case "GenericInst":
		if name, err := instantiationName(ref); err == nil {
			return name
		}
	case "ApiRef":
		return naming.Export(ref.Name)
	}
	return ""
}

// delegateRequester adapts requestEventDelegate as the typemap's RequestDelegate
// seam for method parameters: a grounded delegate returns its handler type name,
// and any grounding failure degrades the parameter. The skip's
// event-delegate-unloweable framing is discarded here — the mapper reports a
// parameter under its own delegate keys, which is the honest attribution.
func (g *Generator) delegateRequester(meta *wasdkmeta.NamespaceMeta) func(ref *wasdkmeta.TypeRef) (string, bool) {
	return func(ref *wasdkmeta.TypeRef) (string, bool) {
		name, skipped := g.requestEventDelegate(meta, ref)
		if skipped != nil {
			return "", false
		}
		return name, true
	}
}

// requestEventDelegate grounds a delegate type — a closed generic instantiation
// such as TypedEventHandler`2<Object, RoutedEventArgs>, or a plain delegate
// reference such as RoutedEventHandler — into a package-local handler type, deduped
// by name, and returns the handler's Go type name. A non-nil skip degrades the
// requesting member.
func (g *Generator) requestEventDelegate(meta *wasdkmeta.NamespaceMeta, ref *wasdkmeta.TypeRef) (string, *skip) {
	var handlerName, iid string
	var invoke wasdkmeta.Method

	switch ref.Kind {
	case "GenericInst":
		open := g.registry.Delegate(ref.Namespace, ref.Name)
		if open == nil {
			return "", eventUnloweable("%s does not resolve to an open delegate", refDisplay(ref))
		}
		name, err := instantiationName(ref)
		if err != nil {
			return "", eventUnloweable("delegate instantiation %s cannot be named", refDisplay(ref))
		}
		// No declared [Guid] on a closed instantiation; the IID is derived from
		// the signature grammar, and every other projection derives the same one.
		derived, err := pinterface.InstanceIID(ref, g.registry)
		if err != nil {
			return "", eventUnloweable("delegate instantiation %s cannot be grounded: %v", refDisplay(ref), err)
		}
		handlerName, iid = name, derived
		invoke = substituteMethod(&open.Invoke, ref.Args)

	case "ApiRef":
		open := g.registry.Delegate(ref.Namespace, ref.Name)
		if open == nil || open.GUID == "" {
			return "", eventUnloweable("delegate %s unresolved or missing [Guid]", refDisplay(ref))
		}
		if open.Arity > 0 {
			return "", eventUnloweable("delegate %s is an open generic", refDisplay(ref))
		}
		handlerName = naming.Export(ref.Name)
		iid = open.GUID
		invoke = substituteMethod(&open.Invoke, nil)

	default:
		return "", eventUnloweable("delegate reference kind %q", ref.Kind)
	}

	if existing, seen := g.pdelByName[handlerName]; seen {
		// The same handler name must mean the same delegate. Mangled names drop
		// namespaces, so two same-named references could otherwise alias two
		// distinct IIDs onto one Go type.
		clone := cloneRef(ref)
		if !reflect.DeepEqual(*existing, clone) {
			return "", eventUnloweable("handler name %s is already bound to a different delegate", handlerName)
		}
		return handlerName, nil
	}
	if g.claimedNames[handlerName] || g.claimedNames["IID_"+handlerName] || g.claimedNames["New"+handlerName] {
		return "", eventUnloweable("handler name %s collides with an existing declaration", handlerName)
	}

	model, skipped := g.buildDelegateModel(meta, refDisplay(ref), handlerName, iid, &invoke)
	if skipped != nil {
		return "", skipped
	}
	g.claimedNames[handlerName] = true
	g.claimedNames["IID_"+handlerName] = true
	g.claimedNames["New"+handlerName] = true
	clone := cloneRef(ref)
	g.pdelByName[handlerName] = &clone
	g.pdelModels = append(g.pdelModels, model)
	return handlerName, nil
}

// buildDelegateModel lowers a grounded delegate's Invoke signature into the handler
// render model.
//
// The adapter converts each raw ABI word to a typed callback argument:
//
//   - interface, class and Object pointers → a typed pointer through an unsafe
//     cast. A BORROWED reference, valid only for the callback's duration.
//   - HString → winrt.HStringToString, which reads without consuming the source
//     handle: the event source still owns it.
//   - Bool → raw != 0.
//   - integer scalars and enums → a direct conversion.
//
// Anything else makes the event unloweable: floats (which never crossed the raw
// word intact in the first place), structs, arrays, and unresolved or unemittable
// types. So does an Invoke with a return value, an [out] parameter, or an arity
// past the delegate runtime's trampolines. A parameterless Invoke lowers fine —
// the constructor simply takes a func() — and that case matters more than it
// looks: DispatcherQueueHandler is parameterless, and it is what every TryEnqueue
// takes, so without it there would be no way to move work onto the UI thread at
// all.
func (g *Generator) buildDelegateModel(meta *wasdkmeta.NamespaceMeta, fullName, goName, iid string, invoke *wasdkmeta.Method) (view.DelegateModel, *skip) {
	if invoke.Return != nil {
		return view.DelegateModel{}, eventUnloweable("%s Invoke returns a value", fullName)
	}
	if len(invoke.Params) > maxDelegateParams {
		return view.DelegateModel{}, eventUnloweable("%s Invoke has %d parameters (0-%d supported)",
			fullName, len(invoke.Params), maxDelegateParams)
	}
	literal, err := guidLiteral(iid)
	if err != nil {
		return view.DelegateModel{}, eventUnloweable("%s IID: %v", fullName, err)
	}

	context := g.resolveContext(meta.Namespace)
	scratch := typemap.ImportSet{}
	model := view.DelegateModel{
		TypeName:   goName,
		FullName:   fullName,
		GUID:       iid,
		IIDVar:     "IID_" + goName,
		IIDLiteral: literal,
		CtorName:   "New" + goName,
		ParamCount: len(invoke.Params),
	}

	var decls, noteLines []string
	taken := map[string]bool{}
	var hasPointer, hasString bool
	for i := range invoke.Params {
		param := &invoke.Params[i]
		if param.Out {
			return view.DelegateModel{}, eventUnloweable("%s Invoke parameter %s is [out]", fullName, param.Name)
		}
		paramName := freshLocal(naming.ParamName(param.Name), taken)
		taken[paramName] = true
		resolved := g.mapper.GoType(&param.Type, context, scratch)
		word := fmt.Sprintf("raw[%d]", i)
		switch resolved.Kind {
		case typemap.KindInterfacePtr, typemap.KindObjectPtr:
			model.ArgExprs = append(model.ArgExprs, "("+resolved.GoType+")(unsafe.Pointer("+word+"))")
			hasPointer = true
		case typemap.KindString:
			model.ArgExprs = append(model.ArgExprs, "winrt.HStringToString(syswinrt.HSTRING("+word+"))")
			hasString = true
		case typemap.KindBool:
			model.ArgExprs = append(model.ArgExprs, word+" != 0")
		case typemap.KindScalar, typemap.KindEnum:
			model.ArgExprs = append(model.ArgExprs, resolved.GoType+"("+word+")")
		case typemap.KindUnsupported:
			return view.DelegateModel{}, eventUnloweable("%s Invoke parameter %s: %s",
				fullName, param.Name, splitReason(resolved.Reason).detail)
		default:
			return view.DelegateModel{}, eventUnloweable("%s Invoke parameter %s (%s) has no adapter conversion",
				fullName, param.Name, resolved.GoType)
		}
		if resolved.Note != "" {
			noteLines = append(noteLines, "Parameter "+paramName+"'s "+resolved.Note+".")
		}
		decls = append(decls, paramName+" "+resolved.GoType)
	}
	model.FnParams = strings.Join(decls, ", ")

	model.CtorCommentLines = []string{
		fmt.Sprintf("%s wraps fn as a COM-callable %s.", model.CtorName, fullName),
		"The handler starts with one Go-held reference; Close it once no native",
		"code can still invoke it.",
	}
	if hasPointer {
		model.CtorCommentLines = append(model.CtorCommentLines,
			"Pointer-typed callback arguments are BORROWED references owned by the",
			"event source for the duration of the callback: do not Release them or",
			"retain them past its return.")
	}
	if hasString {
		model.CtorCommentLines = append(model.CtorCommentLines,
			"String arguments are read without consuming the source HSTRING.")
	}
	model.CtorCommentLines = append(model.CtorCommentLines, noteLines...)

	g.pdelImports.Merge(scratch)
	return model, nil
}

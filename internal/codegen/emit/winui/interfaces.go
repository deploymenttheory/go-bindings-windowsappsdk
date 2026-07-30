package emitwinui

import (
	"fmt"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// interfaceFirstSlot is where an interface's own methods begin: IInspectable
// occupies slots 0-5.
const interfaceFirstSlot = 6

// skip describes why a member cannot be emitted: the diagnostic key plus a
// human-readable detail, which also lands in the slot comment.
type skip struct {
	key    string
	detail string
}

// emittedMethod records the Go surface of one emitted interface method, so a
// package-level wrapper (a factory or composable constructor) mirrors the
// generated method exactly instead of re-deriving a signature that could differ
// from it.
//
// The zero value marks a slot that was skipped, or that holds an event accessor.
type emittedMethod struct {
	emitted  bool
	goName   string
	paramStr string
	// paramDecls are the individual "name type" declarations paramStr joins,
	// index-aligned with paramNames. Recorded because a wrapper that restates only
	// a PREFIX of the parameters — a composable constructor supplies the trailing
	// composition pair itself — has to rebuild the list.
	paramDecls []string
	// paramNames are the declared Go parameter names in order: a wrapper's
	// pass-through arguments.
	paramNames []string
	// returnType is the logical Go return type ("" for none).
	returnType string
	// imports are the edges the method's signature and body need. A wrapper
	// restating the signature in another file must merge them into that file's
	// set, or the file names a package it has not imported.
	imports typemap.ImportSet
}

// splitReason converts a typemap degradation Reason ("key: detail") into a skip.
func splitReason(reason string) skip {
	key, detail, found := strings.Cut(reason, ": ")
	if !found {
		return skip{key: reason, detail: reason}
	}
	return skip{key: key, detail: detail}
}

// buildInterfaceModels converts a namespace's interfaces into vtable dispatch
// structs. Every non-generic interface is emitted, [ExclusiveTo] ones included —
// they ARE the ABI surface, and every runtime class is reached through one.
func (g *Generator) buildInterfaceModels(meta *wasdkmeta.NamespaceMeta, imports typemap.ImportSet) []view.InterfaceModel {
	models := make([]view.InterfaceModel, 0, len(meta.Interfaces))
	for _, name := range sortedKeys(meta.Interfaces) {
		definition := meta.Interfaces[name]
		if definition.Arity > 0 {
			// An open generic has no Go form; only closed instantiations of it are
			// emitted, into the packages that consume them.
			g.diag("generic-type-skipped", "interface %s.%s (arity %d)", meta.Namespace, name, definition.Arity)
			continue
		}
		goName := naming.Export(name)
		if !g.claimTypeName(goName) {
			g.diag("name-collision-skipped", "interface %s.%s", meta.Namespace, name)
			continue
		}
		models = append(models, g.buildInterface(meta, meta.Namespace+"."+name, goName, &definition, imports))
	}
	return models
}

// buildInterface lowers one interface definition — declared, or a grounded generic
// instantiation — into its render model.
//
// fullName is the display name for doc comments and diagnostics: the exact
// metadata name for a declared interface, the instantiation display form
// ("Windows.Foundation.Collections.IVectorView`1<String>") for a pinterface.
func (g *Generator) buildInterface(meta *wasdkmeta.NamespaceMeta, fullName, goName string, definition *wasdkmeta.Interface, imports typemap.ImportSet) view.InterfaceModel {
	model := view.InterfaceModel{
		TypeName:    goName,
		FullName:    fullName,
		GUID:        definition.GUID,
		ExclusiveTo: definition.ExclusiveTo,
	}
	for i := range definition.Requires {
		model.Requires = append(model.Requires, refDisplay(&definition.Requires[i]))
	}

	if definition.GUID != "" {
		literal, err := guidLiteral(definition.GUID)
		if err != nil {
			g.diag("malformed-guid", "interface %s: %v", fullName, err)
		} else if iidVar := "IID_" + goName; g.claimName(iidVar) {
			model.IIDVar = iidVar
			model.IIDLiteral = literal
		} else {
			g.diag("name-collision-skipped", "IID var for %s", fullName)
		}
	} else {
		g.diag("interface-missing-guid", "%s", fullName)
	}

	// Events reference their add_/remove_ accessors by MethodDef name; the
	// accessors themselves sit in Methods at their vtable slots.
	addEvents := map[string]*wasdkmeta.Event{}
	removeEvents := map[string]*wasdkmeta.Event{}
	for i := range definition.Events {
		event := &definition.Events[i]
		if event.AddMethod != "" {
			addEvents[event.AddMethod] = event
		}
		if event.RemoveMethod != "" {
			removeEvents[event.RemoveMethod] = event
		}
	}

	// Vtable methods in MethodDef order: slot = 6 + index. A skipped member NEVER
	// renumbers the ones after it — it leaves an audit comment at its own slot
	// instead, because renumbering would silently point every later method at the
	// wrong function.
	methodNames := map[string]bool{}
	records := make([]emittedMethod, len(definition.Methods))
	for i := range definition.Methods {
		method := &definition.Methods[i]
		slot := interfaceFirstSlot + i
		memberPath := model.FullName + "." + method.Name

		var methodModel view.MethodModel
		var skipped *skip
		var methodImports typemap.ImportSet
		plain := false
		switch {
		case strings.HasPrefix(method.Name, "add_"):
			methodModel, skipped = g.buildAddAccessor(meta, goName, method, slot, addEvents[method.Name])
		case strings.HasPrefix(method.Name, "remove_"):
			methodModel, skipped = g.buildRemoveAccessor(goName, method, slot, removeEvents[method.Name])
		default:
			// A per-method import set, merged into the file's set here and
			// recorded on the method so a wrapper restating the signature can
			// merge it into ITS file's set too.
			methodImports = typemap.ImportSet{}
			methodModel, skipped = g.buildMethod(meta, goName, method, slot, methodImports)
			imports.Merge(methodImports)
			plain = true
		}
		if skipped != nil {
			g.diag(skipped.key, "%s (%s)", memberPath, skipped.detail)
			model.Methods = append(model.Methods, view.MethodModel{
				SkipComment: fmt.Sprintf("slot %d: %s skipped: %s", slot, method.Name, skipped.detail),
			})
			continue
		}
		for methodNames[methodModel.GoName] {
			methodModel.GoName += "_"
		}
		methodNames[methodModel.GoName] = true
		model.Methods = append(model.Methods, methodModel)
		if plain {
			records[i] = emittedMethod{
				emitted:    true,
				goName:     methodModel.GoName,
				paramStr:   methodModel.ParamStr,
				paramDecls: splitParamDecls(methodModel.ParamStr),
				paramNames: goParamNames(method),
				returnType: logicalReturnType(methodModel.ReturnSig),
				imports:    methodImports,
			}
		}
	}
	// Recorded under the display full name: exact metadata names for declared
	// interfaces (what the factory gather looks up), and instantiation display
	// forms, which never collide with them.
	g.ifaceMethods[fullName] = records
	return model
}

// splitParamDecls recovers the individual "name type" declarations from a joined
// parameter string. Exact by construction: every lowered parameter type is a
// single identifier, a pointer, or a qualified name, so no emitted declaration
// ever contains ", ".
func splitParamDecls(paramStr string) []string {
	if paramStr == "" {
		return nil
	}
	return strings.Split(paramStr, ", ")
}

// goParamNames lists a method's Go parameter names in declaration order — exactly
// the names buildMethod declares, so a wrapper can pass them through.
func goParamNames(method *wasdkmeta.Method) []string {
	names := make([]string, len(method.Params))
	for i := range method.Params {
		names[i] = naming.ParamName(method.Params[i].Name)
	}
	return names
}

// logicalReturnType extracts the logical Go return type from a ReturnSig
// buildMethod produced ("(*IButton, error)" → "*IButton"; "error" → "").
func logicalReturnType(returnSig string) string {
	if returnSig == "error" {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(returnSig, "("), ", error)")
}

// buildMethod lowers one logical method to its ABI dispatch shape:
// SyscallN(LpVtbl[slot], self, lowered params…, retval out-pointer) →
// win32.ErrIfFailed. A nil skip means the method is emitted.
//
// Out-param invariant: every pointer the native side WRITES through — the trailing
// retval and any [out] parameter — must be heap-allocated, never a stack address.
// A WinRT call can reenter Go on the same goroutine, because any interface-typed
// argument could be a Go-implemented object whose QueryInterface or AddRef runs
// here; at that point a concurrent garbage collection may shrink-move this
// goroutine's stack and strand the native side's raw pointer. Retval locals are
// therefore declared `result := new(T)`, and every out-pointer word crosses through
// winrt.OutParam, which forces the pointee onto Go's non-moving heap. Applied
// uniformly: one small allocation per out-param per call is the accepted cost of
// never having to guess which calls can reenter.
func (g *Generator) buildMethod(meta *wasdkmeta.NamespaceMeta, interfaceGoName string, method *wasdkmeta.Method, slot int, imports typemap.ImportSet) (view.MethodModel, *skip) {
	metadataName := method.Name
	var goName, accessorNote string
	switch {
	case strings.HasPrefix(metadataName, "get_"):
		goName = naming.Export(strings.TrimPrefix(metadataName, "get_"))
		accessorNote = fmt.Sprintf(" (propget %s)", metadataName)
	case strings.HasPrefix(metadataName, "put_"):
		goName = "Set" + naming.Export(strings.TrimPrefix(metadataName, "put_"))
		accessorNote = fmt.Sprintf(" (propput %s)", metadataName)
	case method.Overload != "":
		goName = naming.Export(method.Overload)
	default:
		goName = naming.Export(metadataName)
	}

	context := g.resolveContext(meta.Namespace)
	// Parameters may ground a delegate reference into a package-local handler
	// type. Returns resolve WITHOUT that seam, so a method RETURNING a delegate
	// keeps degrading — handing a native delegate back to Go has no useful
	// meaning here, since there is no Go callback behind it to call. The return
	// context says so explicitly, so the degradation is reported as the policy it is
	// (delegate-return-skipped) rather than as a missing generics capability.
	returnContext := context
	returnContext.IsReturn = true
	paramContext := context
	paramContext.RequestDelegate = g.delegateRequester(meta)

	scratch := typemap.ImportSet{}
	var noteLines []string
	model := view.MethodModel{GoName: goName, Slot: slot}

	// Return shape first: the parameter preambles need the zero-value error
	// return to short-circuit with.
	errReturn := "return err"
	if method.Return != nil {
		resolved := g.mapper.GoType(method.Return, returnContext, scratch)
		switch resolved.Kind {
		case typemap.KindUnsupported:
			s := splitReason(resolved.Reason)
			return view.MethodModel{}, &s
		case typemap.KindString:
			model.ReturnKind = view.RetString
			model.ReturnSig = "(string, error)"
			model.ResultDecl = "result := new(syswinrt.HSTRING)"
			model.ResultExpr = "winrt.TakeHString(*result)"
			model.ZeroReturn = `""`
		case typemap.KindBool:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(bool, error)"
			model.ResultDecl = "result := new(byte)"
			model.ResultExpr = "*result != 0"
			model.ZeroReturn = "false"
		case typemap.KindScalar, typemap.KindEnum, typemap.KindFloat:
			// A WinRT return is an [out, retval] POINTER — the HRESULT is the
			// actual return — so a double comes back through memory the callee
			// writes, never through XMM0. Nothing about the lowering differs from
			// an integer scalar, which is why Opacity and FontSize are safe here
			// while a by-value float PARAMETER needs its bit pattern taken.
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultDecl = "result := new(" + resolved.GoType + ")"
			model.ResultExpr = "*result"
			model.ZeroReturn = "0"
		case typemap.KindGUID, typemap.KindStruct:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultDecl = "result := new(" + resolved.GoType + ")"
			model.ResultExpr = "*result"
			model.ZeroReturn = resolved.GoType + "{}"
		case typemap.KindInterfacePtr, typemap.KindObjectPtr, typemap.KindDelegatePtr:
			model.ReturnKind = view.RetValue
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultDecl = "result := new(" + resolved.GoType + ")"
			model.ResultExpr = "*result"
			model.ZeroReturn = "nil"
		case typemap.KindArray:
			// A returned conformant array is a RECEIVE array: the callee allocates
			// the buffer with CoTaskMemAlloc and the caller frees it, so this is the
			// one return shape that takes two out-words and owns memory afterwards.
			model.ReturnKind = view.RetArray
			model.ReturnSig = "(" + resolved.GoType + ", error)"
			model.ResultSizeDecl = "resultSize := new(uint32)"
			model.ResultDecl = "result := new(*" + resolved.Elem.GoType + ")"
			model.ResultElemType = resolved.Elem.GoType
			model.ZeroReturn = "nil"
		default:
			return view.MethodModel{}, &skip{key: "unsupported-return", detail: resolved.GoType}
		}
		if resolved.Note != "" {
			noteLines = append(noteLines, "The return value's "+resolved.Note+".")
		}
		errReturn = "return " + model.ZeroReturn + ", err"
	} else {
		model.ReturnKind = view.RetError
		model.ReturnSig = "error"
	}

	// Parameters, in metadata order.
	paramNames := make(map[string]bool, len(method.Params))
	for i := range method.Params {
		paramNames[naming.ParamName(method.Params[i].Name)] = true
	}
	var decls []string
	for i := range method.Params {
		param := &method.Params[i]
		paramName := naming.ParamName(param.Name)
		resolved := g.mapper.GoType(&param.Type, paramContext, scratch)
		if resolved.Kind == typemap.KindUnsupported {
			s := splitReason(resolved.Reason)
			return view.MethodModel{}, &s
		}
		if resolved.Kind == typemap.KindDelegatePtr {
			noteLines = append(noteLines, fmt.Sprintf(
				"A nil %s passes NULL at the ABI (WinRT accepts it where a handler may be cleared).", paramName))
		}
		if resolved.Note != "" {
			noteLines = append(noteLines, "Parameter "+paramName+"'s "+resolved.Note+".")
		}
		if param.Out {
			decl, preamble, args, skipped, postamble := lowerOutParam(paramName, param, resolved, paramNames)
			if skipped != nil {
				return view.MethodModel{}, skipped
			}
			// A postamble runs inside the success path, and the RetArray body already
			// owns that path to copy out of and free the callee's buffer. No member in
			// the metadata needs both, so the combination is refused rather than
			// given an untested body shape.
			if len(postamble) > 0 && model.ReturnKind == view.RetArray {
				return view.MethodModel{}, &skip{key: "out-param-skipped",
					detail: fmt.Sprintf("%s needs a conversion alongside an array return", paramName)}
			}
			decls = append(decls, decl)
			model.Preamble = append(model.Preamble, preamble...)
			model.ArgExprs = append(model.ArgExprs, args...)
			model.Postamble = append(model.Postamble, postamble...)
			continue
		}
		decl, preamble, args, skipped := g.lowerInParam(paramName, param, resolved, paramNames, errReturn)
		if skipped != nil {
			return view.MethodModel{}, skipped
		}
		decls = append(decls, decl)
		model.Preamble = append(model.Preamble, preamble...)
		model.ArgExprs = append(model.ArgExprs, args...)
	}
	if method.Return != nil {
		// A retval array is two words, count first, in the WinRT ReceiveArray order.
		if model.ReturnKind == view.RetArray {
			model.ArgExprs = append(model.ArgExprs, "uintptr(winrt.OutParam(unsafe.Pointer(resultSize)))")
		}
		model.ArgExprs = append(model.ArgExprs, "uintptr(winrt.OutParam(unsafe.Pointer(result)))")
	}
	model.ParamStr = strings.Join(decls, ", ")

	model.CommentLines = append(model.CommentLines, fmt.Sprintf(
		"%s%s dispatches through %s's vtable slot %d.", goName, accessorNote, interfaceGoName, slot))
	model.CommentLines = append(model.CommentLines, noteLines...)

	imports.Merge(scratch)
	return model, nil
}

// lowerOutParam lowers a non-retval [out] parameter to a Go pointer parameter passed
// straight through. Only shapes whose Go representation is ABI-identical qualify.
//
// The word crosses through winrt.OutParam (the out-param invariant, see buildMethod),
// which makes the parameter leak in the method's escape summary — so a caller's
// `&local` argument is itself moved to the heap at the call site, which is exactly
// what is needed.
//
// Returns the argument WORDS, plural: an array parameter occupies two.
func lowerOutParam(paramName string, param *wasdkmeta.Param, resolved typemap.Resolved, taken map[string]bool) (decl string, preamble []string, args []string, skipped *skip, postamble []string) {
	switch resolved.Kind {
	case typemap.KindScalar, typemap.KindEnum, typemap.KindStruct, typemap.KindGUID,
		typemap.KindFloat, typemap.KindInterfacePtr, typemap.KindObjectPtr:
		// KindFloat belongs here for the same reason it needs no special handling as a
		// return: an [out] parameter is a POINTER the callee writes through, so a
		// float32 crosses through memory and never through XMM0. Only a by-value float
		// PARAMETER needs its bit pattern taken. Leaving it out cost
		// ICompositionPropertySet.TryGetScalar while every TryGetVector3 beside it
		// worked, which is what made the omission visible.
		return paramName + " *" + resolved.GoType, nil,
			[]string{"uintptr(winrt.OutParam(unsafe.Pointer(" + paramName + ")))"}, nil, nil

	case typemap.KindArray:
		if param.ByRef {
			// A byref [out] array is a RECEIVE array: the callee allocates and writes
			// back both a count pointer and a buffer pointer, so the Go signature
			// would have to RETURN the slice rather than take one. There is exactly
			// one such member in the committed metadata
			// (ITextRangeProvider.GetBoundingRectangles) — not enough to justify a
			// promote-out-param-to-return path, but ample reason to refuse rather
			// than lower it as a fill array, which would hand the callee a count
			// where it expects a pointer.
			return "", nil, nil, &skip{key: "array-receive-param-skipped",
				detail: fmt.Sprintf("%s is a callee-allocated array; only fill arrays are lowered", paramName)}, nil
		}
		// A non-byref [out] array is a FILL array: the CALLER allocates and the
		// callee writes into it. The lowering is identical to an input array — the
		// difference is only who writes, which the doc comment records.
		decl, preamble, args, skipped = lowerArrayWords(paramName, resolved, taken)
		return decl, preamble, args, skipped, nil

	case typemap.KindString:
		// The ABI slot is an HSTRING the callee allocates and hands to the caller. The
		// caller's Go variable is a *string, so a raw slot is declared beside it and
		// converted after the call by winrt.TakeHString, which takes ownership and
		// deletes the handle — the same conversion an HSTRING RETURN already uses.
		//
		// Exposing the handle instead was the alternative, and it is a worse API: an
		// out HSTRING transfers ownership, so a caller who merely reads it leaks the
		// string, and nothing about the signature would say so.
		raw := rawLocal(paramName, taken)
		return paramName + " *string",
			[]string{raw + " := new(syswinrt.HSTRING)"},
			[]string{"uintptr(winrt.OutParam(unsafe.Pointer(" + raw + ")))"},
			nil, []string{"*" + paramName + " = winrt.TakeHString(*" + raw + ")"}

	case typemap.KindBool:
		// A WinRT boolean is one byte, and nothing guarantees it is 0 or 1 — so it
		// cannot be received into a *bool directly, which would let a stray byte become
		// a Go bool that is neither true nor false in comparisons.
		raw := rawLocal(paramName, taken)
		return paramName + " *bool",
			[]string{raw + " := new(byte)"},
			[]string{"uintptr(winrt.OutParam(unsafe.Pointer(" + raw + ")))"},
			nil, []string{"*" + paramName + " = *" + raw + " != 0"}
	}
	return "", nil, nil, &skip{key: "out-param-skipped",
		detail: fmt.Sprintf("out parameter %s not representable", paramName)}, nil
}

// rawLocal names the raw ABI slot declared beside a converted out-parameter, keeping
// it distinct from every parameter name already in scope.
func rawLocal(paramName string, taken map[string]bool) string {
	name := "_" + paramName + "Raw"
	for taken[name] {
		name += "_"
	}
	taken[name] = true
	return name
}

// lowerArrayWords lowers a slice parameter to the WinRT conformant-array pair: a
// count word and a data-pointer word.
//
// An empty slice passes (0, NULL), which is what WinRT expects and which also avoids
// indexing element zero of a slice that has none. The data pointer goes through
// winrt.OutParam for the same reason a by-value aggregate does: the callee holds it
// across a call that can reenter Go, and a moving stack would strand it.
func lowerArrayWords(paramName string, resolved typemap.Resolved, taken map[string]bool) (decl string, preamble []string, args []string, skipped *skip) {
	size := freshLocal("_"+paramName+"Size", taken)
	data := freshLocal("_"+paramName+"Data", taken)
	preamble = []string{
		size + " := uintptr(len(" + paramName + "))",
		data + " := uintptr(0)",
		"if len(" + paramName + ") > 0 {",
		data + " = uintptr(winrt.OutParam(unsafe.Pointer(&" + paramName + "[0])))",
		"}",
	}
	return paramName + " " + resolved.GoType, preamble, []string{size, data}, nil
}

// guidByValueSize is sizeof(win32.GUID): sixteen bytes, so a by-value GUID
// parameter always takes the by-reference path.
const guidByValueSize = 16

// lowerByValueAggregate lowers a by-value struct or GUID parameter under the
// Windows x64 rule, which keys on SIZE alone: an aggregate of 1, 2, 4 or 8 bytes
// travels in a general purpose register as an integer of that width, and any other
// size travels as a pointer to a caller-owned temporary. MSVC never puts an
// aggregate in an XMM register whatever its fields, so a two-float Point is an
// 8-byte integer word.
//
// The inline read is width-exact rather than a blanket 8-byte load: reading eight
// bytes out of a four-byte struct would read past it.
//
// The by-reference temporary goes through winrt.OutParam. That helper is named for
// the out-param invariant, but the hazard is identical in this direction — a native
// call can reenter Go on the same goroutine and a concurrent collection may then
// move the stack, leaving the callee reading freed memory through a stale pointer.
// Heap-escaping the argument closes it.
func lowerByValueAggregate(paramName, goType string, size int, taken map[string]bool) (decl string, preamble []string, args []string, skipped *skip) {
	decl = paramName + " " + goType
	if typemap.ClassifyAggregate(size) == typemap.ParamByRef {
		return decl, nil, []string{"uintptr(winrt.OutParam(unsafe.Pointer(&" + paramName + ")))"}, nil
	}
	local := freshLocal("_"+paramName, taken)
	var read string
	switch size {
	case 1:
		read = "uintptr(*(*uint8)(unsafe.Pointer(&" + paramName + ")))"
	case 2:
		read = "uintptr(*(*uint16)(unsafe.Pointer(&" + paramName + ")))"
	case 4:
		read = "uintptr(*(*uint32)(unsafe.Pointer(&" + paramName + ")))"
	default:
		read = "*(*uintptr)(unsafe.Pointer(&" + paramName + "))"
	}
	return decl, []string{local + " := " + read}, []string{local}, nil
}

// lowerInParam lowers one input parameter to its SyscallN argument word, with any
// conversion preamble.
func (g *Generator) lowerInParam(paramName string, param *wasdkmeta.Param, resolved typemap.Resolved, taken map[string]bool, errReturn string) (decl string, preamble []string, args []string, skipped *skip) {
	switch resolved.Kind {
	case typemap.KindScalar, typemap.KindEnum:
		return paramName + " " + resolved.GoType, nil, []string{"uintptr(" + paramName + ")"}, nil

	case typemap.KindBool:
		local := freshLocal("_"+paramName, taken)
		preamble = []string{
			local + " := uintptr(0)",
			"if " + paramName + " {",
			local + " = 1",
			"}",
		}
		return paramName + " bool", preamble, []string{local}, nil

	case typemap.KindString:
		local := freshLocal("h"+naming.Export(paramName), taken)
		preamble = []string{
			local + ", err := winrt.NewHString(" + paramName + ")",
			"if err != nil {",
			errReturn,
			"}",
			"defer " + local + ".Close()",
		}
		return paramName + " string", preamble, []string{"uintptr(" + local + ".Raw())"}, nil

	case typemap.KindFloat:
		// amd64 asmstdcall mirrors each of the first four argument words into
		// XMM0-XMM3 before the call, and arguments five and beyond occupy the same
		// stack slot whatever their type — so a float travels as its bit pattern in
		// an ordinary argument word. This is the path Opacity, Width and FontSize
		// take, and it is why arm64 is excluded: its asm never loads V0-V7.
		local := freshLocal("_"+paramName, taken)
		bits := "math.Float64bits"
		if resolved.GoType == "float32" {
			// The low 32 bits of the word land in the low 32 bits of the XMM
			// register, which is exactly where a single is read from.
			bits = "math.Float32bits"
		}
		preamble = []string{local + " := uintptr(" + bits + "(" + paramName + "))"}
		return paramName + " " + resolved.GoType, preamble, []string{local}, nil

	case typemap.KindStruct:
		layout, ok := g.mapper.StructLayout(resolved.StructNamespace, resolved.StructName)
		if !ok {
			return "", nil, nil, &skip{key: "byval-struct-param-skipped",
				detail: fmt.Sprintf("by-value %s.%s parameter %s has no computable amd64 layout",
					resolved.StructNamespace, resolved.StructName, paramName)}
		}
		return lowerByValueAggregate(paramName, resolved.GoType, layout.Size, taken)

	case typemap.KindGUID:
		return lowerByValueAggregate(paramName, resolved.GoType, guidByValueSize, taken)

	case typemap.KindArray:
		return lowerArrayWords(paramName, resolved, taken)

	case typemap.KindInterfacePtr, typemap.KindObjectPtr:
		return paramName + " " + resolved.GoType, nil,
			[]string{"uintptr(unsafe.Pointer(" + paramName + "))"}, nil

	case typemap.KindDelegatePtr:
		// A Go-implemented handler crosses as its COM object pointer; nil passes
		// NULL, which WinRT accepts wherever a handler may be cleared.
		local := freshLocal("_"+paramName, taken)
		preamble = []string{
			local + " := uintptr(0)",
			"if " + paramName + " != nil {",
			local + " = " + paramName + ".Ptr()",
			"}",
		}
		return paramName + " " + resolved.GoType, preamble, []string{local}, nil
	}
	return "", nil, nil, &skip{key: "unsupported-param",
		detail: fmt.Sprintf("parameter %s (%s)", param.Name, resolved.GoType)}
}

// freshLocal returns a local identifier that collides with no parameter.
func freshLocal(candidate string, taken map[string]bool) string {
	for taken[candidate] {
		candidate += "_"
	}
	return candidate
}

// refDisplay renders a TypeRef for doc comments: full metadata names, generic
// arguments in angle brackets, primitives under their projected WinRT names —
// IVectorView`1<String>.
func refDisplay(ref *wasdkmeta.TypeRef) string {
	switch ref.Kind {
	case "Native":
		if projected, ok := nativeMangles[ref.Name]; ok {
			return projected
		}
		return ref.Name
	case "GenericParamRef":
		return fmt.Sprintf("T%d", ref.Index)
	case "Array":
		if ref.Elem != nil {
			return refDisplay(ref.Elem) + "[]"
		}
		return "[]"
	}
	name := ref.Name
	if ref.Namespace != "" {
		name = ref.Namespace + "." + name
	}
	if len(ref.Args) > 0 {
		args := make([]string, len(ref.Args))
		for i := range ref.Args {
			args[i] = refDisplay(&ref.Args[i])
		}
		name += "<" + strings.Join(args, ", ") + ">"
	}
	return name
}

package emitwinui

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// buildClassModels converts a namespace's runtime classes into render models: a
// struct embedding the default interface by value, a New<Class> constructor when
// the class is directly activatable, an As<Interface> query method per other
// instance interface, package-level statics accessors for the [Static] interfaces,
// and package-level constructors for the [Activatable] and [Composable] factory
// interfaces.
//
// Statics accessors are independent of the class type: a statics-only class emits
// them with no class type at all, because there is no instance to be had. A
// factory-less composable class is also a valid platform shape — Compositor
// hands out Visual objects rather than letting one be constructed — so it gets a
// class type, queries and statics and no diagnostic.
func (g *Generator) buildClassModels(meta *wasdkmeta.NamespaceMeta, imports typemap.ImportSet) []view.ClassModel {
	models := make([]view.ClassModel, 0, len(meta.Classes))
	for _, name := range sortedKeys(meta.Classes) {
		class := meta.Classes[name]
		fullName := meta.Namespace + "." + name
		model, typeEmitted := g.buildClassType(meta, name, fullName, &class, imports)
		model.Statics = g.buildStaticsAccessors(meta, fullName, &class, imports)
		if typeEmitted {
			model.Factories = g.buildFactoryFuncs(meta, fullName, &class, &model, imports)
			model.ComposableCtors = g.buildComposableCtorFuncs(meta, fullName, &class, &model, imports)
		} else {
			for _, factory := range class.ActivatableFactories {
				g.diag("factory-skipped", "%s factory %s (class type not emitted)", fullName, factory)
			}
			for _, factory := range class.ComposableFactories {
				g.diag("composable-factory-skipped", "%s factory %s (class type not emitted)", fullName, factory)
			}
		}
		if typeEmitted || len(model.Statics) > 0 {
			models = append(models, model)
		}
	}
	return models
}

// buildClassType builds the class-type part of the model: the struct, the direct
// constructor, and the query methods. false means the class type is not emitted —
// silently for statics-only classes, whose accessors still render, and with a
// diagnostic otherwise.
func (g *Generator) buildClassType(meta *wasdkmeta.NamespaceMeta, name, fullName string, class *wasdkmeta.Class, imports typemap.ImportSet) (view.ClassModel, bool) {
	if class.DefaultInterface == nil {
		// A statics-only class has nothing to instantiate: no type and no
		// diagnostic, because the accessors are its whole projection.
		if !(len(class.StaticInterfaces) > 0 && len(class.Interfaces) == 0) {
			g.diag("class-default-missing-skipped", "%s", fullName)
		}
		return view.ClassModel{FullName: fullName}, false
	}
	if class.DefaultInterface.Kind != "ApiRef" {
		g.diag("class-default-generic-skipped", "%s (default %s)", fullName, refDisplay(class.DefaultInterface))
		return view.ClassModel{FullName: fullName}, false
	}

	context := g.resolveContext(meta.Namespace)
	scratch := typemap.ImportSet{}
	resolvedDefault := g.mapper.GoType(class.DefaultInterface, context, scratch)
	if resolvedDefault.Kind != typemap.KindInterfacePtr {
		g.diag("class-default-generic-skipped", "%s (default %s not emittable)", fullName, refDisplay(class.DefaultInterface))
		return view.ClassModel{FullName: fullName}, false
	}
	defaultIIDRef, ok := g.iidRef(class.DefaultInterface, meta.Namespace)
	if !ok {
		g.diag("class-default-missing-skipped", "%s (default interface has no IID)", fullName)
		return view.ClassModel{FullName: fullName}, false
	}

	goName := naming.Export(name)
	if !g.claimTypeName(goName) {
		g.diag("name-collision-skipped", "class %s", fullName)
		return view.ClassModel{FullName: fullName}, false
	}
	model := view.ClassModel{
		TypeName:         goName,
		FullName:         fullName,
		DefaultInterface: strings.TrimPrefix(resolvedDefault.GoType, "*"),
		DefaultIIDRef:    defaultIIDRef,
	}

	// Direct activation, through RoActivateInstance.
	if class.ActivatableDirect {
		if ctor := "New" + goName; g.claimName(ctor) {
			model.CtorName = ctor
		} else {
			g.diag("name-collision-skipped", "constructor New%s for %s", goName, fullName)
		}
	}

	// Query methods, in two passes: the class's own instance interfaces, then
	// everything inherited up the Extends chain. Own interfaces first so that if a
	// name ever collided, the class's own member is the one that keeps it.
	methodNames := map[string]bool{}
	for i := range class.Interfaces {
		target := &class.Interfaces[i]
		if target.Namespace == class.DefaultInterface.Namespace && target.Name == class.DefaultInterface.Name {
			continue // the default is embedded, not queried
		}
		g.addQueryMethod(&model, target, fullName, "", meta.Namespace, context, scratch, methodNames)
	}
	g.addInheritedQueryMethods(&model, class, fullName, meta.Namespace, context, scratch, methodNames)

	imports.Merge(scratch)
	return model, true
}

// addInheritedQueryMethods walks the class's Extends chain and projects each base
// class's instance interfaces as query methods on the derived class.
//
// This is what makes the generated surface usable. A COM object implements every
// interface in its hierarchy, so QueryInterface reaches all of them — but a class's
// InterfaceImpl list names only the interfaces declared at its own level. Button's
// is [IButton], which carries Flyout and nothing else: Click is on
// Primitives.IButtonBase and Content on IContentControl, two and three levels up.
// XAML's [ExclusiveTo] interfaces carry no Requires either, so without walking
// Extends a generated Button could not reach anything a button is used for.
//
// The default interface of a BASE class is queried like any other; only the class's
// own default is embedded.
//
// The traversal is the Registry's, shared with ComputeBlockedImports, so the edges
// projected here are exactly the edges the cycle breaker accounted for.
func (g *Generator) addInheritedQueryMethods(model *view.ClassModel, class *wasdkmeta.Class, fullName, fromNamespace string, context typemap.Context, scratch typemap.ImportSet, methodNames map[string]bool) {
	problems := g.registry.WalkBaseChain(class, func(baseFullName string, base *wasdkmeta.Class) {
		for i := range base.Interfaces {
			g.addQueryMethod(model, &base.Interfaces[i], fullName, baseFullName, fromNamespace, context, scratch, methodNames)
		}
	})
	for _, problem := range problems {
		g.diag("base-chain-incomplete", "%s: %s", fullName, problem)
	}
}

// addQueryMethod resolves one interface reference and appends its As<Interface>
// query method, or records why it could not. inheritedFrom names the base class the
// interface came from, and is empty for the class's own interfaces.
func (g *Generator) addQueryMethod(model *view.ClassModel, target *wasdkmeta.TypeRef, fullName, inheritedFrom, fromNamespace string, context typemap.Context, scratch typemap.ImportSet, methodNames map[string]bool) {
	asName := naming.InterfaceAsName(target.Name)
	memberPath := fullName + "." + asName

	resolved := g.mapper.GoType(target, context, scratch)
	if resolved.Kind != typemap.KindInterfacePtr {
		s := splitReason(resolved.Reason)
		if resolved.Kind != typemap.KindUnsupported {
			s = skip{key: "class-interface-skipped", detail: refDisplay(target) + " is not an emittable interface"}
		}
		key := s.key
		if inheritedFrom != "" {
			// Distinct key: an inherited interface lost to a severed import edge is
			// a consequence of the cycle breaking, not of anything about this
			// class, and conflating the two would make the counts unreadable.
			key = "inherited-interface-skipped"
		}
		g.diag(key, "%s (%s)", memberPath, s.detail)
		return
	}
	if target.Kind == "GenericInst" {
		// The instantiation is package-local under its mangled name, so the query
		// method follows that rather than the backtick-arity metadata name.
		asName = naming.InterfaceAsName(strings.TrimPrefix(resolved.GoType, "*"))
		memberPath = fullName + "." + asName
	}
	iidRef, ok := g.iidRef(target, fromNamespace)
	if !ok {
		g.diag("class-interface-skipped", "%s (%s has no IID)", memberPath, refDisplay(target))
		return
	}
	if methodNames[asName] {
		// Silent for an inherited duplicate: a class re-declaring an interface its
		// base already declares is ordinary, and the derived one already won.
		if inheritedFrom == "" {
			g.diag("name-collision-skipped", "%s", memberPath)
		}
		return
	}
	methodNames[asName] = true

	var note string
	if inheritedFrom != "" {
		note = "Inherited from " + inheritedFrom + "."
	}
	model.QueryMethods = append(model.QueryMethods, view.QueryMethodModel{
		GoName:        asName,
		InterfaceType: strings.TrimPrefix(resolved.GoType, "*"),
		IIDRef:        iidRef,
		Note:          note,
	})
}

// buildStaticsAccessors projects a class's [Static] interfaces as package-level
// accessor functions returning the statics interface fetched through the class's
// activation factory.
//
// GetActivationFactory queries the statics IID itself, so the pointer it returns IS
// the statics interface — every generated interface struct is a single
// vtable-pointer word, which is what makes the re-type sound.
func (g *Generator) buildStaticsAccessors(meta *wasdkmeta.NamespaceMeta, fullName string, class *wasdkmeta.Class, imports typemap.ImportSet) []view.StaticsAccessorModel {
	if len(class.StaticInterfaces) == 0 {
		return nil
	}
	context := g.resolveContext(meta.Namespace)
	statics := slices.Clone(class.StaticInterfaces)
	sort.Strings(statics)
	var models []view.StaticsAccessorModel
	for _, staticFullName := range statics {
		ref, ok := interfaceRef(staticFullName)
		if !ok {
			g.diag("statics-skipped", "%s (%s is not a namespace-qualified name)", fullName, staticFullName)
			continue
		}
		scratch := typemap.ImportSet{}
		resolved := g.mapper.GoType(&ref, context, scratch)
		if resolved.Kind != typemap.KindInterfacePtr {
			g.diag("statics-skipped", "%s (%s: %s)", fullName, staticFullName, splitReason(resolved.Reason).detail)
			continue
		}
		iidRef, ok := g.iidRef(&ref, meta.Namespace)
		if !ok {
			g.diag("statics-skipped", "%s (%s has no IID)", fullName, staticFullName)
			continue
		}
		funcName := naming.StaticsAccessorName(ref.Name)
		if !g.claimName(funcName) {
			g.diag("name-collision-skipped", "statics accessor %s for %s", funcName, fullName)
			continue
		}
		models = append(models, view.StaticsAccessorModel{
			FuncName:          funcName,
			InterfaceType:     strings.TrimPrefix(resolved.GoType, "*"),
			InterfaceFullName: staticFullName,
			IIDRef:            iidRef,
			ClassFullName:     fullName,
		})
		imports.Merge(scratch)
	}
	return models
}

// buildFactoryFuncs projects a class's [Activatable] factory interfaces as
// package-level constructors: each emitted factory method becomes a function that
// fetches the factory, delegates to the generated interface method — so the
// parameter lowering is exactly the method's, and the two cannot drift — and wraps
// the returned default-interface pointer as the class type.
//
// The factory is fetched per call; a factory cache is a future optimization.
// Adopting a method's signature adopts its import edges, so the recorded per-method
// imports merge into the classes file's set.
func (g *Generator) buildFactoryFuncs(meta *wasdkmeta.NamespaceMeta, fullName string, class *wasdkmeta.Class, model *view.ClassModel, imports typemap.ImportSet) []view.FactoryFuncModel {
	var models []view.FactoryFuncModel
	for ordinal, factoryFullName := range class.ActivatableFactories {
		ref, definition, iidRef, ok := g.resolveFactory(meta, fullName, factoryFullName, "factory-skipped")
		if !ok {
			continue
		}
		records := g.ifaceMethods[factoryFullName]
		for i := range definition.Methods {
			method := &definition.Methods[i]
			memberPath := fullName + " factory " + factoryFullName
			var record emittedMethod
			if i < len(records) {
				record = records[i]
			}
			if !record.emitted {
				g.diag("factory-skipped", "%s (method %s not emitted on the factory interface)", memberPath, method.Name)
				continue
			}
			// The wrapper's unsafe re-type is only sound when the factory method
			// hands back the class's default interface.
			if record.returnType != "*"+model.DefaultInterface {
				g.diag("factory-skipped", "%s (method %s does not return the class default interface)", memberPath, method.Name)
				continue
			}
			funcName, ok := g.claimCtorName(projectedName(method), model.TypeName, ordinal, fullName, "factory constructor")
			if !ok {
				continue
			}
			imports.Merge(record.imports)
			models = append(models, view.FactoryFuncModel{
				FuncName:        funcName,
				FactoryType:     naming.Export(ref.Name),
				FactoryFullName: factoryFullName,
				FactoryIIDRef:   iidRef,
				MethodName:      record.goName,
				ParamStr:        record.paramStr,
				ArgNames:        record.paramNames,
			})
		}
	}
	return models
}

// buildComposableCtorFuncs projects a class's [Composable] factory interfaces as
// package-level constructors. This is the path most of the WinUI surface takes:
// nearly every XAML class is composable, so for most of the tree it is the only
// way to make an instance at all.
//
// Composition is instantiate-only. Each emitted factory method whose trailing
// parameter pair is the composition contract — baseInterface (in Object) plus
// innerInterface (out Object) — and whose return is the class's default interface
// becomes a function taking only the LEADING parameters. The body fetches the
// factory, calls the generated method with a NULL outer and a heap-escaping inner
// out-pointer, releases the returned non-nil inner (under null-outer composition it
// is a second reference to the object the retval already carries), and re-types the
// default-interface pointer as the class.
//
// Go-side derivation — a non-null outer — needs COM aggregation support that does
// not exist yet, and is deliberately out of scope.
func (g *Generator) buildComposableCtorFuncs(meta *wasdkmeta.NamespaceMeta, fullName string, class *wasdkmeta.Class, model *view.ClassModel, imports typemap.ImportSet) []view.ComposableCtorModel {
	var models []view.ComposableCtorModel
	for ordinal, factoryFullName := range class.ComposableFactories {
		ref, definition, iidRef, ok := g.resolveFactory(meta, fullName, factoryFullName, "composable-factory-skipped")
		if !ok {
			continue
		}
		records := g.ifaceMethods[factoryFullName]
		for i := range definition.Methods {
			method := &definition.Methods[i]
			memberPath := fullName + " composable factory " + factoryFullName
			var record emittedMethod
			if i < len(records) {
				record = records[i]
			}
			if !record.emitted {
				g.diag("composable-factory-skipped", "%s (method %s not emitted on the factory interface)", memberPath, method.Name)
				continue
			}
			if !composableTailParams(method) {
				g.diag("composable-factory-skipped",
					"%s (method %s does not end with the (baseInterface in, innerInterface out) Object pair)",
					memberPath, method.Name)
				continue
			}
			if record.returnType != "*"+model.DefaultInterface {
				g.diag("composable-factory-skipped", "%s (method %s does not return the class default interface)", memberPath, method.Name)
				continue
			}
			// CreateInstance → New<Class>; CreateInstanceWith<X> →
			// New<Class>With<X>; anything else keeps its projected name after the
			// New<Class> stem.
			projected := projectedName(method)
			suffix, isCreateInstance := strings.CutPrefix(projected, "CreateInstance")
			if !isCreateInstance {
				suffix = naming.Export(projected)
			}
			funcName, ok := g.claimCtorName("New"+model.TypeName+suffix, model.TypeName, ordinal, fullName, "composable constructor")
			if !ok {
				continue
			}
			// Leading parameters only: the constructor supplies the trailing
			// composition pair itself.
			leadingDecls := record.paramDecls[:len(record.paramDecls)-2]
			leadingNames := record.paramNames[:len(record.paramNames)-2]
			taken := make(map[string]bool, len(leadingNames))
			for _, name := range leadingNames {
				taken[name] = true
			}
			innerName := freshLocal("inner", taken)
			argNames := make([]string, 0, len(leadingNames)+2)
			argNames = append(argNames, leadingNames...)
			argNames = append(argNames, "nil", innerName)
			imports.Merge(record.imports)
			models = append(models, view.ComposableCtorModel{
				FuncName:        funcName,
				FactoryType:     naming.Export(ref.Name),
				FactoryFullName: factoryFullName,
				FactoryIIDRef:   iidRef,
				MethodName:      record.goName,
				ParamStr:        strings.Join(leadingDecls, ", "),
				InnerName:       innerName,
				ArgNames:        argNames,
			})
		}
	}
	return models
}

// resolveFactory validates a factory interface reference and returns what both
// factory gathers need from it. diagKey distinguishes the caller in diagnostics.
//
// A factory interface must live in the class's own namespace. They are
// [ExclusiveTo] their class in practice, and this generator only records the
// emitted method surface for interfaces in the namespace currently being built —
// so a cross-namespace factory has no surface to delegate to.
func (g *Generator) resolveFactory(meta *wasdkmeta.NamespaceMeta, classFullName, factoryFullName, diagKey string) (wasdkmeta.TypeRef, *wasdkmeta.Interface, string, bool) {
	ref, ok := interfaceRef(factoryFullName)
	if !ok {
		g.diag(diagKey, "%s factory %s (not a namespace-qualified name)", classFullName, factoryFullName)
		return ref, nil, "", false
	}
	definition := g.registry.Interface(ref.Namespace, ref.Name)
	switch {
	case definition == nil:
		g.diag(diagKey, "%s factory %s (unresolved)", classFullName, factoryFullName)
		return ref, nil, "", false
	case definition.Arity > 0:
		g.diag(diagKey, "%s factory %s (generic)", classFullName, factoryFullName)
		return ref, nil, "", false
	case !g.mapper.SamePackage(meta.Namespace, ref.Namespace):
		// The emitted method surface is recorded per package, so a factory outside it
		// has nothing to delegate to. Clustering widens this: a factory in a sibling
		// namespace of the same package is now reachable.
		g.diag(diagKey, "%s factory %s (outside the class package)", classFullName, factoryFullName)
		return ref, nil, "", false
	}
	iidRef, ok := g.iidRef(&ref, meta.Namespace)
	if !ok {
		g.diag(diagKey, "%s factory %s (no IID)", classFullName, factoryFullName)
		return ref, nil, "", false
	}
	return ref, definition, iidRef, true
}

// claimCtorName reserves a package-level constructor name, with a deterministic
// fallback chain.
//
// Bare factory method names recur across classes in a dense package — Create and
// CreateInstance are everywhere in XAML — so a loser first gains its class name
// (Create → CreateButton), unless it already carries it, where disambiguating
// within the class is what is actually needed; then it takes the factory's 1-based
// attribute ordinal. Deterministic at every step, because regeneration has to
// reproduce.
func (g *Generator) claimCtorName(candidate, typeName string, ordinal int, classFullName, what string) (string, bool) {
	if g.claimName(candidate) {
		return candidate, true
	}
	withType := candidate
	if !strings.HasSuffix(withType, typeName) {
		withType += typeName
	}
	switch suffixed := withType + strconv.Itoa(ordinal+1); {
	case withType != candidate && g.claimName(withType):
		return withType, true
	case g.claimName(suffixed):
		return suffixed, true
	}
	g.diag("name-collision-skipped", "%s %s for %s", what, candidate, classFullName)
	return "", false
}

// projectedName is the unique name a method projects under: the [Overload]
// attribute's name when present, since WinRT overloads share one MethodDef name.
func projectedName(method *wasdkmeta.Method) string {
	if method.Overload != "" {
		return method.Overload
	}
	return method.Name
}

// composableTailParams reports whether a composable factory method's last two
// parameters are the composition contract: baseInterface (in Object) plus
// innerInterface (out Object).
func composableTailParams(method *wasdkmeta.Method) bool {
	if len(method.Params) < 2 {
		return false
	}
	base := &method.Params[len(method.Params)-2]
	inner := &method.Params[len(method.Params)-1]
	isObject := func(ref *wasdkmeta.TypeRef) bool {
		return ref.Kind == "Native" && ref.Name == "Object"
	}
	return !base.Out && isObject(&base.Type) && inner.Out && isObject(&inner.Type)
}

// interfaceRef builds the ApiRef for a full interface metadata name; false when the
// name has no namespace segment.
func interfaceRef(fullName string) (wasdkmeta.TypeRef, bool) {
	dot := strings.LastIndex(fullName, ".")
	if dot < 0 {
		return wasdkmeta.TypeRef{}, false
	}
	return wasdkmeta.TypeRef{
		Kind: "ApiRef", Namespace: fullName[:dot], Name: fullName[dot+1:], TargetKind: "Interface",
	}, true
}

// iidRef builds the address expression of an interface's IID variable
// ("&IID_IButton", "&wrtfoundation.IID_IStringable"); false when the interface
// carries no GUID.
//
// A generic instantiation resolves to the derived IID var emitted alongside it in
// the consuming package, which is always package-local.
func (g *Generator) iidRef(ref *wasdkmeta.TypeRef, fromNamespace string) (string, bool) {
	if ref.Kind == "GenericInst" {
		mangled, err := instantiationName(ref)
		if err != nil {
			return "", false
		}
		return "&IID_" + mangled, true
	}
	definition := g.registry.Interface(ref.Namespace, ref.Name)
	if definition == nil || definition.GUID == "" {
		return "", false
	}
	iidVar := "IID_" + naming.Export(ref.Name)
	// Same PACKAGE, not merely same namespace: a cluster's members share one scope, so
	// an IID var declared by a sibling namespace is reachable unqualified.
	if g.mapper.SamePackage(fromNamespace, ref.Namespace) {
		return "&" + iidVar, true
	}
	// The alias has to come from the mapper, not from naming directly: an external
	// namespace takes the wrt prefix, and a bare ImportAlias here would name a
	// package the file did not import.
	return "&" + g.mapper.AliasFor(ref.Namespace) + "." + iidVar, true
}

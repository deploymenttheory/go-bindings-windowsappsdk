package typemap

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

const (
	metadataDir = "../../../metadata/wasdk"
	modulePath  = "github.com/deploymenttheory/go-bindings-windowsappsdk"
)

var cachedMapper *Mapper

// mapper resolves against the committed metadata and the pinned module. Real
// metadata rather than fixtures, because the questions here — does a Windows.*
// struct resolve to the right package, is Thickness passed by reference — are
// questions about that metadata.
func mapper(t *testing.T) *Mapper {
	t.Helper()
	if cachedMapper != nil {
		return cachedMapper
	}
	externalSet, err := external.Load("")
	if err != nil {
		t.Fatalf("loading the Windows.* universe: %v", err)
	}
	registry, err := pipeline.Load(metadataDir, externalSet)
	if err != nil {
		t.Fatalf("loading the committed metadata: %v", err)
	}
	clusters := pipeline.ComputeClusters(registry)
	cachedMapper = &Mapper{
		Registry:   registry,
		ModulePath: modulePath,
		Clusters:   clusters,
		Blocked:    pipeline.ComputeBlockedImports(registry, clusters),
	}
	return cachedMapper
}

func resolve(t *testing.T, ref wasdkmeta.TypeRef, namespace string) (Resolved, ImportSet) {
	t.Helper()
	imports := ImportSet{}
	return mapper(t).GoType(&ref, Context{Namespace: namespace}, imports), imports
}

func apiRef(namespace, name, kind string, isExternal bool) wasdkmeta.TypeRef {
	return wasdkmeta.TypeRef{
		Kind: "ApiRef", Namespace: namespace, Name: name,
		TargetKind: kind, External: isExternal,
	}
}

func TestNativeTypes(t *testing.T) {
	for name, want := range map[string]struct {
		goType string
		kind   Kind
	}{
		"Void":    {"", KindVoid},
		"Bool":    {"bool", KindBool},
		"Char16":  {"uint16", KindScalar},
		"I4":      {"int32", KindScalar},
		"U8":      {"uint64", KindScalar},
		"F64":     {"float64", KindFloat},
		"HString": {"string", KindString},
		"Guid":    {"win32.GUID", KindGUID},
		"Object":  {"*syswinrt.IInspectable", KindObjectPtr},
	} {
		got, _ := resolve(t, wasdkmeta.TypeRef{Kind: "Native", Name: name}, "Microsoft.UI.Xaml")
		if got.GoType != want.goType || got.Kind != want.kind {
			t.Errorf("Native %s = %q/%d, want %q/%d", name, got.GoType, got.Kind, want.goType, want.kind)
		}
	}
}

func TestUnknownNativeIsRefused(t *testing.T) {
	got, _ := resolve(t, wasdkmeta.TypeRef{Kind: "Native", Name: "Quux"}, "Microsoft.UI.Xaml")
	if got.Kind != KindUnsupported {
		t.Fatalf("an unknown native resolved to %q", got.GoType)
	}
	if !strings.HasPrefix(got.Reason, "unknown-native-type") {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestLocalReferenceIsUnqualifiedInItsOwnNamespace covers the commonest case: a
// type naming a sibling in the same package needs no qualifier, and adding one
// would not compile.
func TestLocalReferenceIsUnqualifiedInItsOwnNamespace(t *testing.T) {
	got, imports := resolve(t, apiRef("Microsoft.UI.Xaml", "Visibility", "Enum", false), "Microsoft.UI.Xaml")
	if got.GoType != "Visibility" {
		t.Errorf("GoType = %q, want the bare Visibility", got.GoType)
	}
	if len(imports) != 0 {
		t.Errorf("recorded %d imports for a same-package reference", len(imports))
	}
}

// TestLocalCrossPackageReference checks the alias and the import path together:
// getting either wrong produces code that does not compile, and getting the path
// subtly wrong produces code that compiles against the wrong package.
func TestLocalCrossPackageReference(t *testing.T) {
	got, imports := resolve(t,
		apiRef("Microsoft.UI.Dispatching", "DispatcherQueuePriority", "Enum", false),
		"Microsoft.UI.Xaml")
	if got.GoType != "uidispatching.DispatcherQueuePriority" {
		t.Errorf("GoType = %q, want uidispatching.DispatcherQueuePriority", got.GoType)
	}
	entry, ok := imports["uidispatching"]
	if !ok {
		t.Fatalf("no uidispatching import recorded; got %v", imports)
	}
	if want := modulePath + "/bindings/winui/ui/dispatching"; entry.Path != want {
		t.Errorf("import path = %q, want %q", entry.Path, want)
	}
	// Local namespaces feed the emit closure so the generated package exists.
	if entry.Namespace != "Microsoft.UI.Dispatching" {
		t.Errorf("import namespace = %q, want the namespace to be chased", entry.Namespace)
	}
}

// TestSameClusterReferencesAreUnqualified is the behaviour clustering exists to
// produce: two mutually recursive namespaces share one Go package, so a reference
// between them needs neither a qualifier nor an import.
//
// This is what makes UIElement's input events expressible. They are declared in
// Microsoft.UI.Xaml and take argument types from Microsoft.UI.Xaml.Input; when those
// were separate packages the reference crossed a severed edge and every one of those
// events was dropped.
func TestSameClusterReferencesAreUnqualified(t *testing.T) {
	for _, check := range []struct{ from, namespace, name, kind string }{
		{"Microsoft.UI.Xaml", "Microsoft.UI.Xaml.Input", "IPointerRoutedEventArgs", "Interface"},
		{"Microsoft.UI.Xaml.Controls", "Microsoft.UI.Xaml.Controls.Primitives", "IButtonBase", "Interface"},
		{"Microsoft.UI.Xaml.Media", "Microsoft.UI.Xaml.Controls", "Symbol", "Enum"},
	} {
		got, imports := resolve(t, apiRef(check.namespace, check.name, check.kind, false), check.from)
		if got.Kind == KindUnsupported {
			t.Errorf("%s -> %s.%s degraded: %s", check.from, check.namespace, check.name, got.Reason)
			continue
		}
		if strings.Contains(got.GoType, ".") {
			t.Errorf("%s -> %s.%s resolved to %q; a same-package reference must not be qualified",
				check.from, check.namespace, check.name, got.GoType)
		}
		if len(imports) != 0 {
			t.Errorf("%s -> %s.%s recorded imports %v; it is in the same package",
				check.from, check.namespace, check.name, imports)
		}
	}
}

// TestExternalReferenceResolvesIntoTheOtherModule is the whole point of loading
// the external universe: a Windows.* type must import go-bindings-winrt's
// package, with the prefixed alias, and must NOT be added to this module's emit
// closure — that would ask it to generate a package which already exists.
func TestExternalReferenceResolvesIntoTheOtherModule(t *testing.T) {
	got, imports := resolve(t,
		apiRef("Windows.Foundation", "Rect", "Struct", true),
		"Microsoft.UI.Xaml")
	if got.Kind != KindStruct {
		t.Fatalf("Rect resolved as kind %d (%s)", got.Kind, got.Reason)
	}
	if got.GoType != "wrtfoundation.Rect" {
		t.Errorf("GoType = %q, want wrtfoundation.Rect", got.GoType)
	}
	entry, ok := imports["wrtfoundation"]
	if !ok {
		t.Fatalf("no wrtfoundation import recorded; got %v", imports)
	}
	if want := external.BindingsImportRoot + "/foundation"; entry.Path != want {
		t.Errorf("import path = %q, want %q", entry.Path, want)
	}
	if entry.Namespace != "" {
		t.Errorf("import namespace = %q, but an external namespace must not be chased for emission", entry.Namespace)
	}
}

// TestTheAliasCollisionIsAvoidedInPractice resolves both halves of the actual
// collision into one import set — which is what an emitted file does.
//
// Microsoft.UI.Xaml.Interop and Windows.UI.Xaml.Interop both strip to
// "uixamlinterop". If the prefix were not applied, the second write would replace
// the first and the file would reference a package it had not imported.
func TestTheAliasCollisionIsAvoidedInPractice(t *testing.T) {
	imports := ImportSet{}
	ctx := Context{Namespace: "Microsoft.UI.Xaml.Controls"}

	local := apiRef("Microsoft.UI.Xaml.Interop", "TypeName", "Struct", false)
	foreign := apiRef("Windows.UI.Xaml.Interop", "TypeName", "Struct", true)
	localResolved := mapper(t).GoType(&local, ctx, imports)
	foreignResolved := mapper(t).GoType(&foreign, ctx, imports)

	if localResolved.Kind == KindUnsupported || foreignResolved.Kind == KindUnsupported {
		t.Skipf("one side is not emittable here (%s / %s); the collision test needs both",
			localResolved.Reason, foreignResolved.Reason)
	}
	if localResolved.GoType == foreignResolved.GoType {
		t.Fatalf("both resolved to %q — the collision is not avoided", localResolved.GoType)
	}
	if len(imports) != 2 {
		t.Fatalf("%d imports for two distinct packages: %v", len(imports), imports)
	}
	paths := map[string]bool{}
	for alias, entry := range imports {
		if paths[entry.Path] {
			t.Errorf("path %s imported twice", entry.Path)
		}
		paths[entry.Path] = true
		if !strings.HasSuffix(entry.Path, "/ui/xaml/interop") {
			t.Errorf("alias %q → %q, which is not a UI.Xaml.Interop package", alias, entry.Path)
		}
	}
}

// TestClassReferenceLowersToItsDefaultInterface pins the rule that makes class
// parameters work at all: a runtime class has no vtable of its own, so a class in
// a signature is its default interface pointer at the ABI.
func TestClassReferenceLowersToItsDefaultInterface(t *testing.T) {
	got, _ := resolve(t,
		apiRef("Microsoft.UI.Xaml.Controls", "Button", "Class", false),
		"Microsoft.UI.Xaml.Controls")
	if got.Kind != KindInterfacePtr {
		t.Fatalf("Button resolved as kind %d (%s), want an interface pointer", got.Kind, got.Reason)
	}
	if got.GoType != "*IButton" {
		t.Errorf("GoType = %q, want *IButton", got.GoType)
	}
}

// TestExternalClassReferenceLowersInTheOtherModule is the case that needs care:
// the external class's default interface is recorded in go-bindings-winrt's
// metadata, which carries no External flag of its own, so it has to be propagated
// or the interface would be looked for locally.
func TestExternalClassReferenceLowersInTheOtherModule(t *testing.T) {
	got, imports := resolve(t,
		apiRef("Windows.Foundation", "Uri", "Class", true),
		"Microsoft.UI.Xaml")
	if got.Kind != KindInterfacePtr {
		t.Fatalf("Uri resolved as kind %d (%s), want an interface pointer", got.Kind, got.Reason)
	}
	if !strings.HasPrefix(got.GoType, "*wrtfoundation.") {
		t.Errorf("GoType = %q, want a *wrtfoundation.I... pointer", got.GoType)
	}
	if entry, ok := imports["wrtfoundation"]; !ok {
		t.Error("no wrtfoundation import recorded")
	} else if entry.Namespace != "" {
		t.Errorf("external namespace %q was added to the emit closure", entry.Namespace)
	}
}

// TestGenericInterfaceHasNoGoType covers what neither module emits: an open
// generic interface. Only closed instantiations become Go types.
func TestGenericInterfaceHasNoGoType(t *testing.T) {
	got, _ := resolve(t,
		apiRef("Windows.Foundation.Collections", "IVector`1", "Interface", true),
		"Microsoft.UI.Xaml")
	if got.Kind != KindUnsupported {
		t.Fatalf("an open generic interface resolved to %q", got.GoType)
	}
	if !strings.HasPrefix(got.Reason, "generic-member-skipped") {
		t.Errorf("reason = %q", got.Reason)
	}
}

// TestForeignNamespaceIsSkippedDistinctly is the WebView2 rule. It must not
// resolve, and it must not look like an unresolved reference either: one is a
// permanent absence and the other is a broken pin, and confusing them would send
// someone looking for a winmd that does not exist.
func TestForeignNamespaceIsSkippedDistinctly(t *testing.T) {
	got, imports := resolve(t,
		apiRef("Microsoft.Web.WebView2.Core", "CoreWebView2", "", false),
		"Microsoft.UI.Xaml.Controls")
	if got.Kind != KindUnsupported {
		t.Fatalf("a WebView2 type resolved to %q — it has no Go equivalent", got.GoType)
	}
	if !strings.HasPrefix(got.Reason, "foreign-type-skipped") {
		t.Errorf("reason = %q, want foreign-type-skipped", got.Reason)
	}
	if got.GoType == "uintptr" {
		t.Error("degraded to uintptr; a binding that compiles and then crashes is worse than one that is absent")
	}
	if len(imports) != 0 {
		t.Errorf("recorded imports for a type with no package: %v", imports)
	}
}

// TestBlockedEdgeDegrades checks the import-cycle break actually takes effect.
// Go rejects an import cycle outright, so a reference along a severed edge has to
// degrade rather than record the import.
func TestBlockedEdgeDegrades(t *testing.T) {
	m := mapper(t)
	if len(m.Blocked) == 0 {
		t.Skip("no cycles were severed in this metadata")
	}
	var src, dst string
	for from := range m.Blocked {
		for to := range m.Blocked[from] {
			src, dst = from, to
			break
		}
		if src != "" {
			break
		}
	}
	// Any type in dst will do; pick one the registry actually holds.
	meta := m.Registry.ByNamespace[dst]
	if meta == nil {
		t.Fatalf("severed target %s is not in the registry", dst)
	}
	var name string
	for candidate := range meta.Enums {
		name = candidate
		break
	}
	if name == "" {
		t.Skipf("%s has no enum to test with", dst)
	}
	imports := ImportSet{}
	ref := apiRef(dst, name, "Enum", false)
	got := m.GoType(&ref, Context{Namespace: src}, imports)
	if got.Kind != KindUnsupported {
		t.Fatalf("%s → %s.%s resolved to %q despite the severed edge", src, dst, name, got.GoType)
	}
	if !strings.HasPrefix(got.Reason, "import-cycle-skipped") {
		t.Errorf("reason = %q, want import-cycle-skipped", got.Reason)
	}
	if len(imports) != 0 {
		t.Errorf("recorded an import across a severed edge: %v", imports)
	}
}

// TestEventRegistrationTokenComesFromTheSharedFoundation is an identity check
// rather than a naming one. Every add_/remove_ pair exchanges this struct, so a
// second definition would make this module's events incompatible with
// go-bindings-winrt's — and it would compile.
func TestEventRegistrationTokenComesFromTheSharedFoundation(t *testing.T) {
	got, imports := resolve(t,
		apiRef("Windows.Foundation", "EventRegistrationToken", "Struct", true),
		"Microsoft.UI.Xaml")
	if got.GoType != "syswinrt.EventRegistrationToken" {
		t.Errorf("GoType = %q, want syswinrt.EventRegistrationToken from the shared ABI foundation", got.GoType)
	}
	if entry, ok := imports["syswinrt"]; !ok || entry.Path != SysWinRTImport {
		t.Errorf("syswinrt import = %v, want %s", imports["syswinrt"], SysWinRTImport)
	}
	if !IsExternalType("Windows.Foundation", "EventRegistrationToken") {
		t.Error("IsExternalType does not recognize it")
	}
	// HResult flattens to a plain scalar.
	hr, _ := resolve(t, apiRef("Windows.Foundation", "HResult", "Struct", true), "Microsoft.UI.Xaml")
	if hr.GoType != "int32" {
		t.Errorf("HResult = %q, want int32", hr.GoType)
	}
}

// TestGenericParametersDegrade covers the IR kind that has no Go form at all.
func TestGenericParametersDegrade(t *testing.T) {
	param, _ := resolve(t, wasdkmeta.TypeRef{Kind: "GenericParamRef", Index: 0}, "Microsoft.UI.Xaml")
	if param.Kind != KindUnsupported || !strings.HasPrefix(param.Reason, "generic-member-skipped") {
		t.Errorf("generic parameter resolved to %q / %q", param.GoType, param.Reason)
	}
}

// arrayOf builds a conformant-array TypeRef over the given element.
func arrayOf(elem wasdkmeta.TypeRef) wasdkmeta.TypeRef {
	return wasdkmeta.TypeRef{Kind: "Array", Elem: &elem}
}

// TestArraysResolveToSlices covers the element kinds whose Go representation is
// byte-identical to the ABI form, which is what makes a direct slice view over the
// callee's buffer sound. Anything not on this list has to be refused, not copied.
func TestArraysResolveToSlices(t *testing.T) {
	for _, check := range []struct {
		what string
		ref  wasdkmeta.TypeRef
		want string
	}{
		{"scalar", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "I4"}), "[]int32"},
		{"byte", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "U1"}), "[]byte"},
		{"double", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "F64"}), "[]float64"},
		{"GUID", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "Guid"}), "[]win32.GUID"},
		{"Object", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "Object"}), "[]*syswinrt.IInspectable"},
		{
			"an emittable struct",
			arrayOf(apiRef("Microsoft.UI.Xaml", "Thickness", "Struct", false)),
			"[]Thickness",
		},
		{
			"an external struct",
			arrayOf(apiRef("Windows.Foundation", "Point", "Struct", true)),
			"[]wrtfoundation.Point",
		},
		{
			"an interface pointer",
			arrayOf(apiRef("Microsoft.UI.Xaml", "IDependencyObject", "Interface", false)),
			"[]*IDependencyObject",
		},
	} {
		got, _ := resolve(t, check.ref, "Microsoft.UI.Xaml")
		if got.Kind != KindArray {
			t.Errorf("%s array resolved as kind %d (%s)", check.what, got.Kind, got.Reason)
			continue
		}
		if got.GoType != check.want {
			t.Errorf("%s array = %q, want %q", check.what, got.GoType, check.want)
		}
		// The lowering branches on the element, so it has to be carried through.
		if got.Elem == nil {
			t.Errorf("%s array carries no resolved element", check.what)
		}
	}
}

// TestArrayElementsNeedingConversionAreRefused is the safety half. An HSTRING is a
// handle and a WinRT boolean is one byte with no guarantee it is 0 or 1, so a direct
// slice view over either would reinterpret the bytes as something they are not — and
// it would compile and run. Refusing is the only correct answer until per-element
// conversion exists.
func TestArrayElementsNeedingConversionAreRefused(t *testing.T) {
	for _, check := range []struct {
		what string
		ref  wasdkmeta.TypeRef
	}{
		{"HSTRING", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "HString"})},
		{"Bool", arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "Bool"})},
		{"a nested array", arrayOf(arrayOf(wasdkmeta.TypeRef{Kind: "Native", Name: "I4"}))},
		{"an unresolved element", arrayOf(apiRef("Microsoft.Nope", "IGone", "Interface", false))},
	} {
		got, imports := resolve(t, check.ref, "Microsoft.UI.Xaml")
		if got.Kind != KindUnsupported {
			t.Errorf("an array of %s resolved to %q", check.what, got.GoType)
			continue
		}
		if !strings.HasPrefix(got.Reason, "array-element-skipped") {
			t.Errorf("array of %s: reason = %q, want array-element-skipped", check.what, got.Reason)
		}
		// A refused element must leave no import behind: the member is skipped, so an
		// import recorded for it would be unused and would not compile.
		if len(imports) != 0 {
			t.Errorf("array of %s recorded imports despite being refused: %v", check.what, imports)
		}
	}
	// An array with no element at all is malformed metadata, not a conversion gap.
	got, _ := resolve(t, wasdkmeta.TypeRef{Kind: "Array"}, "Microsoft.UI.Xaml")
	if got.Kind != KindUnsupported {
		t.Errorf("an elementless array resolved to %q", got.GoType)
	}
}

// TestInstantiationSeamIsUsedWhenWired proves the callback path: with the seam
// wired a closed generic instantiation becomes a package-local type and records
// no import, because the type is emitted into the consuming package.
func TestInstantiationSeamIsUsedWhenWired(t *testing.T) {
	ref := wasdkmeta.TypeRef{
		Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IVector`1",
		TargetKind: "Interface", External: true,
		Args: []wasdkmeta.TypeRef{{Kind: "Native", Name: "HString"}},
	}
	imports := ImportSet{}
	var asked *wasdkmeta.TypeRef
	ctx := Context{
		Namespace: "Microsoft.UI.Xaml",
		RequestInstantiation: func(r *wasdkmeta.TypeRef) (string, bool) {
			asked = r
			return "IVectorOfString", true
		},
	}
	got := mapper(t).GoType(&ref, ctx, imports)
	if got.Kind != KindInterfacePtr || got.GoType != "*IVectorOfString" {
		t.Fatalf("GoType = %q kind %d, want *IVectorOfString", got.GoType, got.Kind)
	}
	if asked == nil {
		t.Error("the instantiation seam was never called")
	}
	if len(imports) != 0 {
		t.Errorf("a monomorphized type is package-local, but imports were recorded: %v", imports)
	}
}

// TestInstantiationSeamRefusalDegrades: a seam that cannot ground the type must
// leave the member degraded rather than emit a name for something absent.
func TestInstantiationSeamRefusalDegrades(t *testing.T) {
	ref := wasdkmeta.TypeRef{
		Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IVector`1",
		TargetKind: "Interface", External: true,
	}
	ctx := Context{
		Namespace:            "Microsoft.UI.Xaml",
		RequestInstantiation: func(*wasdkmeta.TypeRef) (string, bool) { return "", false },
	}
	got := mapper(t).GoType(&ref, ctx, ImportSet{})
	if got.Kind != KindUnsupported {
		t.Fatalf("a refused instantiation resolved to %q", got.GoType)
	}
}

// TestImportPathsForBothModules pins the two roots side by side, since one wrong
// path means code compiled against the wrong package.
func TestImportPathsForBothModules(t *testing.T) {
	m := mapper(t)
	// Microsoft.UI.Xaml.Controls is part of the XAML cluster, so its types live in the
	// cluster's package — not in a controls package of their own.
	if got, want := m.ImportPathFor("Microsoft.UI.Xaml.Controls"),
		modulePath+"/bindings/winui/ui/xaml"; got != want {
		t.Errorf("clustered path = %q, want %q", got, want)
	}
	// An acyclic namespace keeps its own package.
	if got, want := m.ImportPathFor("Microsoft.UI.Dispatching"),
		modulePath+"/bindings/winui/ui/dispatching"; got != want {
		t.Errorf("independent path = %q, want %q", got, want)
	}
	if got, want := m.ImportPathFor("Windows.Foundation.Collections"),
		external.BindingsImportRoot+"/foundation/collections"; got != want {
		t.Errorf("external path = %q, want %q", got, want)
	}
	if got := m.AliasFor("Microsoft.UI.Xaml.Controls"); got != "uixaml" {
		t.Errorf("clustered alias = %q, want uixaml", got)
	}
	if got := m.AliasFor("Microsoft.UI.Dispatching"); got != "uidispatching" {
		t.Errorf("independent alias = %q", got)
	}
	if got := m.AliasFor("Windows.Foundation.Collections"); got != "wrtfoundationcollections" {
		t.Errorf("external alias = %q", got)
	}
	// The runtime is go-bindings-winrt's, not a reimplementation.
	if got := m.RuntimeImportPath(); got != WinRTRuntimeImport {
		t.Errorf("runtime import = %q, want %q", got, WinRTRuntimeImport)
	}
	if !strings.HasPrefix(m.RuntimeImportPath(), external.ModulePath) {
		t.Error("the runtime import does not point at go-bindings-winrt")
	}
}

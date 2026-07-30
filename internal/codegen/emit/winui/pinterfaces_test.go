package emitwinui

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// asyncOperationOfBool builds Windows.Foundation.IAsyncOperation`1<Bool> as this
// module's metadata records it: External, because the definition lives in
// go-bindings-winrt.
func asyncOperationOfBool(external bool) wasdkmeta.TypeRef {
	return wasdkmeta.TypeRef{
		Kind:       "GenericInst",
		Name:       "IAsyncOperation`1",
		Namespace:  "Windows.Foundation",
		TargetKind: "Interface",
		External:   external,
		Args: []wasdkmeta.TypeRef{
			{Kind: "Native", Name: "Bool"},
		},
	}
}

// TestSameTypeIgnoresWhereTheDefinitionWasFound is the regression test for the bug
// this file's dedupe guard had.
//
// The guard compared references with reflect.DeepEqual, which includes the External
// flag — and that flag records WHOSE metadata the reference was read from, not which
// type it is. The same IAsyncOperation`1<Bool> arrives External:true from this
// module's JSON and External:false when it surfaces through go-bindings-winrt's own
// IR for AsyncOperationCompletedHandler`1.Invoke, because inside that module
// Windows.Foundation is local. So the guard rejected 72 instantiations as name
// collisions when they were the same type, and every member naming one degraded.
func TestSameTypeIgnoresWhereTheDefinitionWasFound(t *testing.T) {
	fromThisModule := asyncOperationOfBool(true)
	throughWinRTsOwnIR := asyncOperationOfBool(false)

	if !sameType(&fromThisModule, &throughWinRTsOwnIR) {
		t.Error("the same instantiation read from two modules' metadata is being treated as " +
			"two different types; every member naming it will degrade")
	}
	// And TargetKind, for the same reason: for a fixed namespace and name it cannot
	// disagree, so a difference in it is provenance too.
	noTargetKind := asyncOperationOfBool(true)
	noTargetKind.TargetKind = ""
	if !sameType(&fromThisModule, &noTargetKind) {
		t.Error("an un-annotated TargetKind is being treated as a different type")
	}
}

// TestSameTypeStillCatchesRealCollisions is the other half. The guard exists for a
// reason: mangled names drop namespaces, so two instantiations with same-named
// arguments from different namespaces collide on one Go type name. Letting that
// through would alias two distinct IIDs onto one type — which compiles, and then
// fails at QueryInterface.
func TestSameTypeStillCatchesRealCollisions(t *testing.T) {
	vectorOf := func(namespace, name string) wasdkmeta.TypeRef {
		return wasdkmeta.TypeRef{
			Kind: "GenericInst", Name: "IVector`1",
			Namespace: "Windows.Foundation.Collections", TargetKind: "Interface",
			Args: []wasdkmeta.TypeRef{
				{Kind: "ApiRef", Name: name, Namespace: namespace, TargetKind: "Class"},
			},
		}
	}
	// Both mangle to IVectorOfTextRange, and they are not the same type.
	a := vectorOf("Microsoft.UI.Xaml.Documents", "TextRange")
	b := vectorOf("Microsoft.UI.Text", "TextRange")
	if nameA, _ := instantiationName(&a); nameA != "IVectorOfTextRange" {
		t.Fatalf("mangled to %s, want IVectorOfTextRange — the premise of this test is gone", nameA)
	}
	if sameType(&a, &b) {
		t.Error("two instantiations over different types are being treated as the same; " +
			"they would alias two IIDs onto one Go type")
	}

	// Different arity, and a different open type, must also differ.
	if sameType(&a, &wasdkmeta.TypeRef{Kind: "GenericInst", Name: "IVector`1",
		Namespace: "Windows.Foundation.Collections", TargetKind: "Interface"}) {
		t.Error("an instantiation and an argument-less reference to the same open type compare equal")
	}

	// And the recursion: a nested argument that differs must be caught.
	outerA := wasdkmeta.TypeRef{Kind: "GenericInst", Name: "IVector`1",
		Namespace: "Windows.Foundation.Collections", Args: []wasdkmeta.TypeRef{a}}
	outerB := wasdkmeta.TypeRef{Kind: "GenericInst", Name: "IVector`1",
		Namespace: "Windows.Foundation.Collections", Args: []wasdkmeta.TypeRef{b}}
	if sameType(&outerA, &outerB) {
		t.Error("sameType does not recurse into nested generic arguments")
	}
}

// TestCollectionClassesProjectTheirInstantiation is the user-visible payoff of
// resolving a generic default interface.
//
// Thirty XAML classes have a generic instantiation as their ONLY interface —
// UIElementCollection's is IVector`1<UIElement> — and refusing them cost far more
// than the classes themselves: Panel.Children, Grid.RowDefinitions,
// TextBlock.Inlines and ItemsControl.Items all return one, so each degraded to
// IInspectable. Silently, because a property whose type is an un-emitted class is
// not itself a diagnostic.
func TestCollectionClassesProjectTheirInstantiation(t *testing.T) {
	classes := source(t, "ui/xaml/xaml_classes.go")
	if !strings.Contains(classes, "type UIElementCollection struct {\n\tIVectorOfUIElement\n}") {
		t.Error("UIElementCollection does not embed the monomorphized IVector<UIElement>")
	}

	interfaces := source(t, "ui/xaml/xaml_interfaces.go")
	// A class reference in a signature is its default interface at the ABI, so the
	// property's Go type is the instantiation rather than the class.
	for _, want := range []string{
		"func (self *IPanel) Children() (*IVectorOfUIElement, error)",
		"func (self *IGrid) RowDefinitions() (*IVectorOfRowDefinition, error)",
	} {
		if !strings.Contains(interfaces, want) {
			t.Errorf("%s is not emitted; the collection property degraded", want)
		}
	}

	// Append is what the property is FOR, and it comes from the instantiation's own
	// monomorphized methods.
	pinterfaces := source(t, "ui/xaml/xaml_pinterfaces.go")
	if !strings.Contains(pinterfaces, "func (self *IVectorOfUIElement) Append(value *IUIElement) error") {
		t.Error("IVectorOfUIElement has no typed Append")
	}
}

// TestNestedInstantiationsAreGrounded covers the transitive closure. Substituting
// arguments into an open interface surfaces further instantiations —
// IVector<T>.GetView yields IVectorView<T> — and those have to be queued and emitted
// into the same package, not degraded.
func TestNestedInstantiationsAreGrounded(t *testing.T) {
	pinterfaces := source(t, "ui/xaml/xaml_pinterfaces.go")
	for _, want := range []string{
		"func (self *IVectorOfUIElement) GetView() (*IVectorViewOfUIElement, error)",
		"type IVectorViewOfUIElement struct",
		"var IID_IVectorViewOfUIElement = ",
	} {
		if !strings.Contains(pinterfaces, want) {
			t.Errorf("%q missing: the nested instantiation was not grounded", want)
		}
	}
}

// TestAsyncOperationsAreUsable states what the async surface needs to be worth
// having. A completion handler must be settable and the result gettable; the
// Completed GETTER is deliberately absent, because handing a native delegate back to
// Go has no useful meaning — there is no Go callback behind it.
func TestAsyncOperationsAreUsable(t *testing.T) {
	pinterfaces := source(t, "ui/xaml/xaml_pinterfaces.go")
	if !strings.Contains(pinterfaces, "func (self *IAsyncOperationOfBool) SetCompleted(handler *AsyncOperationCompletedHandlerOfBool) error") {
		t.Error("IAsyncOperationOfBool.SetCompleted is not emitted with a typed handler")
	}
	if !strings.Contains(pinterfaces, "func (self *IAsyncOperationOfBool) GetResults() (bool, error)") {
		t.Error("IAsyncOperationOfBool.GetResults is not emitted")
	}
	if strings.Contains(pinterfaces, "func (self *IAsyncOperationOfBool) Completed()") {
		t.Error("the Completed getter is emitted; returning a native delegate to Go has no " +
			"callback behind it, so this should degrade")
	}
	// And it degrades under the key that says why, not under a generics key.
	result := emit(t)
	var returns, generics int
	for _, diagnostic := range result.generator.Diagnostics {
		switch {
		case strings.HasPrefix(diagnostic, "delegate-return-skipped:"):
			returns++
		case strings.HasPrefix(diagnostic, "generic-member-skipped:"):
			generics++
		}
	}
	if returns == 0 {
		t.Error("no delegate-return-skipped diagnostics; returned delegates are being " +
			"reported as something else")
	}
	// The attribution is the point: before this key existed, 48 returned-delegate
	// members were counted as a generics limitation, which is not what they are.
	if generics > returns {
		t.Errorf("%d generic-member-skipped vs %d delegate-return-skipped: returned "+
			"delegates look like they are still being misattributed", generics, returns)
	}
}

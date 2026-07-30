package emitwinui

import (
	"strings"
	"testing"
)

// TestFloatOutParametersLower is the plainest kind of bug: a missing case.
//
// An [out] parameter is a POINTER the callee writes through, so a float32 crosses
// through memory and never through XMM0 — identical to an [out] int32. KindFloat was
// simply absent from the admitted kinds, which is why
// ICompositionPropertySet.TryGetScalar degraded while every TryGetVector3 beside it
// (a struct of floats) worked. That neighbour is what makes the omission legible, so
// both are asserted together.
func TestFloatOutParametersLower(t *testing.T) {
	source := source(t, "ui/composition/composition_interfaces.go")
	for _, want := range []string{
		"func (self *ICompositionPropertySet) TryGetScalar(propertyName string, value *float32) (CompositionGetValueStatus, error)",
		"func (self *ICompositionPropertySet) TryGetVector3(propertyName string, value *wrtfoundationnumerics.Vector3) (CompositionGetValueStatus, error)",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("%s is not emitted", want)
		}
	}
}

// TestStringOutParametersConvertOnSuccessOnly covers both halves of the postamble:
// that the conversion happens at all, and that it is inside the success path.
//
// An HSTRING out-parameter transfers ownership, so the caller must delete the handle;
// winrt.TakeHString is what does that, and it is the same conversion an HSTRING RETURN
// already uses. Exposing the raw handle instead would compile and leak.
//
// The ordering matters independently. On a failed call the callee wrote nothing, so
// converting the slot would overwrite the caller's variable with the empty string —
// reporting an error AND destroying the value the caller passed in.
func TestStringOutParametersConvertOnSuccessOnly(t *testing.T) {
	source := source(t, "ui/text/text_interfaces.go")
	if !strings.Contains(source, "func (self *ITextRange) GetText(options TextGetOptions, value *string) error") {
		t.Fatal("ITextRange.GetText does not take a *string out-parameter")
	}
	body := methodBody(source, "func (self *ITextRange) GetText(")
	if body == "" {
		t.Fatal("could not isolate the GetText body")
	}
	if !strings.Contains(body, "_valueRaw := new(syswinrt.HSTRING)") {
		t.Error("no raw HSTRING slot is declared for the out-parameter")
	}
	if !strings.Contains(body, "*value = winrt.TakeHString(*_valueRaw)") {
		t.Error("the HSTRING is not converted and taken; the caller would leak the handle")
	}
	// The error check has to come first. Comparing positions rather than matching a
	// blob, so reformatting the body does not silently stop checking the order.
	check := strings.Index(body, "if err := win32.ErrIfFailed")
	convert := strings.Index(body, "*value = winrt.TakeHString")
	if check < 0 || convert < 0 || check > convert {
		t.Error("the conversion is not inside the success path: a failed call would " +
			"overwrite the caller's variable from a slot the callee never wrote")
	}
}

// TestBoolOutParametersGoThroughAByte pins why a *bool cannot be handed to the callee
// directly: a WinRT boolean is one byte and nothing guarantees it is 0 or 1, so a
// stray value written into a Go bool is neither true nor false in comparisons.
func TestBoolOutParametersGoThroughAByte(t *testing.T) {
	source := source(t, "ui/composition/composition_interfaces.go")
	body := methodBody(source, "func (self *ICompositionPropertySet2) TryGetBoolean(")
	if body == "" {
		t.Fatal("ICompositionPropertySet2.TryGetBoolean is not emitted")
	}
	if !strings.Contains(body, "value *bool") && !strings.Contains(source, "value *bool") {
		t.Error("the out-parameter is not exposed as *bool")
	}
	if !strings.Contains(body, "_valueRaw := new(byte)") {
		t.Error("the ABI slot is not a byte")
	}
	if !strings.Contains(body, "*value = *_valueRaw != 0") {
		t.Error("the byte is not normalized into a Go bool")
	}
}

// TestStructsCarryHstringFieldsAsHandles is the struct half of the same question, and
// it lands differently on purpose.
//
// A parameter converts because there is a call boundary to convert at. A struct field
// has none — the struct crosses as a block of bytes — so the field IS the handle, and
// the doc comment has to say what that obliges the caller to do. A syswinrt.HSTRING
// field with no explanation reads like a string-shaped thing and leaks.
func TestStructsCarryHstringFieldsAsHandles(t *testing.T) {
	source := source(t, "ui/xaml/markup/markup_structs.go")
	if !strings.Contains(source, "type XmlnsDefinition struct {\n\tXmlNamespace syswinrt.HSTRING\n\tNamespace    syswinrt.HSTRING\n}") {
		t.Error("XmlnsDefinition's string fields are not emitted as HSTRING handles")
	}
	for _, want := range []string{
		"are HSTRING handles, not Go strings",
		"winrt.NewHString",
		"winrt.TakeHString",
	} {
		if !strings.Contains(source, want) {
			t.Errorf("the doc comment does not mention %q, so the ownership rule is unstated", want)
		}
	}
}

// TestExternalTypesAreOnlyNamedIfTheDependencyDeclaresThem is the guard that this
// phase turned out to need, and it is the one worth keeping longest.
//
// This module resolved a Windows.* reference on the strength of the type appearing in
// go-bindings-winrt's committed IR. That is the wrong evidence: the IR says what the
// type is, and only the emitted source says whether there is a Go declaration to name.
// Windows.UI.Xaml.Interop.TypeName is { HSTRING; TypeKind } and that module skips it,
// so the moment this one stopped refusing HSTRING struct fields, 31 members began
// naming an undeclared type and the whole tree stopped compiling.
//
// A degraded member costs a binding. A reference to an undeclared name costs every
// consumer their build, so the check is deliberately conservative.
func TestExternalTypesAreOnlyNamedIfTheDependencyDeclaresThem(t *testing.T) {
	result := emit(t)
	mapper := result.generator.Mapper()

	// TypeKind, an enum in the same namespace, IS declared. Asserting the pair rather
	// than the failure alone: a check that answered false for everything would pass a
	// one-sided test while degrading the entire external surface.
	if !mapper.ExternalDeclares("Windows.UI.Xaml.Interop", "TypeKind") {
		t.Error("Windows.UI.Xaml.Interop.TypeKind is reported as undeclared, but " +
			"go-bindings-winrt emits it; the check is refusing types that exist")
	}
	if mapper.ExternalDeclares("Windows.UI.Xaml.Interop", "TypeName") {
		t.Error("Windows.UI.Xaml.Interop.TypeName is reported as declared, but " +
			"go-bindings-winrt skips it — naming it does not compile")
	}
	if mapper.StructEmittable("Windows.UI.Xaml.Interop", "TypeName") {
		t.Error("TypeName is treated as representable; every member using it will name " +
			"an undeclared type")
	}

	// And nothing in the output names it. This is the assertion that would have caught
	// the regression directly.
	for path, content := range result.files {
		if strings.Contains(content, "wrtuixamlinterop.TypeName") {
			t.Errorf("%s names wrtuixamlinterop.TypeName, which is not declared", path)
		}
	}
}

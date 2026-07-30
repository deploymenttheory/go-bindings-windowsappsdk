package typemap

import (
	"strings"
	"testing"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
)

// The layout model is computed rather than measured, because the answer is needed
// at generate time. These tests are what make that sound: the sizes it derives
// are checked against unsafe.Sizeof of the equivalent Go types, and against the
// real metadata for the structs XAML actually passes.

func TestClassifyAggregate(t *testing.T) {
	for size, want := range map[int]ParamClass{
		1: ParamInline, 2: ParamInline, 4: ParamInline, 8: ParamInline,
		// Anything else travels as a pointer to a caller-owned temporary.
		3: ParamByRef, 5: ParamByRef, 12: ParamByRef, 16: ParamByRef,
		24: ParamByRef, 32: ParamByRef, 64: ParamByRef,
	} {
		if got := ClassifyAggregate(size); got != want {
			t.Errorf("ClassifyAggregate(%d) = %d, want %d", size, got, want)
		}
	}
}

// TestXamlStructLayouts is the load-bearing case. Thickness is on nearly every
// element as Margin or Padding, and misclassifying it would corrupt the argument
// on every one of those calls.
func TestXamlStructLayouts(t *testing.T) {
	m := mapper(t)
	for _, want := range []struct {
		namespace, name string
		size, align     int
		class           ParamClass
	}{
		// Four doubles: 32 bytes, so by reference.
		{"Microsoft.UI.Xaml", "Thickness", 32, 8, ParamByRef},
		{"Microsoft.UI.Xaml", "CornerRadius", 32, 8, ParamByRef},
		// A double plus an Int32-backed enum, padded to the double's alignment.
		{"Microsoft.UI.Xaml", "GridLength", 16, 8, ParamByRef},
		// Two floats: 8 bytes, so it travels inline in a register.
		{"Windows.Foundation", "Point", 8, 4, ParamInline},
		// Four floats.
		{"Windows.Foundation", "Rect", 16, 4, ParamByRef},
		{"Windows.Foundation", "Size", 8, 4, ParamInline},
	} {
		layout, ok := m.StructLayout(want.namespace, want.name)
		if !ok {
			t.Errorf("%s.%s has no computable layout", want.namespace, want.name)
			continue
		}
		if layout.Size != want.size || layout.Align != want.align {
			t.Errorf("%s.%s = %d/%d (size/align), want %d/%d",
				want.namespace, want.name, layout.Size, layout.Align, want.size, want.align)
		}
		if got := ClassifyAggregate(layout.Size); got != want.class {
			t.Errorf("%s.%s is passed as %d, want %d", want.namespace, want.name, got, want.class)
		}
	}
}

// TestComputedSizesMatchGo is the premise the whole model rests on: that Go lays
// these out the way C does, so a size computed from the metadata equals
// unsafe.Sizeof of the type that gets emitted. Assert it against types declared
// here exactly as the emitter would declare them.
func TestComputedSizesMatchGo(t *testing.T) {
	type thickness struct{ Left, Top, Right, Bottom float64 }
	type point struct{ X, Y float32 }
	type rect struct{ X, Y, Width, Height float32 }
	type gridLength struct {
		Value float64
		Type  int32
	}

	m := mapper(t)
	for _, check := range []struct {
		namespace, name string
		goSize, goAlign uintptr
	}{
		{"Microsoft.UI.Xaml", "Thickness", unsafe.Sizeof(thickness{}), unsafe.Alignof(thickness{})},
		{"Microsoft.UI.Xaml", "GridLength", unsafe.Sizeof(gridLength{}), unsafe.Alignof(gridLength{})},
		{"Windows.Foundation", "Point", unsafe.Sizeof(point{}), unsafe.Alignof(point{})},
		{"Windows.Foundation", "Rect", unsafe.Sizeof(rect{}), unsafe.Alignof(rect{})},
	} {
		layout, ok := m.StructLayout(check.namespace, check.name)
		if !ok {
			t.Errorf("%s.%s has no computable layout", check.namespace, check.name)
			continue
		}
		if uintptr(layout.Size) != check.goSize {
			t.Errorf("%s.%s computed size %d, but Go lays the same fields out in %d",
				check.namespace, check.name, layout.Size, check.goSize)
		}
		if uintptr(layout.Align) != check.goAlign {
			t.Errorf("%s.%s computed align %d, but Go uses %d",
				check.namespace, check.name, layout.Align, check.goAlign)
		}
	}
}

// TestGUIDLayoutMatchesTheRealType pins the one shape whose alignment is not its
// size: win32.GUID is {uint32, uint16, uint16, [8]byte} — sixteen bytes aligned
// to four, not to eight. Taken from the actual type rather than restated.
func TestGUIDLayoutMatchesTheRealType(t *testing.T) {
	if got := unsafe.Sizeof(win32.GUID{}); got != guidSize {
		t.Errorf("guidSize = %d, but win32.GUID is %d bytes", guidSize, got)
	}
	if got := unsafe.Alignof(win32.GUID{}); got != guidAlign {
		t.Errorf("guidAlign = %d, but win32.GUID aligns to %d", guidAlign, got)
	}
}

func TestUnknownStructHasNoLayout(t *testing.T) {
	if _, ok := mapper(t).StructLayout("Microsoft.UI.Xaml", "NoSuchStruct"); ok {
		t.Error("an unknown struct reported a layout")
	}
}

// TestEveryStructWithFieldsIsSizeable is the completeness check that matters.
// Emit has to classify a by-value struct parameter's calling convention, so a
// struct with fields that cannot be sized would be a member it could neither pass
// correctly nor honestly skip.
//
// Fieldless structs are excluded, and that is not a loophole. Every one of them in
// this metadata is an [ApiContract] version marker — WinUIContract, XamlContract,
// forty in total — which exists only to be named as an argument to
// [ContractVersion]. None appears in a signature, so none needs a calling
// convention; TestContractMarkersAreTheOnlyFieldlessStructs holds that line.
func TestEveryStructWithFieldsIsSizeable(t *testing.T) {
	m := mapper(t)
	var checked int
	var unsizeable []string
	for _, meta := range m.Registry.Namespaces {
		for name := range meta.Structs {
			definition := meta.Structs[name]
			if len(definition.Fields) == 0 || !m.StructEmittable(meta.Namespace, name) {
				continue
			}
			checked++
			if _, ok := m.StructLayout(meta.Namespace, name); !ok {
				unsizeable = append(unsizeable, meta.Namespace+"."+name)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no emittable structs with fields found")
	}
	if len(unsizeable) > 0 {
		t.Errorf("%d structs with fields have no layout: %v", len(unsizeable), unsizeable)
	}
	t.Logf("%d structs with fields, all sizeable", checked)
}

// TestContractMarkersAreTheOnlyFieldlessStructs is what makes the exclusion above
// safe. If a real value struct ever turned up with no fields, it would be
// silently unpassable, so the assumption is stated rather than relied on: every
// fieldless struct is named *Contract.
func TestContractMarkersAreTheOnlyFieldlessStructs(t *testing.T) {
	m := mapper(t)
	var markers, unexpected []string
	for _, meta := range m.Registry.Namespaces {
		for name := range meta.Structs {
			if len(meta.Structs[name].Fields) > 0 {
				continue
			}
			full := meta.Namespace + "." + name
			if strings.HasSuffix(name, "Contract") {
				markers = append(markers, full)
				continue
			}
			unexpected = append(unexpected, full)
		}
	}
	if len(unexpected) > 0 {
		t.Errorf("%d fieldless structs are not contract markers, so they would be "+
			"unpassable by value without anyone noticing: %v", len(unexpected), unexpected)
	}
	if len(markers) == 0 {
		t.Error("no contract markers found; the metadata shape has changed")
	}
	t.Logf("%d fieldless structs, all [ApiContract] markers", len(markers))
}

func TestRoundUp(t *testing.T) {
	for _, check := range []struct{ offset, align, want int }{
		{0, 8, 0}, {1, 8, 8}, {8, 8, 8}, {9, 8, 16},
		{5, 4, 8}, {4, 4, 4}, {7, 1, 7}, {7, 0, 7},
	} {
		if got := roundUp(check.offset, check.align); got != check.want {
			t.Errorf("roundUp(%d, %d) = %d, want %d", check.offset, check.align, got, check.want)
		}
	}
}

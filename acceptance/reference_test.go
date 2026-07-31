//go:build windows && amd64

package acceptance

// Tests for the two things the scrolling batch added to app: a Go-implemented
// IReference<T>, and a collection usable as an ItemsSource.
//
// Both are exercised by gallery pages, but a page only proves the tree LAYS OUT. These
// assert the values actually cross the ABI, which is the part that would fail silently.

import (
	"testing"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	wrtnumerics "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation/numerics"
)

// TestReferenceRoundTripsThroughTheABI reads the value back through the projected
// interface rather than from the Go struct.
//
// Reading r.value would test nothing: the question is whether a caller that has only the
// interface pointer — which is all the framework ever has — gets the value out. That
// path is the generated Value() method calling slot 6, so it exercises the vtable, the
// trampoline and the write through the out pointer.
func TestReferenceRoundTripsThroughTheABI(t *testing.T) {
	enterApartment(t)

	want := wrtnumerics.Vector2{X: 12.5, Y: -3.25}
	reference, err := app.NewReference[uixaml.IReferenceOfVector2](
		want, &uixaml.IID_IReferenceOfVector2,
		"Windows.Foundation.IReference`1<Windows.Foundation.Numerics.Vector2>")
	if err != nil {
		t.Fatalf("NewReference: %v", err)
	}
	defer reference.Close()

	got, err := reference.Value().Value()
	if err != nil {
		t.Fatalf("reading Value through the projected interface: %v", err)
	}
	if got != want {
		t.Errorf("Value() = %+v, want %+v", got, want)
	}
}

// TestReferenceAnswersItsIID checks the object is reachable the way the framework will
// reach it: by QueryInterface for the pinterface IID, not by the pointer it was handed.
func TestReferenceAnswersItsIID(t *testing.T) {
	enterApartment(t)

	reference, err := app.NewReference[uixaml.IReferenceOfDouble](
		42.5, &uixaml.IID_IReferenceOfDouble, "Windows.Foundation.IReference`1<Double>")
	if err != nil {
		t.Fatalf("NewReference: %v", err)
	}
	defer reference.Close()

	queried, err := winrt.QueryInterface[uixaml.IReferenceOfDouble](
		unsafe.Pointer(reference.Value()), &uixaml.IID_IReferenceOfDouble)
	if err != nil {
		t.Fatalf("QueryInterface for IReference<Double>: %v", err)
	}
	defer queried.Release()

	got, err := queried.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if got != 42.5 {
		t.Errorf("Value() = %v, want 42.5", got)
	}
}

// TestItemsSourceIsAReadableVector checks the collection through IVector<Object>, which
// is what a control does with it.
//
// The elements go in as boxed strings and come back as IInspectables; unboxing them
// again is what proves the codec retained the right thing rather than a copy of a
// pointer that has since been released.
func TestItemsSourceIsAReadableVector(t *testing.T) {
	enterApartment(t)

	want := []string{"alpha", "beta", "gamma"}
	source, err := app.NewStringItemsSource(want, winrt.CollectionIIDs{
		Iterable:   uixaml.IID_IIterableOfObject,
		Iterator:   uixaml.IID_IIteratorOfObject,
		VectorView: uixaml.IID_IVectorViewOfObject,
		Vector:     uixaml.IID_IVectorOfObject,
	})
	if err != nil {
		t.Fatalf("NewStringItemsSource: %v", err)
	}
	defer source.Close()

	vector, err := winrt.QueryInterface[uixaml.IVectorOfObject](
		unsafe.Pointer(source.Inspectable()), &uixaml.IID_IVectorOfObject)
	if err != nil {
		t.Fatalf("QueryInterface for IVector<Object>: %v", err)
	}
	defer vector.Release()

	size, err := vector.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if int(size) != len(want) {
		t.Fatalf("Size() = %d, want %d", size, len(want))
	}

	for index, expected := range want {
		element, err := vector.GetAt(uint32(index))
		if err != nil {
			t.Fatalf("GetAt(%d): %v", index, err)
		}
		// Unboxing is the reverse of app.Box: query the boxed value for IPropertyValue
		// and read it back in the type it was created as.
		value, err := winrt.QueryInterface[wrtfoundation.IPropertyValue](
			unsafe.Pointer(element), &wrtfoundation.IID_IPropertyValue)
		if err != nil {
			element.Release()
			t.Fatalf("element %d is not an IPropertyValue: %v", index, err)
		}
		got, err := value.GetString()
		value.Release()
		element.Release()
		if err != nil {
			t.Fatalf("GetString on element %d: %v", index, err)
		}
		if got != expected {
			t.Errorf("element %d = %q, want %q", index, got, expected)
		}
	}
}

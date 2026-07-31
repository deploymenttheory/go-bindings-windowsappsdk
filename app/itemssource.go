//go:build windows && amd64

package app

// Building an ItemsSource from a Go slice.
//
// There are two ways to put items into a control, and modern WinUI uses the second.
//
// ItemsControl.Items is a collection the FRAMEWORK creates and owns: get it, Append to
// it, done. That is what the CommonStyles pages use, and it needs nothing from here.
//
// ItemsSource is the opposite. The control does not create a collection, it consumes
// one the caller supplies, and it is the only route on ItemsRepeater and ItemsView —
// neither has an Items property at all. So a Go application driving them has to hand
// WinRT a collection object, and Go slices are not that.
//
// go-bindings-winrt implements the collection (winrt.NewVectorObject). What it cannot
// know is which IIDs to answer QueryInterface with, because IVector<Object> is
// monomorphized into each consuming package — the four IIDs live in the generated
// bindings, not in the runtime. Assembling that by hand is four IID references, a
// codec, a boxing loop and a lifetime, per call site.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// ItemsSource is a Go-owned IVector<Object> suitable for an ItemsSource property.
type ItemsSource struct {
	vector *winrt.Vector

	// boxed are the IInspectables handed to the vector. The vector retains each, so
	// these are this side's own references and are released by Close.
	boxed []*syswinrt.IInspectable
}

// NewItemsSource boxes each value and returns a collection over them.
//
// iids are the monomorphized IIDs from the package whose control will consume it. For
// the XAML cluster that is:
//
//	source, err := app.NewItemsSource(values, winrt.CollectionIIDs{
//		Iterable:   uixaml.IID_IIterableOfObject,
//		Iterator:   uixaml.IID_IIteratorOfObject,
//		VectorView: uixaml.IID_IVectorViewOfObject,
//		Vector:     uixaml.IID_IVectorOfObject,
//	})
//	defer source.Close()
//	err = repeater.SetItemsSource(source.Inspectable())
//
// They are asked for rather than assumed because this package must not depend on any
// one generated package: the same helper serves controls in any of them, and the IIDs
// genuinely differ — a pinterface IID is derived from the type arguments, so
// IVector<Object> in one namespace and IVector<Object> in another are the same IID only
// because the argument is the same. Passing the wrong package's IIDs would produce a
// collection the control cannot QueryInterface, which fails at binding time rather
// than here.
//
// The control holds a reference for as long as it is bound to the collection, so Close
// must not run before the control is done with it. Values are boxed with Box, so the
// same set of Go types is supported.
func NewItemsSource(values []any, iids winrt.CollectionIIDs) (*ItemsSource, error) {
	source := &ItemsSource{}
	items := make([]any, 0, len(values))
	for index, value := range values {
		boxed, err := Box(value)
		if err != nil {
			source.Close()
			return nil, fmt.Errorf("app: boxing item %d: %w", index, err)
		}
		source.boxed = append(source.boxed, boxed)
		// CodecInterface's payload element is the interface pointer as a uintptr, not
		// an unsafe.Pointer: it calls AddRef and Release on the word directly. Passing
		// an unsafe.Pointer type-asserts and panics inside a COM callback, where a Go
		// panic cannot unwind past the native frames — the process wedges rather than
		// reporting anything. Storing a COM pointer as a uintptr is safe for the usual
		// reason: it addresses native memory the collector never moves or tracks.
		items = append(items, uintptr(unsafe.Pointer(boxed)))
	}

	// CodecInterface is the codec for IInspectable elements: it retains on the way in
	// and releases on the way out, which is what makes the vector's copy independent of
	// this side's.
	source.vector = winrt.NewVectorObject(
		"Windows.Foundation.Collections.IVector`1<Object>", iids, winrt.CodecInterface, items)
	return source, nil
}

// NewStringItemsSource is the common case: a list of strings.
func NewStringItemsSource(values []string, iids winrt.CollectionIIDs) (*ItemsSource, error) {
	boxed := make([]any, len(values))
	for i, value := range values {
		boxed[i] = value
	}
	return NewItemsSource(boxed, iids)
}

// Inspectable is the collection, typed for an ItemsSource property.
func (s *ItemsSource) Inspectable() *syswinrt.IInspectable {
	return (*syswinrt.IInspectable)(unsafe.Pointer(s.vector))
}

// Close releases this side's references: the collection itself, and the boxed values it
// retained copies of.
func (s *ItemsSource) Close() {
	if s.vector != nil {
		s.vector.Release()
		s.vector = nil
	}
	for _, boxed := range s.boxed {
		boxed.Release()
	}
	s.boxed = nil
}

// NewObjectItemsSource is NewItemsSource for items that are already WinRT objects —
// UIElements, view models, anything with an IInspectable.
//
// The difference from NewItemsSource is that nothing is boxed. Box turns a Go VALUE into
// a PropertyValue; an object is already an IInspectable and boxing one would either fail
// or, worse, wrap a pointer as a number and hand the control something it cannot render.
// ItemsRepeater supports UIElements as items directly — no ItemTemplate needed, the
// element IS the realized element — and that path needs this constructor.
//
// The collection retains each object, so the caller keeps its own references and should
// release them when done. Close releases only what this side added.
func NewObjectItemsSource(objects []*syswinrt.IInspectable, iids winrt.CollectionIIDs) (*ItemsSource, error) {
	items := make([]any, 0, len(objects))
	for index, object := range objects {
		if object == nil {
			return nil, fmt.Errorf("app: item %d is nil", index)
		}
		// The payload element is the pointer as a uintptr — CodecInterface's form,
		// which retains and releases the word directly. See NewItemsSource.
		items = append(items, uintptr(unsafe.Pointer(object)))
	}
	return &ItemsSource{
		vector: winrt.NewVectorObject(
			"Windows.Foundation.Collections.IVector`1<Object>", iids, winrt.CodecInterface, items),
	}, nil
}

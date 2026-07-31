//go:build windows && amd64

package app

// Building an IReference<T> — a boxed nullable — from Go.
//
// WinRT expresses "a T, or nothing" as IReference<T>, and the scrolling API is full of
// them: ScrollPresenter.ZoomTo takes a nullable centre point, AddScrollVelocity a
// nullable inertia decay rate. Passing nil means "no value" and is often what you want.
// Passing a value needs an object.
//
// For most types that object comes from Windows.Foundation.PropertyValue, whose statics
// box each primitive — CreateInt32, CreateString, CreatePoint. app.Box is that route.
// It does not cover the numerics types:
//
//	CreateEmpty CreateUInt8 CreateInt16 CreateUInt16 CreateInt32 CreateUInt32
//	CreateInt64 CreateUInt64 CreateSingle CreateDouble CreateChar16 CreateBoolean
//	CreateString CreateInspectable CreateGuid CreateDateTime CreateTimeSpan
//	CreatePoint CreateSize CreateRect
//
// That is the complete list, and there is no CreateVector2. PropertyValue predates
// Windows.Foundation.Numerics, and it was never extended — so an IReference<Vector2>
// cannot be obtained from the operating system at all. This is not a gap in the
// projection: C++/WinRT has the same problem and solves it the same way, by
// IMPLEMENTING IReference<T> locally. winrt::box_value of a Vector2 returns an object
// C++/WinRT wrote, not one the platform handed back.
//
// So this does the same. IReference<T> is one method — get_Value at slot 6, writing
// through an out pointer — which is small enough to implement exactly.
//
// # WHERE THIS DOES NOT WORK, AND WHAT TO USE INSTEAD
//
// A Go-implemented IReference<T> satisfies a callee that only calls get_Value.
// ScrollPresenter.ZoomTo does exactly that, which is the case this was written for.
//
// It is REFUSED by XAML's dependency-property system. Setting DoubleAnimation.From to one
// fails with E_FAIL, because that property is stored as a dependency-property value and
// read back through IPropertyValue — an interface this object does not implement and
// cannot usefully fake.
//
// The rule that follows is short:
//
//   - if PropertyValue can box the type, BOX IT and query for IReference<T>. A boxed
//     value implements both interfaces, so it satisfies either consumer. Every WinRT
//     primitive plus Point, Size, Rect and GUID is in that set — see Box.
//   - use NewReference only for the types PropertyValue CANNOT box, which is the
//     numerics: Vector2, Vector3, Matrix and the rest. Those never appear as
//     dependency-property values in this metadata, so the limitation and the need do
//     not overlap.
//
// Found by ProgressRingStoryboardAnimationPage in the gallery, which animates a double
// and needs the boxed form.

import (
	"fmt"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// ePointer is what get_Value returns when handed a null out pointer. Writing through it
// would take the process down, and a caller passing null has a bug worth reporting.
const ePointer = uintptr(0x80004003) // E_POINTER

// Reference is a Go-implemented IReference<T>, holding one value of type V.
//
// T is the projected interface the call site wants — uixaml.IReferenceOfVector2 and the
// like, monomorphized into the package that consumes it. V is the value type it wraps.
type Reference[T any, V any] struct {
	implementation *winrt.Implementation

	// value is stored on the Reference rather than captured by the method closure so
	// that it is reachable from the object for as long as the object lives. The method
	// reads it on every call, which native code may make at any point before Close.
	value V
}

// NewReference builds an IReference<T> whose Value is the given one.
//
// T must be named explicitly; V is inferred:
//
//	centre, err := app.NewReference[uixaml.IReferenceOfVector2](
//		wrtnumerics.Vector2{X: 100, Y: 100},
//		&uixaml.IID_IReferenceOfVector2,
//		"Windows.Foundation.IReference`1<Windows.Foundation.Numerics.Vector2>")
//	defer centre.Close()
//	_, err = presenter.ZoomTo(2, centre.Value())
//
// runtimeClass is what the object reports from GetRuntimeClassName. Nothing in the
// scrolling API reads it, but it is what appears in a framework diagnostic, so it is
// asked for rather than invented.
//
// V MUST be plain data — scalars, structs of scalars, GUIDs. The value is copied into
// the caller's memory a word at a time by the ABI's own rules, so a V containing a Go
// pointer would put one somewhere the garbage collector cannot see it. Every WinRT type
// that occurs as an IReference argument satisfies this; the constraint is on Go types a
// caller might substitute.
func NewReference[T any, V any](value V, iid *win32.GUID, runtimeClass string) (*Reference[T, V], error) {
	reference := &Reference[T, V]{value: value}

	// Slot 6 is get_Value: one out parameter, the address to write the value to.
	//
	// winrt.InParam is the sanctioned conversion for an address native code supplied —
	// the inbound counterpart of winrt.OutParam, added in go-bindings-winrt v0.6.0 for
	// exactly this case. It keeps the "this is a native address, not a Go pointer"
	// justification in one documented place instead of once per call site, and keeps
	// `go vet` clean here.
	//
	// The null check is not optional. A caller passing null for an out-parameter is a
	// caller bug, and the documented answer is E_POINTER rather than the access
	// violation that writing through it would give.
	getValue := func(args []uintptr) uintptr {
		if len(args) < 1 || args[0] == 0 {
			return ePointer
		}
		*(*V)(winrt.InParam(args[0])) = reference.value
		return 0
	}

	implementation, err := winrt.NewImplementation(runtimeClass,
		winrt.Interface{IID: *iid, Methods: []winrt.Method{getValue}})
	if err != nil {
		return nil, fmt.Errorf("app: implementing %s: %w", runtimeClass, err)
	}
	reference.implementation = implementation
	return reference, nil
}

// Value is the pointer to pass to the API expecting the IReference.
//
// It is the implementation's COM identity reinterpreted as T. That is sound because a
// projected interface pointer is exactly one word — the vtable — and this object's
// identity facet carries the vtable for the IID it was built with.
func (r *Reference[T, V]) Value() *T {
	return (*T)(r.implementation.Ptr())
}

// Close releases the caller's reference.
//
// Call it once no native code can still hold the object. Every consumer in the scrolling
// API reads the value during the call and keeps nothing, so a defer at the call site is
// correct there.
func (r *Reference[T, V]) Close() {
	if r.implementation != nil {
		r.implementation.Close()
		r.implementation = nil
	}
}

//go:build windows && amd64

package app

// Reading a boxed value back out.
//
// Box is only half of the round trip, and the other half is needed more often than it
// looks: a WinRT property typed IInspectable gives back an IInspectable, so anything
// read from ItemsSource, TreeViewNode.Content, ContentControl.Content or a
// DependencyProperty arrives boxed. Doing it by hand is a QueryInterface for
// IPropertyValue and a Get<Type> whose name has to match how the value was created.
//
// It was written after the second place needed it — an acceptance test and then
// TreeViewNode.Content in the gallery — rather than speculatively, which is the same
// bar the rest of this package is held to.

import (
	"fmt"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// Unbox reads a boxed value created by Box (or by anything else that produced a
// PropertyValue) back as T.
//
//	text, err := app.Unbox[string](content)
//
// The caller keeps ownership of value; Unbox releases only what it creates itself.
//
// The type must match how the value was BOXED, not merely be convertible: a value
// created with CreateInt32 does not come back through GetInt64. That is WinRT's rule
// rather than this function's, and the error says which type was asked for so a mismatch
// reads as a mismatch instead of as a zero.
func Unbox[T any](value *syswinrt.IInspectable) (T, error) {
	var zero T
	if value == nil {
		return zero, fmt.Errorf("app: cannot unbox a nil IInspectable")
	}

	property, err := winrt.QueryInterface[wrtfoundation.IPropertyValue](
		unsafe.Pointer(value), &wrtfoundation.IID_IPropertyValue)
	if err != nil {
		return zero, fmt.Errorf("app: the value is not a boxed PropertyValue: %w", err)
	}
	defer property.Release()

	// The switch is on a T-typed variable rather than on T itself, because Go has no
	// type switch over a type parameter. Each branch assigns through a *T that aliases
	// the same storage, which is what lets one generic function cover every case
	// without reflection.
	var result any
	switch any(zero).(type) {
	case string:
		result, err = property.GetString()
	case bool:
		result, err = property.GetBoolean()
	case int16:
		result, err = property.GetInt16()
	case int32:
		result, err = property.GetInt32()
	case int64:
		result, err = property.GetInt64()
	case byte:
		result, err = property.GetUInt8()
	case uint16:
		result, err = property.GetUInt16()
	case uint32:
		result, err = property.GetUInt32()
	case uint64:
		result, err = property.GetUInt64()
	case float32:
		result, err = property.GetSingle()
	case float64:
		result, err = property.GetDouble()
	case win32.GUID:
		result, err = property.GetGuid()
	case wrtfoundation.Point:
		result, err = property.GetPoint()
	case wrtfoundation.Size:
		result, err = property.GetSize()
	case wrtfoundation.Rect:
		result, err = property.GetRect()
	default:
		return zero, fmt.Errorf("app: cannot unbox to %T; Box and Unbox cover the WinRT "+
			"primitive set plus Point, Size, Rect and GUID", zero)
	}
	if err != nil {
		return zero, fmt.Errorf("app: reading the boxed value as %T: %w", zero, err)
	}

	typed, ok := result.(T)
	if !ok {
		// Unreachable unless a branch above is wired to the wrong getter, which is
		// exactly the mistake worth catching loudly rather than returning a zero for.
		return zero, fmt.Errorf("app: internal: unboxed %T where %T was expected", result, zero)
	}
	return typed, nil
}

// UnboxOr is Unbox with a fallback, for the reads where a missing or differently-typed
// value is ordinary rather than an error — a Content that has not been set yet, say.
func UnboxOr[T any](value *syswinrt.IInspectable, fallback T) T {
	unboxed, err := Unbox[T](value)
	if err != nil {
		return fallback
	}
	return unboxed
}

//go:build windows && amd64

package app

// The ergonomic layer, and the shape it deliberately does NOT take.
//
// The obvious way to make generated bindings pleasant is to promote inherited members
// onto the classes that inherit them, so Button.SetMargin works without first querying
// IFrameworkElement. That was measured before it was written, and it does not survive
// the measurement:
//
//	every interface on the class and its bases   165,561 promoted members
//	default interfaces of the class and its bases  75,046
//	the same, properties only                      59,863
//
// Button alone would gain 331 methods, and IUIElement carries 183 by itself. That is
// not an ergonomic surface, it is an unusable autocomplete list attached to a
// generated tree several times its current size.
//
// So the XAML surface is not made smaller here. What is removed is the CEREMONY around
// it — the query-release pair, the error check per property, the handler built in two
// steps, the boxing dance — with a handful of generic combinators that work against
// every generated type without naming any of them. Nothing below mentions a control.
// A new WinUI release adds types and this file does not change.
//
// The escape hatch is that there is nothing to escape: these take and return the
// generated types, so any call can be written the long way instead, and mixed freely.

import (
	"fmt"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

// Releaser is any COM interface pointer: everything generated embeds IInspectable,
// which embeds IUnknown.
type Releaser interface {
	Release() uint32
}

// Handler is a generated event-handler type. Not a Releaser: a handler is a
// Go-implemented COM object wrapped for the caller, so it closes rather than
// releasing, and Close is the wrapper's own name for dropping its reference.
type Handler interface {
	Close()
}

// All returns the first error among its arguments, or nil.
//
// Every generated setter returns an error, and a block of them is the most common
// thing in UI code — so the choice is between an if per line and this. The arguments
// are all evaluated, which is what makes it usable for setters: they are calls, and
// they must all run.
//
//	return app.All(
//		panel.SetSpacing(16),
//		panel.SetPadding(uixaml.Thickness{Left: 32, Top: 32, Right: 32, Bottom: 32}),
//	)
//
// Not for anything whose later steps depend on an earlier one succeeding — for that
// the error has to be checked where it happens.
func All(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// With queries an interface, runs fn against it, and releases it.
//
// get is a generated As<Interface> method value, so this covers every class and every
// interface in the tree with no per-type code:
//
//	err := app.With(button.AsFrameworkElement, func(fe *uixaml.IFrameworkElement) error {
//		return app.All(
//			fe.SetHorizontalAlignment(uixaml.HorizontalAlignmentCenter),
//			fe.SetWidth(200),
//		)
//	})
//
// The release is deferred, so it happens whichever way fn returns. Writing it by hand
// is three lines of ceremony per interface and one forgotten Release away from a leak
// that nothing reports.
func With[T Releaser](get func() (T, error), fn func(T) error) error {
	value, err := get()
	if err != nil {
		return err
	}
	defer value.Release()
	return fn(value)
}

// On grounds a delegate and registers it as an event handler.
//
// add is a generated Add<Event> method value and ground is the generated New<Handler>
// constructor, which together pin the handler type and the argument type — so this one
// function covers every two-argument event in the metadata, which is nearly all of
// them:
//
//	_, err := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
//		func(sender *syswinrt.IInspectable, args *uixaml.IRoutedEventArgs) {
//			// on the UI thread
//		})
//
// The handler is NOT closed. It stays registered, and the runtime holds a reference to
// it, until the returned token is passed to the matching Remove<Event> — closing it
// here would unregister the callback the caller just asked for.
func On[H Handler, A any](
	add func(H) (syswinrt.EventRegistrationToken, error),
	ground func(func(*syswinrt.IInspectable, *A)) (H, error),
	fn func(sender *syswinrt.IInspectable, args *A),
) (syswinrt.EventRegistrationToken, error) {
	handler, err := ground(fn)
	if err != nil {
		return syswinrt.EventRegistrationToken{}, err
	}
	token, err := add(handler)
	if err != nil {
		// Registration failed, so nothing holds the handler and closing it here is the
		// only way it is ever dropped. On success the opposite is true: the runtime
		// holds a reference until the token is passed to the matching Remove.
		handler.Close()
		return syswinrt.EventRegistrationToken{}, err
	}
	return token, nil
}

// Box wraps a Go value as the IInspectable that WinRT's untyped properties take —
// Button.Content, ListBox items, DependencyProperty values.
//
// WinRT has no untyped value: an IInspectable holding a string is a boxed
// PropertyValue, created through the activation factory. Doing that by hand is the
// statics call, the create, and two releases, for what reads as an assignment.
//
// The caller owns the result and must Release it. Supported: the WinRT primitive set
// plus Point, Size and Rect. An unsupported type is an error rather than a silent
// CreateEmpty, because a Content that silently became empty is worse to debug than one
// that refused.
func Box(value any) (*syswinrt.IInspectable, error) {
	statics, err := wrtfoundation.PropertyValueStatics()
	if err != nil {
		return nil, fmt.Errorf("app: the PropertyValue activation factory: %w", err)
	}
	defer statics.Release()

	switch v := value.(type) {
	case nil:
		return statics.CreateEmpty()
	case string:
		return statics.CreateString(v)
	case bool:
		return statics.CreateBoolean(v)
	case int:
		// Go's int is 64-bit here, but a UI value that fits an Int32 should box as one:
		// XAML's own converters expect the narrower type, and an Int64 where an Int32
		// is expected fails at the unbox rather than at the call.
		if v >= -1<<31 && v < 1<<31 {
			return statics.CreateInt32(int32(v))
		}
		return statics.CreateInt64(int64(v))
	case int16:
		return statics.CreateInt16(v)
	case int32:
		return statics.CreateInt32(v)
	case int64:
		return statics.CreateInt64(v)
	case byte:
		return statics.CreateUInt8(v)
	case uint16:
		return statics.CreateUInt16(v)
	case uint32:
		return statics.CreateUInt32(v)
	case uint64:
		return statics.CreateUInt64(v)
	case float32:
		return statics.CreateSingle(v)
	case float64:
		return statics.CreateDouble(v)
	case win32.GUID:
		return statics.CreateGuid(v)
	case wrtfoundation.Point:
		return statics.CreatePoint(v)
	case wrtfoundation.Size:
		return statics.CreateSize(v)
	case wrtfoundation.Rect:
		return statics.CreateRect(v)
	case *syswinrt.IInspectable:
		// Already an object. Returned with a reference of its own, so the caller can
		// release the result exactly as they would any other Box.
		if v == nil {
			return statics.CreateEmpty()
		}
		v.AddRef()
		return v, nil
	}
	return nil, fmt.Errorf("app: cannot box %T as a WinRT property value", value)
}

// SetContent boxes a value and assigns it as a content control's Content.
//
// The single most common thing anyone does with a Button, and by hand it is a query,
// a statics call, a box, and three releases — for what a caller thinks of as setting
// a caption.
func SetContent[T Releaser](asContentControl func() (T, error), value any) error {
	boxed, err := Box(value)
	if err != nil {
		return err
	}
	defer boxed.Release()

	return With(asContentControl, func(content T) error {
		setter, ok := any(content).(interface {
			SetContent(*syswinrt.IInspectable) error
		})
		if !ok {
			return fmt.Errorf("app: %T has no SetContent", content)
		}
		return setter.SetContent(boxed)
	})
}

// Append adds elements to a Panel's Children, releasing each queried UIElement.
//
// Children is declared on Panel, so a StackPanel reaches it through AsPanel, and each
// child has to be queried for IUIElement before it can go in. That is two queries and
// two releases per child, around one Append.
func Append[P Releaser](asPanel func() (P, error), children ...func() (*uixaml.IUIElement, error)) error {
	return With(asPanel, func(panel P) error {
		holder, ok := any(panel).(interface {
			Children() (*uixaml.IVectorOfUIElement, error)
		})
		if !ok {
			return fmt.Errorf("app: %T has no Children", panel)
		}
		collection, err := holder.Children()
		if err != nil {
			return err
		}
		defer collection.Release()

		for _, asUIElement := range children {
			if err := With(asUIElement, collection.Append); err != nil {
				return err
			}
		}
		return nil
	})
}

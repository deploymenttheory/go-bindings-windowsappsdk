//go:build windows && amd64

package pages

// Helpers shared by the collections batch.
//
// Every page here needs the same two things the CommonStyles pages did not: a WinRT
// collection to bind to, and a DataTemplate to render it with. ItemsRepeater, ItemsView
// and RadioButtons have no Items property to append to — ItemsSource is the only route —
// so both are unavoidable rather than stylistic.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// xamlCollectionIIDs are the monomorphized IVector<Object> IIDs for the XAML cluster.
//
// app.NewItemsSource asks for these rather than assuming them: a pinterface IID is
// derived from its type arguments, so the runtime cannot know which package's
// instantiation a given control will QueryInterface for.
var xamlCollectionIIDs = winrt.CollectionIIDs{
	Iterable:   uixaml.IID_IIterableOfObject,
	Iterator:   uixaml.IID_IIteratorOfObject,
	VectorView: uixaml.IID_IVectorViewOfObject,
	Vector:     uixaml.IID_IVectorOfObject,
}

// XamlCollectionIIDs exposes them for tests that build a source of their own.
func XamlCollectionIIDs() winrt.CollectionIIDs { return xamlCollectionIIDs }

// itemsSource is app.NewStringItemsSource with this package's IIDs already supplied.
func itemsSource(values []string) (*app.ItemsSource, error) {
	return app.NewStringItemsSource(values, xamlCollectionIIDs)
}

// dataTemplate parses a DataTemplate fragment.
//
// A DataTemplate is a factory the framework instantiates once per item, so there is no
// object graph to build in Go — this is the case app/markup.go exists for, not a
// shortcut around the projection.
func dataTemplate(fragment string) (*uixaml.IDataTemplate, error) {
	return app.LoadMarkup[uixaml.IDataTemplate](app.Markup(fragment), &uixaml.IID_IDataTemplate)
}

// elementFactoryOf views a DataTemplate as the IElementFactory that ItemsView and
// AnnotatedScrollBar's templates are typed to.
//
// A DataTemplate implements IElementFactory; the interface is what lets a template and a
// hand-written factory be interchangeable. The caller owns the result.
func elementFactoryOf(template *uixaml.IDataTemplate) (*uixaml.IElementFactory, error) {
	factory, err := winrt.QueryInterface[uixaml.IElementFactory](
		unsafe.Pointer(template), &uixaml.IID_IElementFactory)
	if err != nil {
		return nil, fmt.Errorf("a DataTemplate should be an IElementFactory: %w", err)
	}
	return factory, nil
}

// inspectableOf views any projected interface pointer as the IInspectable that WinRT's
// untyped properties — ItemsSource, ItemsRepeater.ItemTemplate — are declared with.
//
// Sound because every projected interface pointer is one word, the vtable, and every
// WinRT interface derives from IInspectable, so slots 0-5 are present whatever it is.
func inspectableOf[T any](value *T) *syswinrt.IInspectable {
	return (*syswinrt.IInspectable)(unsafe.Pointer(value))
}

// templatedRepeater is the shape most Repeater pages start from: a repeater with a
// layout, a template and a source, all owned by the caller.
func templatedRepeater(layout *uixaml.ILayout, template string, values []string) (*uixaml.ItemsRepeater, *app.ItemsSource, error) {
	repeater, err := uixaml.NewItemsRepeater()
	if err != nil {
		return nil, nil, err
	}
	if layout != nil {
		if err := repeater.SetLayout(layout); err != nil {
			return nil, nil, err
		}
	}

	parsed, err := dataTemplate(template)
	if err != nil {
		return nil, nil, err
	}
	defer parsed.Release()
	if err := repeater.SetItemTemplate(inspectableOf(parsed)); err != nil {
		return nil, nil, err
	}

	source, err := itemsSource(values)
	if err != nil {
		return nil, nil, err
	}
	if err := repeater.SetItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, nil, err
	}
	return repeater, source, nil
}

// stackLayout is the layout most of these pages use, orientation varied.
func stackLayout(orientation uixaml.Orientation, spacing float64) (*uixaml.ILayout, error) {
	layout, err := uixaml.NewStackLayout()
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	if err := app.All(layout.SetOrientation(orientation), layout.SetSpacing(spacing)); err != nil {
		return nil, err
	}
	return layout.AsLayout()
}

// uniformGridLayout is the wrapping layout the grid-shaped pages use.
func uniformGridLayout(itemWidth, itemHeight float64) (*uixaml.ILayout, error) {
	layout, err := uixaml.NewUniformGridLayout()
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	if err := app.All(
		layout.SetMinItemWidth(itemWidth),
		layout.SetMinItemHeight(itemHeight),
		layout.SetMinRowSpacing(4),
		layout.SetMinColumnSpacing(4),
	); err != nil {
		return nil, err
	}
	return layout.AsLayout()
}

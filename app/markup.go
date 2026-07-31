//go:build windows && amd64

package app

// Loading XAML markup at run time, for the things that have no imperative form.
//
// Almost everything in a WinUI tree can be built in Go: construct the control, set its
// properties, append it to its parent. Two things cannot, and they are not oversights in
// this projection — they are markup by nature:
//
//   - a DataTemplate is a FACTORY. The framework instantiates it once per item, and what
//     it instantiates is described by the markup. There is no object graph to build,
//     because the graph does not exist until an item needs one.
//   - a Style is a set of property SETTERS keyed by dependency property, applied to
//     whatever the template produces. Expressing one in Go means naming dependency
//     properties by hand, which is longer and less readable than the markup it replaces.
//
// XamlReader parses and instantiates both, and it works here — it is what the earlier
// investigation used to prove that XAML type resolution was intact. So markup is used
// where markup is the honest representation, and nowhere else.
//
// Everything loaded this way goes through the application's IXamlMetadataProvider, which
// app.Run supplies by deriving the Application (see derived.go). Without it the parser
// cannot resolve the types inside WinUI's own theme dictionaries, which is what made
// half the control set unusable before M7.

import (
	"fmt"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	uixamlmarkup "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/markup"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// PresentationNamespace is the default XAML namespace every fragment needs. A fragment
// without it fails to parse, and the error says nothing useful about why.
const PresentationNamespace = `xmlns="http://schemas.microsoft.com/winfx/2006/xaml/presentation"`

// XamlNamespace is the x: namespace, which carries the directives rather than the types:
// x:Name, x:Key, x:Bind, x:Class.
//
// It is added alongside the presentation namespace because a fragment needing it is not
// unusual — it is most of them. A ControlTemplate names its parts with x:Name so the
// control can find them, a ResourceDictionary keys its entries with x:Key, and without
// this the parser fails with HRESULT 0x802B000A, which says only that something in the
// markup was wrong.
const XamlNamespace = `xmlns:x="http://schemas.microsoft.com/winfx/2006/xaml"`

// LoadMarkup parses a XAML fragment and returns it as T.
//
// The caller owns the result and must Release it. The fragment must carry the
// presentation namespace; PresentationNamespace is it, and Markup will add it for you.
//
//	template, err := app.LoadMarkup[uixaml.IDataTemplate](
//		app.Markup(`<DataTemplate><TextBlock Text="{Binding}"/></DataTemplate>`),
//		&uixaml.IID_IDataTemplate)
//
// A parse failure comes back as the XAML parser's own HRESULT. Those are terse —
// 0x802B000A is every parse and resolution failure alike — so the fragment is included
// in the error, which is usually enough to see what is wrong.
func LoadMarkup[T any](xaml string, iid *win32.GUID) (*T, error) {
	reader, err := uixamlmarkup.XamlReaderStatics()
	if err != nil {
		return nil, fmt.Errorf("app: the XamlReader statics: %w", err)
	}
	defer reader.Release()

	object, err := reader.Load(xaml)
	if err != nil {
		return nil, fmt.Errorf("app: parsing markup %q: %w", summarize(xaml), err)
	}
	defer object.Release()

	value, err := winrt.QueryInterface[T](unsafe.Pointer(object), iid)
	if err != nil {
		return nil, fmt.Errorf("app: markup %q parsed but is not the expected type: %w",
			summarize(xaml), err)
	}
	return value, nil
}

// Markup adds the presentation and x: namespaces to a fragment's root element.
//
// Every fragment needs the first and most need the second; repeating both inline makes
// the markup harder to read than the tree it describes. The insertion is at the first
// tag's name only, which is where the parser wants it.
func Markup(fragment string) string {
	for i := 0; i < len(fragment); i++ {
		if fragment[i] != '<' {
			continue
		}
		// Past the tag name: the namespace goes after it, before any attributes.
		for j := i + 1; j < len(fragment); j++ {
			if c := fragment[j]; c == ' ' || c == '>' || c == '/' {
				return fragment[:j] + " " + PresentationNamespace + " " + XamlNamespace + fragment[j:]
			}
		}
		break
	}
	return fragment
}

// summarize shortens a fragment for an error message. A DataTemplate can be a page of
// markup, and the first line is nearly always the part that identifies it.
func summarize(xaml string) string {
	const limit = 80
	for i, c := range xaml {
		if c == '\n' || i == limit {
			return xaml[:i] + "…"
		}
	}
	return xaml
}

// BoxAs boxes a Go value and returns it as T.
//
// WinRT's nullable properties take an IReference<T> — ToggleButton.IsChecked is
// IReference<Bool>, because a CheckBox is tri-state and nil is the third state — and a
// boxed PropertyValue IS one: the same object answers IReference<Bool> as answers
// IPropertyValue. So the boxing and the query are one step, which is how it reads at
// the call site:
//
//	checked, err := app.BoxAs[uixaml.IReferenceOfBool](true, &uixaml.IID_IReferenceOfBool)
//	toggle.SetIsChecked(checked)
//
// The caller owns the result and must Release it. Passing nil for the property itself
// is the third state and needs no helper.
func BoxAs[T any](value any, iid *win32.GUID) (*T, error) {
	boxed, err := Box(value)
	if err != nil {
		return nil, err
	}
	defer boxed.Release()

	result, err := winrt.QueryInterface[T](unsafe.Pointer(boxed), iid)
	if err != nil {
		return nil, fmt.Errorf("app: a boxed %T is not the requested reference type: %w", value, err)
	}
	return result, nil
}

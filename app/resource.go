//go:build windows && amd64

package app

// Resource lookup: what {StaticResource} and {ThemeResource} do in markup.
//
// A ResourceDictionary is an IMap<Object, Object> at the ABI, and that is the only place
// its Lookup lives — IResourceDictionary itself carries Source, MergedDictionaries and
// ThemeDictionaries and nothing else. So reaching a resource by key is a query to the
// map, a boxed key, and an unbox of the result, which is four calls deep for what markup
// writes as one word.

import (
	"fmt"
	"unsafe"

	win32 "github.com/deploymenttheory/go-bindings-win32/bindings/runtime/win32"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// Resource looks a key up in a resource dictionary and returns it as T.
//
// The dictionary equivalent of {StaticResource Key}:
//
//	style, err := app.Resource[uixaml.IStyle](dictionary, "CaptionTextBlockStyle",
//		&uixaml.IID_IStyle)
//
// The caller owns the result and must Release it. A missing key is an error rather than
// a nil result, because a resource that silently is not there produces a control that
// silently is not styled.
func Resource[T any](dictionary *uixaml.ResourceDictionary, key string, iid *win32.GUID) (*T, error) {
	if dictionary == nil {
		return nil, fmt.Errorf("app: no resource dictionary to look %q up in", key)
	}
	entries, err := dictionary.AsMapOfObjectAndObject()
	if err != nil {
		return nil, fmt.Errorf("app: the dictionary is not a map: %w", err)
	}
	defer entries.Release()

	boxed, err := Box(key)
	if err != nil {
		return nil, err
	}
	defer boxed.Release()

	value, err := entries.Lookup(boxed)
	if err != nil {
		return nil, fmt.Errorf("app: no resource %q: %w", key, err)
	}
	defer value.Release()

	result, err := winrt.QueryInterface[T](unsafe.Pointer(value), iid)
	if err != nil {
		return nil, fmt.Errorf("app: resource %q is not the expected type: %w", key, err)
	}
	return result, nil
}

// ApplicationResource looks a key up in Application.Resources, which is where a theme
// resource merged by XamlControlsResources ends up.
//
// Note the ordering rule this depends on: Application.Resources is unavailable until the
// XAML core is up, and the first Window is what brings it up. app.Run creates that
// window before running the callback, so anything called from a Ready has it.
func ApplicationResource[T any](application *uixaml.IApplication, key string, iid *win32.GUID) (*T, error) {
	resources, err := application.Resources()
	if err != nil {
		return nil, fmt.Errorf("app: Application.Resources: %w", err)
	}
	defer resources.Release()

	// Resources returns the interface; the lookup needs the class's map accessor.
	dictionary, err := winrt.QueryInterface[uixaml.ResourceDictionary](
		unsafe.Pointer(resources), &uixaml.IID_IResourceDictionary)
	if err != nil {
		return nil, fmt.Errorf("app: Application.Resources is not a ResourceDictionary: %w", err)
	}
	defer dictionary.Release()

	return Resource[T](dictionary, key, iid)
}

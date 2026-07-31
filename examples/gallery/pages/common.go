//go:build windows && amd64

package pages

// Shapes shared by the ported pages.
//
// Nearly every TestUI page is a ScrollViewer around a StackPanel of the control under
// test plus the switches that drive it. Writing that out per page would bury the part
// that differs, which is the part worth reading.

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// scrolled wraps content in a vertically scrolling ScrollViewer, which is what almost
// every source page's root element is.
func scrolled(content func() (*uixaml.IUIElement, error)) (*uixaml.IUIElement, error) {
	viewer, err := uixaml.NewScrollViewer()
	if err != nil {
		return nil, err
	}
	if err := app.With(viewer.AsScrollViewer, func(scroll *uixaml.IScrollViewer) error {
		return app.All(
			scroll.SetHorizontalScrollBarVisibility(uixaml.ScrollBarVisibilityDisabled),
			scroll.SetVerticalScrollBarVisibility(uixaml.ScrollBarVisibilityAuto),
		)
	}); err != nil {
		return nil, err
	}
	if err := app.With(content, func(element *uixaml.IUIElement) error {
		return app.With(viewer.AsContentControl, func(host *uixaml.IContentControl) error {
			return host.SetContent(&element.IInspectable)
		})
	}); err != nil {
		return nil, err
	}
	return viewer.AsUIElement()
}

// stack builds a vertical StackPanel of the given children, the body of most pages.
func stack(spacing float64, children ...func() (*uixaml.IUIElement, error)) (*uixaml.StackPanel, error) {
	panel, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		panel.SetOrientation(uixaml.OrientationVertical),
		panel.SetSpacing(spacing),
	); err != nil {
		return nil, err
	}
	if err := app.Append(panel.AsPanel, children...); err != nil {
		return nil, err
	}
	return panel, nil
}

// label builds a TextBlock, the caption beside nearly every switch in these pages.
func label(text string) (*uixaml.TextBlock, error) {
	block, err := uixaml.NewTextBlock()
	if err != nil {
		return nil, err
	}
	if err := block.SetText(text); err != nil {
		return nil, err
	}
	return block, nil
}

//go:build windows && amd64

package pages

// Ports of the smaller CommonStyles pages, which are small enough that one file each
// would be more ceremony than content. The larger ones get their own file.
//
// Sources: controls/dev/CommonStyles/TestUI/{Blank,ScrollBar,ToolTip,Compact,Image}Page.xaml

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "BlankPage", Build: buildBlankPage})
	register(Page{Control: "CommonStyles", Name: "ScrollBarPage", Build: buildScrollBarPage})
	register(Page{Control: "CommonStyles", Name: "ToolTipPage", Build: buildToolTipPage})
	register(Page{Control: "CommonStyles", Name: "ImagePage", Build: buildImagePage})
}

// BlankPage is empty in the source — it exists so a test can navigate somewhere that
// costs nothing. Ported as an empty panel rather than skipped: a page that renders
// nothing is still a page that must render.
func buildBlankPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	panel, err := stack(0)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ScrollBarPage: a horizontal and a vertical ScrollBar in a fixed-size grid.
//
// ScrollBar is a RangeBase, so Orientation is its own but Minimum/Maximum/Value are two
// classes up — the same shape as ProgressBar.
func buildScrollBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	grid, err := gridOf([]uixaml.GridLength{px(300)}, []uixaml.GridLength{px(300), auto()})
	if err != nil {
		return nil, err
	}
	place, err := newPlacement()
	if err != nil {
		return nil, err
	}
	defer place.Close()

	horizontal, err := uixaml.NewScrollBar()
	if err != nil {
		return nil, err
	}
	vertical, err := uixaml.NewScrollBar()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		horizontal.SetOrientation(uixaml.OrientationHorizontal),
		horizontal.SetIndicatorMode(uixaml.ScrollingIndicatorModeMouseIndicator),
		vertical.SetOrientation(uixaml.OrientationVertical),
		vertical.SetIndicatorMode(uixaml.ScrollingIndicatorModeMouseIndicator),
		app.With(horizontal.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetVerticalAlignment(uixaml.VerticalAlignmentBottom)
		}),
	); err != nil {
		return nil, err
	}
	if err := app.Append(grid.AsPanel, horizontal.AsUIElement, vertical.AsUIElement); err != nil {
		return nil, err
	}
	if err := place.at(vertical.AsUIElement, 0, 1); err != nil {
		return nil, err
	}
	return grid.AsUIElement()
}

// ToolTipPage: controls with a ToolTip attached.
//
// ToolTipService.ToolTip is an attached property on a static class, exactly like
// Grid.Row — the tooltip is not a child of the control, it is a property set on it.
func buildToolTipPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	statics, err := uixaml.ToolTipServiceStatics()
	if err != nil {
		return nil, err
	}
	defer statics.Release()

	heading, err := label("Sample controls with ToolTip set")
	if err != nil {
		return nil, err
	}
	button, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(button.AsContentControl, "Button with a simple ToolTip."); err != nil {
		return nil, err
	}
	simple, err := app.Box("Simple ToolTip")
	if err != nil {
		return nil, err
	}
	defer simple.Release()
	if err := app.With(button.AsUIElement, func(element *uixaml.IUIElement) error {
		target, err := dependencyObjectOf(element)
		if err != nil {
			return err
		}
		defer target.Release()
		return statics.SetToolTip(target, simple)
	}); err != nil {
		return nil, err
	}

	// The source's second case puts a ToolTip OBJECT rather than a string, so it can
	// carry a VerticalOffset.
	offsetTarget, err := label("TextBlock with an offset ToolTip.")
	if err != nil {
		return nil, err
	}
	tip, err := uixaml.NewToolTip()
	if err != nil {
		return nil, err
	}
	tipContent, err := app.Box("Offset ToolTip.")
	if err != nil {
		return nil, err
	}
	defer tipContent.Release()
	if err := app.All(
		tip.SetVerticalOffset(-80),
		app.With(tip.AsContentControl, func(content *uixaml.IContentControl) error {
			return content.SetContent(tipContent)
		}),
	); err != nil {
		return nil, err
	}
	if err := app.With(offsetTarget.AsUIElement, func(element *uixaml.IUIElement) error {
		target, err := dependencyObjectOf(element)
		if err != nil {
			return err
		}
		defer target.Release()
		return app.With(tip.AsUIElement, func(tipElement *uixaml.IUIElement) error {
			return statics.SetToolTip(target, &tipElement.IInspectable)
		})
	}); err != nil {
		return nil, err
	}

	panel, err := stack(16, heading.AsUIElement, button.AsUIElement, offsetTarget.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ImagePage: an Image with each Stretch mode.
//
// The source loads a bitmap from ms-appx. There is no application-local image to point
// at here, so the Source is left unset and the Stretch modes are still exercised —
// which is what the page is about. A missing Source is not an error in XAML either.
func buildImagePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	stretches := []struct {
		caption string
		value   uixaml.Stretch
	}{
		{"None", uixaml.StretchNone},
		{"Fill", uixaml.StretchFill},
		{"Uniform", uixaml.StretchUniform},
		{"UniformToFill", uixaml.StretchUniformToFill},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, mode := range stretches {
		caption, err := label(mode.caption)
		if err != nil {
			return nil, err
		}
		image, err := uixaml.NewImage()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			image.SetStretch(mode.value),
			app.With(image.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return app.All(frame.SetWidth(120), frame.SetHeight(80))
			}),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, image.AsUIElement)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

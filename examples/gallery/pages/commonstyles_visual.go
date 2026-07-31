//go:build windows && amd64

package pages

// The visual-property pages: the matrices that vary one property across every control.
//
// Sources: controls/dev/CommonStyles/TestUI/{CornerRadius,BorderThickness,Compact,
// VisualProperties,VisualStates,CommonStyles}Page.xaml
//
// These are the largest sources in the batch — CornerRadiusPage is 956 lines — and they
// are large because XAML has no loop. The page declares the same control a dozen times
// with one attribute varied. A faithful port is the loop the markup could not write; it
// is the same tree.

import (
	"fmt"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "CornerRadiusPage", Build: buildCornerRadiusPage})
	register(Page{Control: "CommonStyles", Name: "BorderThicknessPage", Build: buildBorderThicknessPage})
	register(Page{Control: "CommonStyles", Name: "CompactPage", Build: buildCompactPage})
	register(Page{Control: "CommonStyles", Name: "VisualPropertiesPage", Build: buildVisualPropertiesPage})
	register(Page{Control: "CommonStyles", Name: "VisualStatesPage", Build: buildVisualStatesPage})
	register(Page{Control: "CommonStyles", Name: "CommonStylesPage", Build: buildCommonStylesPage})
}

// sampleControls builds one of each common control, which several of these pages need
// in order to vary a property across all of them.
func sampleControls() ([]namedElement, error) {
	var out []namedElement

	button, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(button.AsContentControl, "Button"); err != nil {
		return nil, err
	}
	out = append(out, namedElement{"Button", button.AsUIElement})

	check, err := uixaml.NewCheckBox()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(check.AsContentControl, "CheckBox"); err != nil {
		return nil, err
	}
	out = append(out, namedElement{"CheckBox", check.AsUIElement})

	combo, err := newComboBox("ComboBox", []string{"One", "Two"})
	if err != nil {
		return nil, err
	}
	out = append(out, namedElement{"ComboBox", combo.AsUIElement})

	text, err := uixaml.NewTextBox()
	if err != nil {
		return nil, err
	}
	if err := text.SetText("TextBox"); err != nil {
		return nil, err
	}
	out = append(out, namedElement{"TextBox", text.AsUIElement})

	slider, err := uixaml.NewSlider()
	if err != nil {
		return nil, err
	}
	if err := app.With(slider.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetWidth(160)
	}); err != nil {
		return nil, err
	}
	out = append(out, namedElement{"Slider", slider.AsUIElement})

	return out, nil
}

// namedElement pairs a caption with the element it labels.
type namedElement struct {
	name    string
	element func() (*uixaml.IUIElement, error)
}

// CornerRadiusPage: a Border around each sample control, at each corner radius the
// source steps through.
func buildCornerRadiusPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, radius := range []float64{0, 2, 4, 8, 16} {
		caption, err := label(fmt.Sprintf("CornerRadius %.0f", radius))
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement)

		controls, err := sampleControls()
		if err != nil {
			return nil, err
		}
		for _, control := range controls {
			border, err := uixaml.NewBorder()
			if err != nil {
				return nil, err
			}
			brush, err := solidBrush(wrtui.Color{A: 255, R: 120, G: 120, B: 120})
			if err != nil {
				return nil, err
			}
			err = app.All(
				border.SetCornerRadius(uixaml.CornerRadius{
					TopLeft: radius, TopRight: radius, BottomRight: radius, BottomLeft: radius,
				}),
				border.SetBorderThickness(uixaml.Thickness{Left: 1, Top: 1, Right: 1, Bottom: 1}),
				border.SetPadding(uixaml.Thickness{Left: 4, Top: 4, Right: 4, Bottom: 4}),
				border.SetBorderBrush(brush),
				app.With(control.element, func(element *uixaml.IUIElement) error {
					return border.SetChild(element)
				}),
			)
			brush.Release()
			if err != nil {
				return nil, err
			}
			children = append(children, border.AsUIElement)
		}
	}
	panel, err := stack(6, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// BorderThicknessPage: the same shape with the thickness varied, including the
// asymmetric case the source is really testing.
func buildBorderThicknessPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	thicknesses := []struct {
		caption string
		value   uixaml.Thickness
	}{
		{"Uniform 1", uixaml.Thickness{Left: 1, Top: 1, Right: 1, Bottom: 1}},
		{"Uniform 4", uixaml.Thickness{Left: 4, Top: 4, Right: 4, Bottom: 4}},
		{"Left only", uixaml.Thickness{Left: 6}},
		{"Top and bottom", uixaml.Thickness{Top: 4, Bottom: 4}},
		{"Asymmetric", uixaml.Thickness{Left: 1, Top: 2, Right: 4, Bottom: 8}},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, thickness := range thicknesses {
		caption, err := label(thickness.caption)
		if err != nil {
			return nil, err
		}
		border, err := uixaml.NewBorder()
		if err != nil {
			return nil, err
		}
		brush, err := solidBrush(wrtui.Color{A: 255, R: 80, G: 140, B: 200})
		if err != nil {
			return nil, err
		}
		inner, err := label("Bordered content")
		if err != nil {
			brush.Release()
			return nil, err
		}
		err = app.All(
			border.SetBorderThickness(thickness.value),
			border.SetBorderBrush(brush),
			border.SetPadding(uixaml.Thickness{Left: 8, Top: 8, Right: 8, Bottom: 8}),
			app.With(inner.AsUIElement, func(element *uixaml.IUIElement) error {
				return border.SetChild(element)
			}),
		)
		brush.Release()
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, border.AsUIElement)
	}
	panel, err := stack(6, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// CompactPage shows the same controls the compact resource dictionary re-sizes. The
// dictionary itself is XamlControlsResources with UseCompactResources, which throws
// outside a prerelease WinUI — so the page shows the controls at their default metrics
// and says so, rather than pretending to switch.
func buildCompactPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	note, err := label("Sample controls at default metrics. UseCompactResources is " +
		"rejected outside prerelease WinUI, by its own check.")
	if err != nil {
		return nil, err
	}
	controls, err := sampleControls()
	if err != nil {
		return nil, err
	}
	children := []func() (*uixaml.IUIElement, error){note.AsUIElement}
	for _, control := range controls {
		children = append(children, control.element)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// VisualPropertiesPage: Foreground, Background and Opacity varied across a control, the
// three the source drives from combo boxes.
func buildVisualPropertiesPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	colours := []struct {
		name  string
		value wrtui.Color
	}{
		{"Red", wrtui.Color{A: 255, R: 200, G: 40, B: 40}},
		{"Green", wrtui.Color{A: 255, R: 40, G: 160, B: 60}},
		{"Blue", wrtui.Color{A: 255, R: 40, G: 80, B: 200}},
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, colour := range colours {
		for _, opacity := range []float64{1.0, 0.5} {
			block, err := label(fmt.Sprintf("%s at %.0f%% opacity", colour.name, opacity*100))
			if err != nil {
				return nil, err
			}
			brush, err := solidBrush(colour.value)
			if err != nil {
				return nil, err
			}
			err = app.All(
				block.SetForeground(brush),
				app.With(block.AsUIElement, func(element *uixaml.IUIElement) error {
					return element.SetOpacity(opacity)
				}),
			)
			brush.Release()
			if err != nil {
				return nil, err
			}
			children = append(children, block.AsUIElement)
		}
	}
	panel, err := stack(4, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// VisualStatesPage: controls in the states their templates define — enabled, disabled,
// and a toggle in each of its states. IsEnabled is Control's.
func buildVisualStatesPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, enabled := range []bool{true, false} {
		caption, err := label(map[bool]string{true: "Enabled", false: "Disabled"}[enabled])
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement)

		controls, err := sampleControls()
		if err != nil {
			return nil, err
		}
		for _, control := range controls {
			if err := app.With(control.element, func(element *uixaml.IUIElement) error {
				asControl, err := controlOf(element)
				if err != nil {
					// Not every sample is a Control; those simply keep their state.
					return nil
				}
				defer asControl.Release()
				return asControl.SetIsEnabled(enabled)
			}); err != nil {
				return nil, err
			}
			children = append(children, control.element)
		}
	}
	panel, err := stack(6, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// CommonStylesPage is the source's index: one of everything, at default styling. It is
// the page that would catch a styling regression across the whole set at once.
func buildCommonStylesPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	controls, err := sampleControls()
	if err != nil {
		return nil, err
	}
	var children []func() (*uixaml.IUIElement, error)
	for _, control := range controls {
		caption, err := label(control.name)
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, control.element)
	}
	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

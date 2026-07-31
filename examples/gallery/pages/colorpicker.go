//go:build windows && amd64

package pages

// Ported from controls/dev/ColorPicker/TestUI/ColorPickerPage.xaml.
//
// The most involved of the seed pages: a control with a dozen visibility switches and a
// Color property that is a by-value struct from Windows.UI. Included precisely because
// it is awkward — a page that only exercises easy controls proves little.

import (
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "ColorPicker", Name: "ColorPickerPage", Build: buildColorPickerPage})
}

func buildColorPickerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	picker, err := uixaml.NewColorPicker()
	if err != nil {
		return nil, err
	}
	// Color is a by-value Windows.UI.Color: four bytes, so it travels in a register
	// rather than by pointer. Set to the source page's starting green.
	if err := app.All(
		picker.SetColor(wrtui.Color{A: 255, R: 0, G: 128, B: 64}),
		picker.SetIsAlphaEnabled(true),
		picker.SetIsColorSpectrumVisible(true),
		picker.SetIsColorPreviewVisible(true),
		picker.SetIsColorSliderVisible(true),
	); err != nil {
		return nil, err
	}

	// The switches, each toggling one part of the picker off and on. The source uses
	// CheckBoxes bound to these properties; a CheckBox's Checked/Unchecked are the
	// imperative equivalent of that binding.
	type toggle struct {
		caption string
		set     func(bool) error
	}
	toggles := []toggle{
		{"Alpha enabled", picker.SetIsAlphaEnabled},
		{"Spectrum visible", picker.SetIsColorSpectrumVisible},
		{"Preview visible", picker.SetIsColorPreviewVisible},
		{"Slider visible", picker.SetIsColorSliderVisible},
	}

	children := []func() (*uixaml.IUIElement, error){picker.AsUIElement}
	for _, item := range toggles {
		box, err := uixaml.NewCheckBox()
		if err != nil {
			return nil, err
		}
		if err := app.SetContent(box.AsContentControl, item.caption); err != nil {
			return nil, err
		}
		// IsChecked is IReference<Bool>: a CheckBox is tri-state, and nil is the third
		// state. A boxed bool answers IReference<Bool>, so BoxAs does both steps.
		checked, err := app.BoxAs[uixaml.IReferenceOfBool](true, &uixaml.IID_IReferenceOfBool)
		if err != nil {
			return nil, err
		}
		err = app.With(box.AsToggleButton, func(toggleButton *uixaml.IToggleButton) error {
			return toggleButton.SetIsChecked(checked)
		})
		checked.Release()
		if err != nil {
			return nil, err
		}
		set := item.set
		if err := app.With(box.AsToggleButton, func(toggleButton *uixaml.IToggleButton) error {
			if _, err := app.On(toggleButton.AddChecked, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { _ = set(true) }); err != nil {
				return err
			}
			_, err := app.On(toggleButton.AddUnchecked, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { _ = set(false) })
			return err
		}); err != nil {
			return nil, err
		}
		children = append(children, box.AsUIElement)
	}

	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

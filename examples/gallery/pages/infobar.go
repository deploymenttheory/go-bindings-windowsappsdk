//go:build windows && amd64

package pages

// Ported from controls/dev/InfoBar/TestUI/InfoBarPage.xaml.
//
// The source drives one InfoBar from a panel of switches, wiring Title, Message,
// IsIconVisible and IsClosable through x:Bind. A binding is a subscription: when the
// TextBox changes, the InfoBar's property is set. There is no imperative binding here,
// so the port does what the binding does — sets the property from the event — which is
// both the honest translation and shorter than the markup.
//
// The source also declares a Style keyed off InfoBarCloseButtonStyle. That is the one
// piece which has no Go form, and it is what app.LoadMarkup exists for.

import (
	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "InfoBar", Name: "InfoBarPage", Build: buildInfoBarPage})
}

func buildInfoBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	bar, err := uixaml.NewInfoBar()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		bar.SetIsOpen(true),
		bar.SetTitle("Title"),
		bar.SetMessage("Message"),
		bar.SetIsIconVisible(true),
		bar.SetIsClosable(true),
		bar.SetSeverity(uixaml.InfoBarSeverityInformational),
	); err != nil {
		return nil, err
	}

	// The severity switches, which in the source are radio buttons re-setting Severity.
	severities := []struct {
		caption string
		value   uixaml.InfoBarSeverity
	}{
		{"Informational", uixaml.InfoBarSeverityInformational},
		{"Success", uixaml.InfoBarSeveritySuccess},
		{"Warning", uixaml.InfoBarSeverityWarning},
		{"Error", uixaml.InfoBarSeverityError},
	}

	children := []func() (*uixaml.IUIElement, error){bar.AsUIElement}
	for _, severity := range severities {
		button, err := uixaml.NewButton()
		if err != nil {
			return nil, err
		}
		if err := app.SetContent(button.AsContentControl, severity.caption); err != nil {
			return nil, err
		}
		value := severity.value
		if err := app.With(button.AsButtonBase, func(base *uixaml.IButtonBase) error {
			_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
					_ = bar.SetSeverity(value)
					_ = bar.SetIsOpen(true)
				})
			return addErr
		}); err != nil {
			return nil, err
		}
		children = append(children, button.AsUIElement)
	}

	// Reopen, since closing it is the point of IsClosable and there is otherwise no way
	// back. The source uses a Button for this too.
	reopen, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(reopen.AsContentControl, "Reopen"); err != nil {
		return nil, err
	}
	if err := app.With(reopen.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				_ = bar.SetIsOpen(true)
			})
		return addErr
	}); err != nil {
		return nil, err
	}
	children = append(children, reopen.AsUIElement)

	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

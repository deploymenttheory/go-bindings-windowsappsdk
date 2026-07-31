//go:build windows && amd64

package pages

// The command-surface pages: CommandBar, MenuFlyout, AppBar.
//
// Sources: controls/dev/CommonStyles/TestUI/{CommandBar,CommandBarCenter,MenuFlyout,AppBar}Page.xaml
//
// Worth noting what these exercise that the earlier pages do not. Every command surface
// holds its children in a TYPED collection of an interface — CommandBar's
// PrimaryCommands is IObservableVector<ICommandBarElement>, MenuFlyout's Items is
// IVector<MenuFlyoutItemBase> — so each child has to be queried to the element type the
// collection holds. That is the monomorphized-generics work carrying real weight.

import (
	"fmt"
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "CommandBarPage", Build: buildCommandBarPage})
	register(Page{Control: "CommonStyles", Name: "CommandBarCenterPage", Build: buildCommandBarCenterPage})
	register(Page{Control: "CommonStyles", Name: "MenuFlyoutPage", Build: buildMenuFlyoutPage})
	register(Page{Control: "CommonStyles", Name: "AppBarPage", Build: buildAppBarPage})
}

// commandBar builds a CommandBar with the source's mix of buttons, toggles and a
// separator in the primary commands, and two more in the overflow.
func commandBar(alignment uixaml.CommandBarDefaultLabelPosition) (*uixaml.CommandBar, error) {
	bar, err := uixaml.NewCommandBar()
	if err != nil {
		return nil, err
	}
	if err := bar.SetDefaultLabelPosition(alignment); err != nil {
		return nil, err
	}

	primary, err := bar.PrimaryCommands()
	if err != nil {
		return nil, err
	}
	defer primary.Release()

	for _, caption := range []string{"Add", "Edit", "Share"} {
		button, err := uixaml.NewAppBarButton()
		if err != nil {
			return nil, err
		}
		if err := button.SetLabel(caption); err != nil {
			return nil, err
		}
		if err := appendCommand(primary, unsafe.Pointer(button)); err != nil {
			return nil, err
		}
	}
	toggle, err := uixaml.NewAppBarToggleButton()
	if err != nil {
		return nil, err
	}
	if err := toggle.SetLabel("Pin"); err != nil {
		return nil, err
	}
	if err := appendCommand(primary, unsafe.Pointer(toggle)); err != nil {
		return nil, err
	}
	separator, err := uixaml.NewAppBarSeparator()
	if err != nil {
		return nil, err
	}
	if err := appendCommand(primary, unsafe.Pointer(separator)); err != nil {
		return nil, err
	}

	secondary, err := bar.SecondaryCommands()
	if err != nil {
		return nil, err
	}
	defer secondary.Release()
	for _, caption := range []string{"Settings", "About"} {
		button, err := uixaml.NewAppBarButton()
		if err != nil {
			return nil, err
		}
		if err := button.SetLabel(caption); err != nil {
			return nil, err
		}
		if err := appendCommand(secondary, unsafe.Pointer(button)); err != nil {
			return nil, err
		}
	}
	return bar, nil
}

// appendCommand queries a command for ICommandBarElement and appends it.
//
// The collection is typed to the interface, not the class, so every child needs the
// query — which is what the markup's implicit conversion does for you.
func appendCommand(commands *uixaml.IObservableVectorOfICommandBarElement, command unsafe.Pointer) error {
	element, err := winrt.QueryInterface[uixaml.ICommandBarElement](command, &uixaml.IID_ICommandBarElement)
	if err != nil {
		return fmt.Errorf("a command is not an ICommandBarElement: %w", err)
	}
	defer element.Release()
	vector, err := commands.AsVectorOfICommandBarElement()
	if err != nil {
		return err
	}
	defer vector.Release()
	return vector.Append(element)
}

func buildCommandBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	bar, err := commandBar(uixaml.CommandBarDefaultLabelPositionBottom)
	if err != nil {
		return nil, err
	}
	caption, err := label("CommandBar with primary and secondary commands")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, bar.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// CommandBarCenterPage is the same bar with its content centred, which is what the
// source varies.
func buildCommandBarCenterPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	bar, err := commandBar(uixaml.CommandBarDefaultLabelPositionRight)
	if err != nil {
		return nil, err
	}
	if err := app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentCenter)
	}); err != nil {
		return nil, err
	}
	caption, err := label("CommandBar, centred")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, bar.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// MenuFlyoutPage: a Button whose Flyout is a MenuFlyout of items, a toggle item and a
// separator — the shapes the source shows.
func buildMenuFlyoutPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	flyout, err := uixaml.NewMenuFlyout()
	if err != nil {
		return nil, err
	}
	items, err := flyout.Items()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	appendItem := func(item unsafe.Pointer) error {
		base, err := winrt.QueryInterface[uixaml.IMenuFlyoutItemBase](item, &uixaml.IID_IMenuFlyoutItemBase)
		if err != nil {
			return err
		}
		defer base.Release()
		return items.Append(base)
	}

	for _, caption := range []string{"Open", "Save", "Close"} {
		item, err := uixaml.NewMenuFlyoutItem()
		if err != nil {
			return nil, err
		}
		if err := item.SetText(caption); err != nil {
			return nil, err
		}
		if err := appendItem(unsafe.Pointer(item)); err != nil {
			return nil, err
		}
	}
	separator, err := uixaml.NewMenuFlyoutSeparator()
	if err != nil {
		return nil, err
	}
	if err := appendItem(unsafe.Pointer(separator)); err != nil {
		return nil, err
	}
	toggle, err := uixaml.NewToggleMenuFlyoutItem()
	if err != nil {
		return nil, err
	}
	// Text is MenuFlyoutItem's, one class above ToggleMenuFlyoutItem, reached through
	// the generated base-chain accessor. IsChecked is the toggle's own.
	if err := app.All(
		app.With(toggle.AsMenuFlyoutItem, func(item *uixaml.IMenuFlyoutItem) error {
			return item.SetText("Word wrap")
		}),
		toggle.SetIsChecked(true),
	); err != nil {
		return nil, err
	}
	if err := appendItem(unsafe.Pointer(toggle)); err != nil {
		return nil, err
	}

	button, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(button.AsContentControl, "Open the menu"); err != nil {
		return nil, err
	}
	// Flyout is on Button itself — one of the two members IButton declares.
	if err := app.With(flyout.AsFlyoutBase, func(base *uixaml.IFlyoutBase) error {
		return button.SetFlyout(base)
	}); err != nil {
		return nil, err
	}

	caption, err := label("Button with a MenuFlyout")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, button.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// AppBarPage: a bare AppBar, which the source uses to check the style applies without
// a CommandBar around it.
func buildAppBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	bar, err := uixaml.NewAppBar()
	if err != nil {
		return nil, err
	}
	content, err := label("AppBar content")
	if err != nil {
		return nil, err
	}
	if err := app.With(content.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(bar.AsContentControl, func(host *uixaml.IContentControl) error {
			return host.SetContent(&element.IInspectable)
		})
	}); err != nil {
		return nil, err
	}
	caption, err := label("A bare AppBar")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, bar.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

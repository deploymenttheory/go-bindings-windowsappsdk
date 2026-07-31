//go:build windows && amd64

package pages

// The command and flyout controls.
//
// Sources: controls/dev/{CommandBarFlyout,SplitButton,DropDownButton,RadioMenuFlyoutItem,
// NumberBox,MenuBar,SwipeControl}/TestUI/*Page.xaml
//
// What these have in common is that the control is a way of ASKING for something rather
// than a way of showing it: a flyout of commands, a button that both acts and offers
// alternatives, a swipe that reveals actions. So most of them are mostly event wiring, and
// the interesting part of each port is which object owns the command.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "CommandBarFlyout", Name: "CommandBarFlyoutPage", Build: buildCommandBarFlyoutPage})
	register(Page{Control: "CommandBarFlyout", Name: "CommandBarFlyoutMainPage", Build: buildCommandBarFlyoutMainPage})
	register(Page{Control: "CommandBarFlyout", Name: "ExtraCommandBarFlyoutPage", Build: buildExtraCommandBarFlyoutPage})
	register(Page{Control: "CommandBarFlyout", Name: "TextCommandBarFlyoutPage", Build: buildTextCommandBarFlyoutPage})

	register(Page{Control: "SplitButton", Name: "SplitButtonPage", Build: buildSplitButtonPage})
	register(Page{Control: "DropDownButton", Name: "DropDownButtonPage", Build: buildDropDownButtonPage})
	register(Page{Control: "RadioMenuFlyoutItem", Name: "RadioMenuFlyoutItemPage", Build: buildRadioMenuFlyoutItemPage})

	register(Page{Control: "NumberBox", Name: "NumberBoxPage", Build: buildNumberBoxPage})
	register(Page{Control: "NumberBox", Name: "NumberBoxAxeTestPage", Build: buildNumberBoxAxeTestPage})

	register(Page{Control: "MenuBar", Name: "MenuBarPage", Build: buildMenuBarPage})

	register(Page{Control: "SwipeControl", Name: "SwipeControlPage", Build: buildSwipeControlPage})
	register(Page{Control: "SwipeControl", Name: "SwipeControlClearPage", Build: buildSwipeControlClearPage})
	register(Page{Control: "SwipeControl", Name: "SwipePage", Build: buildSwipePage})
}

// appBarButton builds one command for a bar or flyout.
func appBarButton(glyph, text string, onClick func()) (*uixaml.AppBarButton, error) {
	control, err := uixaml.NewAppBarButton()
	if err != nil {
		return nil, err
	}
	if err := control.SetLabel(text); err != nil {
		return nil, err
	}
	if glyph != "" {
		icon, err := uixaml.NewFontIcon()
		if err != nil {
			return nil, err
		}
		defer icon.Release()
		if err := icon.SetGlyph(glyph); err != nil {
			return nil, err
		}
		element, err := icon.AsIconElement()
		if err != nil {
			return nil, err
		}
		defer element.Release()
		if err := control.SetIcon(element); err != nil {
			return nil, err
		}
	}
	if onClick != nil {
		if err := app.With(control.AsButtonBase, func(base *uixaml.IButtonBase) error {
			_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { onClick() })
			return addErr
		}); err != nil {
			return nil, err
		}
	}
	return control, nil
}

// appendCommand puts an AppBarButton into a CommandBarFlyout's command collection.
//
// PrimaryCommands and SecondaryCommands are IObservableVector<ICommandBarElement>, so the
// mutation methods come from the IVector it Requires — the same accessor route
// ItemsControl.Items and GradientStops take. And the element goes in as
// ICommandBarElement, the interface every command in a bar implements, rather than as the
// button's own type.
func appendBarCommand(commands *uixaml.IObservableVectorOfICommandBarElement,
	control *uixaml.AppBarButton,
) error {
	vector, err := commands.AsVectorOfICommandBarElement()
	if err != nil {
		return err
	}
	defer vector.Release()
	element, err := winrt.QueryInterface[uixaml.ICommandBarElement](
		unsafe.Pointer(control), &uixaml.IID_ICommandBarElement)
	if err != nil {
		return fmt.Errorf("an AppBarButton should be an ICommandBarElement: %w", err)
	}
	defer element.Release()
	return vector.Append(element)
}

// newCommandBarFlyout builds a flyout with primary and secondary commands.
//
// The split is what the control is for: primary commands are the row of icons, secondary
// are the labelled menu below the "see more" chevron, and the same button type goes in
// both.
func newCommandBarFlyout(status *uixaml.TextBlock) (*uixaml.CommandBarFlyout, error) {
	flyout, err := uixaml.NewCommandBarFlyout()
	if err != nil {
		return nil, err
	}

	primary, err := flyout.PrimaryCommands()
	if err != nil {
		return nil, err
	}
	defer primary.Release()
	for _, entry := range []struct{ glyph, text string }{
		{"", "Cut"}, {"", "Copy"}, {"", "Paste"}, {"", "Delete"},
	} {
		text := entry.text
		control, err := appBarButton(entry.glyph, text, func() {
			_ = status.SetText("Primary command: " + text)
		})
		if err != nil {
			return nil, err
		}
		err = appendBarCommand(primary, control)
		control.Release()
		if err != nil {
			return nil, err
		}
	}

	secondary, err := flyout.SecondaryCommands()
	if err != nil {
		return nil, err
	}
	defer secondary.Release()
	for _, text := range []string{"Select all", "Undo", "Redo"} {
		label := text
		control, err := appBarButton("", label, func() {
			_ = status.SetText("Secondary command: " + label)
		})
		if err != nil {
			return nil, err
		}
		err = appendBarCommand(secondary, control)
		control.Release()
		if err != nil {
			return nil, err
		}
	}
	return flyout, nil
}

// showFlyoutAt opens a flyout over an element.
func showFlyoutAt(flyout *uixaml.IFlyoutBase, frame *uixaml.IFrameworkElement) error {
	return flyout.ShowAt(frame)
}

func buildCommandBarFlyoutPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("No command invoked yet.")
	if err != nil {
		return nil, err
	}
	flyout, err := newCommandBarFlyout(status)
	if err != nil {
		return nil, err
	}
	base, err := flyout.AsFlyoutBase()
	if err != nil {
		return nil, err
	}
	defer base.Release()

	target, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(target.AsContentControl, "Show the command bar flyout"); err != nil {
		return nil, err
	}
	frame, err := target.AsFrameworkElement()
	if err != nil {
		return nil, err
	}
	defer frame.Release()
	if err := app.With(target.AsButtonBase, func(control *uixaml.IButtonBase) error {
		_, addErr := app.On(control.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				if err := showFlyoutAt(base, frame); err != nil {
					_ = status.SetText("ShowAt failed: " + err.Error())
				}
			})
		return addErr
	}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, target.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// CommandBarFlyoutMainPage: the flyout attached as a context menu rather than shown by a
// button, which is the way it is usually met.
func buildCommandBarFlyoutMainPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Right-click the block below.")
	if err != nil {
		return nil, err
	}
	flyout, err := newCommandBarFlyout(status)
	if err != nil {
		return nil, err
	}
	base, err := flyout.AsFlyoutBase()
	if err != nil {
		return nil, err
	}
	defer base.Release()

	band, err := colouredBand(360, 160, "Right-click me", bandColours[2])
	if err != nil {
		return nil, err
	}
	// ContextFlyout is on FrameworkElement, so any element can carry one.
	// ContextFlyout is on IUIElement, not IFrameworkElement — a context menu belongs to
	// anything that can be pointed at, which is a UIElement.
	if err := app.With(band.AsUIElement, func(element *uixaml.IUIElement) error {
		return element.SetContextFlyout(base)
	}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, band.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ExtraCommandBarFlyoutPage: several flyouts at once, which is how the source checks that
// opening one closes the others.
func buildExtraCommandBarFlyoutPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Each block has its own flyout.")
	if err != nil {
		return nil, err
	}

	children := []func() (*uixaml.IUIElement, error){status.AsUIElement}
	for i := 0; i < 3; i++ {
		flyout, err := newCommandBarFlyout(status)
		if err != nil {
			return nil, err
		}
		base, err := flyout.AsFlyoutBase()
		if err != nil {
			return nil, err
		}
		band, err := colouredBand(320, 90, fmt.Sprintf("Block %d", i+1),
			bandColours[i%len(bandColours)])
		if err != nil {
			base.Release()
			return nil, err
		}
		err = app.With(band.AsUIElement, func(element *uixaml.IUIElement) error {
			return element.SetContextFlyout(base)
		})
		base.Release()
		if err != nil {
			return nil, err
		}
		children = append(children, band.AsUIElement)
	}

	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TextCommandBarFlyoutPage: the flyout a text control puts up by itself.
//
// TextCommandBarFlyout is what a TextBox uses for its selection menu — cut, copy, paste,
// bold — and the control creates one without being asked. So the page shows both: a text
// box with its own, and one built directly to see what it contains.
func buildTextCommandBarFlyoutPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	box, err := uixaml.NewTextBox()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		box.SetText("Select this text and right-click it."),
		app.With(box.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetWidth(360)
		}),
	); err != nil {
		return nil, err
	}

	standalone, err := uixaml.NewTextCommandBarFlyout()
	if err != nil {
		return nil, err
	}
	// PrimaryCommands is CommandBarFlyout%s, which TextCommandBarFlyout derives from." % "'s"
	shared, err := standalone.AsCommandBarFlyout()
	if err != nil {
		return nil, err
	}
	defer shared.Release()
	primary, err := shared.PrimaryCommands()
	if err != nil {
		return nil, err
	}
	defer primary.Release()
	// Size is on the IVector it Requires, not on the observable view itself.
	countable, err := primary.AsVectorOfICommandBarElement()
	if err != nil {
		return nil, err
	}
	defer countable.Release()
	count, err := countable.Size()
	if err != nil {
		return nil, err
	}

	note, err := label(fmt.Sprintf(
		"A TextBox creates its own TextCommandBarFlyout for the selection menu.\n\n"+
			"One built directly reports %d primary commands before being attached to "+
			"anything — the control fills it in from the text state when it opens.", count))
	if err != nil {
		return nil, err
	}

	panel, err := stack(10, note.AsUIElement, box.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// menuFlyoutWith builds a MenuFlyout of plain items.
func menuFlyoutWith(status *uixaml.TextBlock, items ...string) (*uixaml.MenuFlyout, error) {
	flyout, err := uixaml.NewMenuFlyout()
	if err != nil {
		return nil, err
	}
	collection, err := flyout.Items()
	if err != nil {
		return nil, err
	}
	defer collection.Release()

	for _, text := range items {
		label := text
		item, err := uixaml.NewMenuFlyoutItem()
		if err != nil {
			return nil, err
		}
		if err := item.SetText(label); err != nil {
			item.Release()
			return nil, err
		}
		if _, err := app.On(item.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				_ = status.SetText("Chose: " + label)
			}); err != nil {
			item.Release()
			return nil, err
		}
		base, err := item.AsMenuFlyoutItemBase()
		item.Release()
		if err != nil {
			return nil, err
		}
		err = collection.Append(base)
		base.Release()
		if err != nil {
			return nil, err
		}
	}
	return flyout, nil
}

// SplitButtonPage: the button that both acts and offers alternatives.
//
// SplitButton has its OWN Click event, separate from ButtonBase's: pressing the left half
// invokes it, pressing the chevron opens the flyout instead. That is the distinction the
// control exists for, and the reason its Click carries SplitButtonClickEventArgs.
func buildSplitButtonPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Press the left half, or the chevron.")
	if err != nil {
		return nil, err
	}

	control, err := uixaml.NewSplitButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(control.AsContentControl, "Paste"); err != nil {
		return nil, err
	}
	flyout, err := menuFlyoutWith(status, "Paste as plain text", "Paste and match style")
	if err != nil {
		return nil, err
	}
	base, err := flyout.AsFlyoutBase()
	if err != nil {
		return nil, err
	}
	defer base.Release()
	if err := control.SetFlyout(base); err != nil {
		return nil, err
	}
	if _, err := app.On(control.AddClick,
		uixaml.NewTypedEventHandlerOfSplitButtonAndSplitButtonClickEventArgs,
		func(_ *uixaml.ISplitButton, _ *uixaml.ISplitButtonClickEventArgs) {
			_ = status.SetText("The button half was pressed.")
		}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// DropDownButtonPage: the button that is only a menu.
//
// DropDownButton derives from Button and adds nothing but the chevron — its whole job is
// to say "this opens something", so the flyout is the entire behaviour.
func buildDropDownButtonPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Open the menu.")
	if err != nil {
		return nil, err
	}
	control, err := uixaml.NewDropDownButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(control.AsContentControl, "Sort by"); err != nil {
		return nil, err
	}
	flyout, err := menuFlyoutWith(status, "Name", "Date modified", "Size")
	if err != nil {
		return nil, err
	}
	base, err := flyout.AsFlyoutBase()
	if err != nil {
		return nil, err
	}
	defer base.Release()
	if err := app.With(control.AsButton, func(button *uixaml.IButton) error {
		return button.SetFlyout(base)
	}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// RadioMenuFlyoutItemPage: menu items that behave as a radio group.
//
// GroupName is what makes them exclusive, and it is a plain string rather than a parent
// object — so items in different flyouts can share a group, and items in the same flyout
// with different names are independent. The page shows both.
func buildRadioMenuFlyoutItemPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Nothing chosen yet.")
	if err != nil {
		return nil, err
	}

	flyout, err := uixaml.NewMenuFlyout()
	if err != nil {
		return nil, err
	}
	items, err := flyout.Items()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	groups := []struct {
		group   string
		options []string
	}{
		{"view", []string{"Small icons", "Medium icons", "Large icons"}},
		{"sort", []string{"By name", "By date"}},
	}
	for index, entry := range groups {
		for optionIndex, option := range entry.options {
			text, group := option, entry.group
			item, err := uixaml.NewRadioMenuFlyoutItem()
			if err != nil {
				return nil, err
			}
			if err := app.All(
				item.SetGroupName(group),
				item.SetIsChecked(optionIndex == 0),
				app.With(item.AsMenuFlyoutItem, func(base *uixaml.IMenuFlyoutItem) error {
					return base.SetText(text)
				}),
			); err != nil {
				item.Release()
				return nil, err
			}
			if err := app.With(item.AsMenuFlyoutItem, func(base *uixaml.IMenuFlyoutItem) error {
				_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
					func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
						_ = status.SetText(fmt.Sprintf("Group %q: %s", group, text))
					})
				return addErr
			}); err != nil {
				item.Release()
				return nil, err
			}
			base, err := item.AsMenuFlyoutItemBase()
			item.Release()
			if err != nil {
				return nil, err
			}
			err = items.Append(base)
			base.Release()
			if err != nil {
				return nil, err
			}
		}
		// A separator between the two groups.
		if index == 0 {
			separator, err := uixaml.NewMenuFlyoutSeparator()
			if err != nil {
				return nil, err
			}
			base, err := separator.AsMenuFlyoutItemBase()
			separator.Release()
			if err != nil {
				return nil, err
			}
			err = items.Append(base)
			base.Release()
			if err != nil {
				return nil, err
			}
		}
	}

	control, err := uixaml.NewDropDownButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(control.AsContentControl, "View options"); err != nil {
		return nil, err
	}
	flyoutBase, err := flyout.AsFlyoutBase()
	if err != nil {
		return nil, err
	}
	defer flyoutBase.Release()
	if err := app.With(control.AsButton, func(button *uixaml.IButton) error {
		return button.SetFlyout(flyoutBase)
	}); err != nil {
		return nil, err
	}

	note, err := label("Two independent radio groups in one flyout. GroupName is a " +
		"string, not a parent — which is why these two do not interfere.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, status.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// NumberBoxPage: the spin modes, the validation modes, and the expression evaluator.
//
// AcceptsExpression is the one worth showing: with it on, typing "2*8+4" and leaving the
// box evaluates to 20. That is a real feature rather than a curiosity, and it is off by
// default.
func buildNumberBoxPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Change a value.")
	if err != nil {
		return nil, err
	}

	cases := []struct {
		caption    string
		spin       uixaml.NumberBoxSpinButtonPlacementMode
		validation uixaml.NumberBoxValidationMode
		expression bool
	}{
		{"Spin buttons inline", uixaml.NumberBoxSpinButtonPlacementModeInline,
			uixaml.NumberBoxValidationModeInvalidInputOverwritten, false},
		{"Spin buttons compact", uixaml.NumberBoxSpinButtonPlacementModeCompact,
			uixaml.NumberBoxValidationModeInvalidInputOverwritten, false},
		{"No spin buttons, validation disabled", uixaml.NumberBoxSpinButtonPlacementModeHidden,
			uixaml.NumberBoxValidationModeDisabled, false},
		{"AcceptsExpression: try 2*8+4", uixaml.NumberBoxSpinButtonPlacementModeInline,
			uixaml.NumberBoxValidationModeInvalidInputOverwritten, true},
	}

	children := []func() (*uixaml.IUIElement, error){status.AsUIElement}
	for _, entry := range cases {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		box, err := uixaml.NewNumberBox()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			box.SetMinimum(0),
			box.SetMaximum(100),
			box.SetValue(10),
			box.SetSmallChange(1),
			box.SetLargeChange(10),
			box.SetSpinButtonPlacementMode(entry.spin),
			box.SetValidationMode(entry.validation),
			box.SetAcceptsExpression(entry.expression),
			app.With(box.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return frame.SetWidth(220)
			}),
		); err != nil {
			return nil, err
		}
		if _, err := app.On(box.AddValueChanged,
			uixaml.NewTypedEventHandlerOfNumberBoxAndNumberBoxValueChangedEventArgs,
			func(_ *uixaml.INumberBox, args *uixaml.INumberBoxValueChangedEventArgs) {
				value, err := args.NewValue()
				if err != nil {
					return
				}
				_ = status.SetText(fmt.Sprintf("New value: %v", value))
			}); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, box.AsUIElement)
	}

	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func buildNumberBoxAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	box, err := uixaml.NewNumberBox()
	if err != nil {
		return nil, err
	}
	header, err := app.Box("Quantity")
	if err != nil {
		return nil, err
	}
	defer header.Release()
	if err := app.All(
		box.SetHeader(header),
		box.SetPlaceholderText("Enter a number"),
		box.SetMinimum(0),
		box.SetMaximum(10),
		box.SetValue(1),
		box.SetSpinButtonPlacementMode(uixaml.NumberBoxSpinButtonPlacementModeInline),
		app.With(box.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetWidth(240)
		}),
	); err != nil {
		return nil, err
	}
	panel, err := stack(8, box.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// MenuBarPage: the classic application menu.
//
// MenuBar.Items is a vector of MenuBarItem and each item's Items is a vector of
// MenuFlyoutItemBase — so the bar, its menus and their entries are three different
// collections of three different types. That is why building one in Go is more verbose
// than the markup, and it is also why the types cannot be mixed up by accident.
func buildMenuBarPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Choose something from the menu.")
	if err != nil {
		return nil, err
	}

	bar, err := uixaml.NewMenuBar()
	if err != nil {
		return nil, err
	}
	items, err := bar.Items()
	if err != nil {
		return nil, err
	}
	defer items.Release()

	menus := []struct {
		title   string
		entries []string
	}{
		{"File", []string{"New", "Open", "Save", "Exit"}},
		{"Edit", []string{"Undo", "Cut", "Copy", "Paste"}},
		{"View", []string{"Zoom in", "Zoom out", "Full screen"}},
	}
	for _, menu := range menus {
		item, err := uixaml.NewMenuBarItem()
		if err != nil {
			return nil, err
		}
		if err := item.SetTitle(menu.title); err != nil {
			item.Release()
			return nil, err
		}
		entries, err := item.Items()
		if err != nil {
			item.Release()
			return nil, err
		}
		for _, text := range menu.entries {
			title, entry := menu.title, text
			flyoutItem, err := uixaml.NewMenuFlyoutItem()
			if err != nil {
				entries.Release()
				item.Release()
				return nil, err
			}
			if err := flyoutItem.SetText(entry); err != nil {
				flyoutItem.Release()
				entries.Release()
				item.Release()
				return nil, err
			}
			if _, err := app.On(flyoutItem.AddClick, uixaml.NewRoutedEventHandler,
				func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
					_ = status.SetText(title + " > " + entry)
				}); err != nil {
				flyoutItem.Release()
				entries.Release()
				item.Release()
				return nil, err
			}
			base, err := flyoutItem.AsMenuFlyoutItemBase()
			flyoutItem.Release()
			if err != nil {
				entries.Release()
				item.Release()
				return nil, err
			}
			err = entries.Append(base)
			base.Release()
			if err != nil {
				entries.Release()
				item.Release()
				return nil, err
			}
		}
		entries.Release()

		bound, err := item.AsMenuBarItem()
		item.Release()
		if err != nil {
			return nil, err
		}
		err = items.Append(bound)
		bound.Release()
		if err != nil {
			return nil, err
		}
	}

	panel, err := stack(10, bar.AsUIElement, status.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// newSwipeItems builds a SwipeItems collection in the given mode.
//
// Mode is the distinction that matters: Reveal shows the items and waits to be tapped,
// Execute runs the single item as soon as the swipe passes a threshold. An Execute
// collection with more than one item is a mistake the control will not report.
func newSwipeItems(mode uixaml.SwipeMode, status *uixaml.TextBlock,
	entries ...struct{ glyph, text string },
) (*uixaml.SwipeItems, error) {
	items, err := uixaml.NewSwipeItems()
	if err != nil {
		return nil, err
	}
	if err := items.SetMode(mode); err != nil {
		return nil, err
	}
	vector, err := items.AsVectorOfSwipeItem()
	if err != nil {
		return nil, err
	}
	defer vector.Release()

	for _, entry := range entries {
		text := entry.text
		item, err := uixaml.NewSwipeItem()
		if err != nil {
			return nil, err
		}
		if err := item.SetText(text); err != nil {
			item.Release()
			return nil, err
		}
		if entry.glyph != "" {
			icon, err := uixaml.NewSymbolIconSource()
			if err == nil {
				source, sourceErr := icon.AsIconSource()
				if sourceErr == nil {
					_ = item.SetIconSource(source)
					source.Release()
				}
				icon.Release()
			}
		}
		if _, err := app.On(item.AddInvoked,
			uixaml.NewTypedEventHandlerOfSwipeItemAndSwipeItemInvokedEventArgs,
			func(_ *uixaml.ISwipeItem, _ *uixaml.ISwipeItemInvokedEventArgs) {
				_ = status.SetText("Invoked: " + text)
			}); err != nil {
			item.Release()
			return nil, err
		}
		bound, err := item.AsSwipeItem()
		item.Release()
		if err != nil {
			return nil, err
		}
		err = vector.Append(bound)
		bound.Release()
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// newSwipeControl wraps content in a SwipeControl with items on the given side.
func newSwipeControl(content string, status *uixaml.TextBlock,
	left *uixaml.SwipeItems, right *uixaml.SwipeItems,
) (*uixaml.SwipeControl, error) {
	control, err := uixaml.NewSwipeControl()
	if err != nil {
		return nil, err
	}
	band, err := colouredBand(360, 72, content, bandColours[1])
	if err != nil {
		return nil, err
	}
	if err := app.With(band.AsUIElement, func(element *uixaml.IUIElement) error {
		return app.With(control.AsContentControl, func(host *uixaml.IContentControl) error {
			return host.SetContent(inspectableOf(element))
		})
	}); err != nil {
		return nil, err
	}
	if left != nil {
		bound, err := left.AsSwipeItems()
		if err != nil {
			return nil, err
		}
		err = control.SetLeftItems(bound)
		bound.Release()
		if err != nil {
			return nil, err
		}
	}
	if right != nil {
		bound, err := right.AsSwipeItems()
		if err != nil {
			return nil, err
		}
		err = control.SetRightItems(bound)
		bound.Release()
		if err != nil {
			return nil, err
		}
	}
	return control, nil
}

func buildSwipeControlPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Swipe a row sideways.")
	if err != nil {
		return nil, err
	}

	reveal, err := newSwipeItems(uixaml.SwipeModeReveal, status,
		struct{ glyph, text string }{"", "Pin"},
		struct{ glyph, text string }{"", "Archive"})
	if err != nil {
		return nil, err
	}
	execute, err := newSwipeItems(uixaml.SwipeModeExecute, status,
		struct{ glyph, text string }{"", "Delete"})
	if err != nil {
		return nil, err
	}

	control, err := newSwipeControl("Swipe left or right", status, reveal, execute)
	if err != nil {
		return nil, err
	}

	note, err := label("Left items are Reveal mode — they wait to be tapped. Right items " +
		"are Execute mode — one item, run as soon as the swipe passes its threshold.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, status.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// SwipeControlClearPage: the items removed after the control has them, which is what the
// source checks does not leave the control in a swiped state.
func buildSwipeControlClearPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Swipe, then clear the items.")
	if err != nil {
		return nil, err
	}
	items, err := newSwipeItems(uixaml.SwipeModeReveal, status,
		struct{ glyph, text string }{"", "Pin"},
		struct{ glyph, text string }{"", "Archive"})
	if err != nil {
		return nil, err
	}
	control, err := newSwipeControl("Swipe me, then clear", status, items, nil)
	if err != nil {
		return nil, err
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Clear the left items", func() {
				// Close first: clearing while open leaves the content displaced.
				if err := control.Close(); err != nil {
					_ = status.SetText("Close failed: " + err.Error())
					return
				}
				if err := control.SetLeftItems(nil); err != nil {
					_ = status.SetText("Clearing failed: " + err.Error())
					return
				}
				_ = status.SetText("Left items cleared; swiping now does nothing.")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Restore them", func() {
				restored, err := newSwipeItems(uixaml.SwipeModeReveal, status,
					struct{ glyph, text string }{"", "Pin"},
					struct{ glyph, text string }{"", "Archive"})
				if err != nil {
					return
				}
				bound, err := restored.AsSwipeItems()
				if err != nil {
					return
				}
				err = control.SetLeftItems(bound)
				bound.Release()
				if err != nil {
					return
				}
				_ = status.SetText("Left items restored.")
			})
		},
	)
	if err != nil {
		return nil, err
	}

	panel, err := stack(10, status.AsUIElement, row.AsUIElement, control.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// SwipePage: a list of swipeable rows, which is what the control is actually for.
func buildSwipePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("Swipe any row.")
	if err != nil {
		return nil, err
	}

	children := []func() (*uixaml.IUIElement, error){status.AsUIElement}
	for i := 1; i <= 6; i++ {
		reveal, err := newSwipeItems(uixaml.SwipeModeReveal, status,
			struct{ glyph, text string }{"", fmt.Sprintf("Pin %d", i)})
		if err != nil {
			return nil, err
		}
		execute, err := newSwipeItems(uixaml.SwipeModeExecute, status,
			struct{ glyph, text string }{"", fmt.Sprintf("Delete %d", i)})
		if err != nil {
			return nil, err
		}
		control, err := newSwipeControl(fmt.Sprintf("Message %d", i), status, reveal, execute)
		if err != nil {
			return nil, err
		}
		children = append(children, control.AsUIElement)
	}

	panel, err := stack(6, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

var _ = wrtui.Color{}

//go:build windows && amd64

package pages

// The status and information controls.
//
// Sources: controls/dev/{ProgressRing,ProgressBar,InfoBar,InfoBadge,TeachingTip,
// RatingControl}/TestUI/*Page.xaml
//
// ProgressBarPage and InfoBarPage were registered in the harness phase, as two of the
// five pages the conformance suite was first proved on. Everything else in this family
// is here.

import (
	"fmt"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

func init() {
	register(Page{Control: "ProgressRing", Name: "ProgressRingPage", Build: buildProgressRingPage})
	register(Page{Control: "ProgressRing", Name: "ProgressRingAxeTestPage", Build: buildProgressRingAxeTestPage})
	register(Page{Control: "ProgressRing", Name: "ProgressRingStoryboardAnimationPage", Build: buildProgressRingStoryboardAnimationPage})
	register(Page{Control: "ProgressRing", Name: "ProgressRingCustomLottieSourcePage", Build: buildProgressRingCustomLottieSourcePage})

	register(Page{Control: "ProgressBar", Name: "ProgressBarAxeTestPage", Build: buildProgressBarAxeTestPage})
	register(Page{Control: "ProgressBar", Name: "ProgressBarReTemplatePage", Build: buildProgressBarReTemplatePage})

	register(Page{Control: "InfoBadge", Name: "InfoBadgePage", Build: buildInfoBadgePage})

	register(Page{Control: "TeachingTip", Name: "TeachingTipPage", Build: buildTeachingTipPage})
	register(Page{Control: "TeachingTip", Name: "TeachingTipInXamlPage", Build: buildTeachingTipInXamlPage})
	register(Page{Control: "TeachingTip", Name: "TeachingTipFocusPage", Build: buildTeachingTipFocusPage})

	register(Page{Control: "RatingControl", Name: "RatingControlPage", Build: buildRatingControlPage})
}

// ProgressRingPage: the ring determinate and indeterminate.
//
// IsIndeterminate and IsActive are separate properties and both matter: a ring that is
// determinate shows Value, one that is not spins, and one that is not active shows
// nothing at all.
func buildProgressRingPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	rings := []struct {
		caption       string
		indeterminate bool
		value         float64
	}{
		{"Indeterminate", true, 0},
		{"Determinate at 25", false, 25},
		{"Determinate at 75", false, 75},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range rings {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		ring, err := uixaml.NewProgressRing()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			ring.SetIsActive(true),
			ring.SetIsIndeterminate(entry.indeterminate),
			ring.SetValue(entry.value),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, ring.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildProgressRingAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	ring, err := uixaml.NewProgressRing()
	if err != nil {
		return nil, err
	}
	if err := app.All(ring.SetIsActive(true), ring.SetIsIndeterminate(true)); err != nil {
		return nil, err
	}
	caption, err := label("An active, indeterminate ProgressRing")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, ring.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ProgressRingStoryboardAnimationPage: the ring's Value driven by a Storyboard.
//
// This is the first Storyboard in the gallery, and it is worth being precise about how it
// is targeted. Storyboard.SetTarget takes the OBJECT and Storyboard.SetTargetProperty
// takes a PATH STRING — the animation does not hold a typed reference to the property, it
// names it. That is why the property name has to be spelled exactly as the metadata does.
// boxedDouble returns a double as the IReference<Double> XAML's property system accepts.
//
// PropertyValue.CreateDouble produces an object implementing BOTH IPropertyValue and
// IReference<Double>, and the dependency-property system reads it through the first. That
// is why this is a Box and a query rather than app.NewReference, which builds an object
// implementing only IReference and is refused here.
func boxedDouble(value float64) (*uixaml.IReferenceOfDouble, error) {
	boxed, err := app.Box(value)
	if err != nil {
		return nil, err
	}
	defer boxed.Release()
	return winrt.QueryInterface[uixaml.IReferenceOfDouble](
		unsafe.Pointer(boxed), &uixaml.IID_IReferenceOfDouble)
}

// timelineDuration is SetDuration, named so a failure says which call it was.
func timelineDuration(timeline *uixaml.ITimeline, duration uixaml.Duration) error {
	return timeline.SetDuration(duration)
}

func buildProgressRingStoryboardAnimationPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	ring, err := uixaml.NewProgressRing()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		ring.SetIsActive(true),
		ring.SetIsIndeterminate(false),
		ring.SetMinimum(0),
		ring.SetMaximum(100),
	); err != nil {
		return nil, fmt.Errorf("ring setup: %w", err)
	}

	animation, err := uixaml.NewDoubleAnimation()
	if err != nil {
		return nil, err
	}
	defer animation.Release()
	// From and To are IReference<Double>, and the route matters — see the note in
	// app/reference.go. A double CAN be boxed by PropertyValue, so it is: the boxed
	// value is an IReference<Double> as well as an IPropertyValue, and XAML's property
	// system needs the second. A Go-implemented IReference is refused here with E_FAIL.
	from, err := boxedDouble(0)
	if err != nil {
		return nil, err
	}
	defer from.Release()
	to, err := boxedDouble(100)
	if err != nil {
		return nil, err
	}
	defer to.Release()
	if err := app.All(animation.SetFrom(from), animation.SetTo(to)); err != nil {
		return nil, fmt.Errorf("From/To: %w", err)
	}

	storyboard, err := uixaml.NewStoryboard()
	if err != nil {
		return nil, fmt.Errorf("NewStoryboard: %w", err)
	}
	statics, err := uixaml.StoryboardStatics()
	if err != nil {
		return nil, fmt.Errorf("StoryboardStatics: %w", err)
	}
	defer statics.Release()

	timeline, err := animation.AsTimeline()
	if err != nil {
		return nil, fmt.Errorf("AsTimeline: %w", err)
	}
	defer timeline.Release()
	// Duration and AutoReverse are ITimeline's, not the animation's own — every
	// animation type shares them, so they live on the base rather than being repeated.
	if err := app.All(
		timelineDuration(timeline, uixaml.Duration{
			Type:     uixaml.DurationTypeTimeSpan,
			TimeSpan: wrtfoundation.TimeSpan{Duration: 3 * 10_000_000}, // 3s in 100ns ticks
		}),
		timeline.SetAutoReverse(true),
	); err != nil {
		return nil, fmt.Errorf("Duration/AutoReverse: %w", err)
	}
	if err := app.With(ring.AsDependencyObject, func(target *uixaml.IDependencyObject) error {
		return statics.SetTarget(timeline, target)
	}); err != nil {
		return nil, fmt.Errorf("SetTarget: %w", err)
	}
	if err := statics.SetTargetProperty(timeline, "Value"); err != nil {
		return nil, fmt.Errorf("SetTargetProperty: %w", err)
	}

	// The storyboard owns the timeline; Children is where it goes.
	children, err := storyboard.Children()
	if err != nil {
		return nil, fmt.Errorf("Storyboard.Children: %w", err)
	}
	defer children.Release()
	if err := children.Append(timeline); err != nil {
		return nil, fmt.Errorf("Storyboard.Children.Append: %w", err)
	}
	// RepeatBehavior is on the storyboard's own Timeline facet.
	if err := app.With(storyboard.AsTimeline, func(self *uixaml.ITimeline) error {
		return self.SetRepeatBehavior(uixaml.RepeatBehavior{
			Type: uixaml.RepeatBehaviorTypeForever,
		})
	}); err != nil {
		return nil, fmt.Errorf("SetRepeatBehavior: %w", err)
	}

	status, err := label("The ring's Value is animated from 0 to 100 and back, forever.")
	if err != nil {
		return nil, err
	}
	// Begun on Loaded: a storyboard targeting an element outside the tree has nothing
	// to animate.
	if err := app.With(ring.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				if err := storyboard.Begin(); err != nil {
					_ = status.SetText("Begin failed: " + err.Error())
				}
			})
		return addErr
	}); err != nil {
		return nil, fmt.Errorf("AddLoaded: %w", err)
	}

	panel, err := stack(8, status.AsUIElement, ring.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ProgressRingCustomLottieSourcePage: the ring showing an animation other than its own.
//
// ProgressRing has no Source property — its template holds an AnimatedVisualPlayer, and
// the source page swaps in a template whose player carries a different animation. So this
// is a retemplate, and the animation comes from one of the eight sources the SDK ships in
// Microsoft.UI.Xaml.Controls.AnimatedVisuals.
//
// An earlier version of this page claimed the animation was out of reach because the
// Lottie codegen tool emits a generated class per animation and there is no such generator
// for Go. The first half is true; the conclusion was wrong, because those generated
// classes are already IN the SDK as ordinary activatable runtime classes. The same
// mistake was in AnimatedIconPage and AnimatedVisualPlayerPage, and all three are fixed.
//
// The xmlns:av declaration is the part worth noting: app.Markup adds the presentation and
// x: namespaces, and anything else — a using: namespace for a specific CLR-style
// namespace — has to be declared in the fragment, which is what "using:" is for.
func buildProgressRingCustomLottieSourcePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	template, err := app.LoadMarkup[uixaml.IControlTemplate](app.Markup(
		`<ControlTemplate TargetType="ProgressRing"
			xmlns:av="using:Microsoft.UI.Xaml.Controls.AnimatedVisuals">
			<AnimatedVisualPlayer AutoPlay="True" Width="80" Height="80">
				<av:AnimatedFindVisualSource/>
			</AnimatedVisualPlayer>
		</ControlTemplate>`), &uixaml.IID_IControlTemplate)
	if err != nil {
		return nil, fmt.Errorf("parsing the retemplate: %w", err)
	}
	defer template.Release()

	custom, err := uixaml.NewProgressRing()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		custom.SetIsActive(true),
		custom.SetIsIndeterminate(true),
		app.With(custom.AsControl, func(control *uixaml.IControl) error {
			return control.SetTemplate(template)
		}),
		app.With(custom.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(96), frame.SetHeight(96))
		}),
	); err != nil {
		return nil, err
	}

	// The stock ring beside it, so the substitution is visible rather than asserted.
	stock, err := uixaml.NewProgressRing()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		stock.SetIsActive(true),
		stock.SetIsIndeterminate(true),
		app.With(stock.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(96), frame.SetHeight(96))
		}),
	); err != nil {
		return nil, err
	}

	row, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		row.SetOrientation(uixaml.OrientationHorizontal),
		row.SetSpacing(24),
		app.Append(row.AsPanel, custom.AsUIElement, stock.AsUIElement),
	); err != nil {
		return nil, err
	}

	note, err := label("A retemplated ProgressRing playing AnimatedFindVisualSource, " +
		"beside the stock ring. ProgressRing has no Source property — its template holds " +
		"an AnimatedVisualPlayer, so replacing the animation means replacing the template.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, row.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

func buildProgressBarAxeTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range []struct {
		caption       string
		indeterminate bool
		value         float64
		paused        bool
		errored       bool
	}{
		{"Determinate at 40", false, 40, false, false},
		{"Indeterminate", true, 0, false, false},
		{"Paused", false, 60, true, false},
		{"Error", false, 60, false, true},
	} {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		bar, err := uixaml.NewProgressBar()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			bar.SetIsIndeterminate(entry.indeterminate),
			bar.SetShowPaused(entry.paused),
			bar.SetShowError(entry.errored),
			app.With(bar.AsRangeBase, func(base *uixaml.IRangeBase) error {
				return base.SetValue(entry.value)
			}),
			app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
				return frame.SetWidth(280)
			}),
		); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, bar.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ProgressBarReTemplatePage: the bar with a ControlTemplate of its own.
//
// A ControlTemplate is markup by nature — it is a tree of setters and named parts the
// control looks up by name — so this is the app/markup.go case rather than a shortcut.
// The point of the page is that a retemplated bar still behaves like a ProgressBar: Value
// still drives the fill, because the template's parts are what the control binds to.
func buildProgressBarReTemplatePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	template, err := app.LoadMarkup[uixaml.IControlTemplate](app.Markup(
		`<ControlTemplate TargetType="ProgressBar">
			<Border Background="#22808080" CornerRadius="8" Height="24">
				<Grid>
					<Rectangle x:Name="ProgressBarIndicator" Fill="#40C040"
						HorizontalAlignment="Left" RadiusX="8" RadiusY="8"/>
					<TextBlock Text="Retemplated" HorizontalAlignment="Center"
						VerticalAlignment="Center" FontSize="12"/>
				</Grid>
			</Border>
		</ControlTemplate>`), &uixaml.IID_IControlTemplate)
	if err != nil {
		return nil, err
	}
	defer template.Release()

	bar, err := uixaml.NewProgressBar()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(bar.AsControl, func(control *uixaml.IControl) error {
			return control.SetTemplate(template)
		}),
		app.With(bar.AsRangeBase, func(base *uixaml.IRangeBase) error {
			return base.SetValue(45)
		}),
		app.With(bar.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetWidth(300)
		}),
	); err != nil {
		return nil, err
	}

	// A default-templated one beside it, so the substitution is visible.
	plain, err := uixaml.NewProgressBar()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.With(plain.AsRangeBase, func(base *uixaml.IRangeBase) error {
			return base.SetValue(45)
		}),
		app.With(plain.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return frame.SetWidth(300)
		}),
	); err != nil {
		return nil, err
	}

	caption, err := label("A ProgressBar with its own ControlTemplate, then the default, " +
		"both at Value 45.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, caption.AsUIElement, bar.AsUIElement, plain.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// InfoBadgePage: the three shapes a badge takes.
//
// Which one you get is decided by what is set, not by a mode: a Value gives the numeric
// badge, an IconSource the icon badge, and neither gives the dot.
func buildInfoBadgePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)

	dot, err := uixaml.NewInfoBadge()
	if err != nil {
		return nil, err
	}
	dotCaption, err := label("Dot: neither Value nor IconSource set")
	if err != nil {
		return nil, err
	}
	children = append(children, dotCaption.AsUIElement, dot.AsUIElement)

	for _, value := range []int32{1, 12, 999} {
		badge, err := uixaml.NewInfoBadge()
		if err != nil {
			return nil, err
		}
		if err := badge.SetValue(value); err != nil {
			return nil, err
		}
		caption, err := label(fmt.Sprintf("Value %d", value))
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, badge.AsUIElement)
	}

	iconBadge, err := uixaml.NewInfoBadge()
	if err != nil {
		return nil, err
	}
	icon, err := uixaml.NewFontIconSource()
	if err != nil {
		return nil, err
	}
	defer icon.Release()
	if err := icon.SetGlyph(""); err != nil {
		return nil, err
	}
	source, err := icon.AsIconSource()
	if err != nil {
		return nil, err
	}
	defer source.Release()
	if err := iconBadge.SetIconSource(source); err != nil {
		return nil, err
	}
	iconCaption, err := label("IconSource set")
	if err != nil {
		return nil, err
	}
	children = append(children, iconCaption.AsUIElement, iconBadge.AsUIElement)

	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// newTeachingTip builds a tip with the buttons the source pages use.
func newTeachingTip(title, subtitle string, target *uixaml.IFrameworkElement,
) (*uixaml.TeachingTip, error) {
	tip, err := uixaml.NewTeachingTip()
	if err != nil {
		return nil, err
	}
	action, err := app.Box("Act")
	if err != nil {
		return nil, err
	}
	defer action.Release()
	close, err := app.Box("Dismiss")
	if err != nil {
		return nil, err
	}
	defer close.Release()

	if err := app.All(
		tip.SetTitle(title),
		tip.SetSubtitle(subtitle),
		tip.SetActionButtonContent(action),
		tip.SetCloseButtonContent(close),
	); err != nil {
		return nil, err
	}
	if target != nil {
		if err := tip.SetTarget(target); err != nil {
			return nil, err
		}
	}
	return tip, nil
}

// TeachingTipPage: a targeted tip and an untargeted one.
//
// A tip WITH a Target points at that element and follows it; one without opens against
// the bottom edge of the window, horizontally centred. Both are popups, which is why they
// must be in the tree to open — a tip that is never added to a parent silently does
// nothing.
//
// WHERE they are parented matters as much as THAT they are. Both tips were originally
// appended to the page's StackPanel, which is the obvious place and the wrong one: a tip
// in a stack occupies a layout slot and is positioned from it, so both rendered visibly
// out of place. Neither the conformance suite nor an automation peer can see that — a
// misplaced tip has the same tree as a correct one — and it was found by looking at the
// running page.
func buildTeachingTipPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	anchor, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(anchor.AsContentControl, "The tip points here"); err != nil {
		return nil, err
	}
	anchorFrame, err := anchor.AsFrameworkElement()
	if err != nil {
		return nil, err
	}
	defer anchorFrame.Release()

	targeted, err := newTeachingTip("Targeted", "This tip has a Target and points at it.", anchorFrame)
	if err != nil {
		return nil, err
	}
	// "at the bottom", not "centred": an untargeted TeachingTip opens against the
	// BOTTOM edge of the XamlRoot, horizontally centred — it does not float in the
	// middle of the window. The subtitle used to say centred, and a reference page
	// that describes behaviour the framework does not have is worse than one that
	// says nothing, because a reader takes it on trust.
	untargeted, err := newTeachingTip("Untargeted",
		"This one has no Target, so it opens centred against the bottom of the window.", nil)
	if err != nil {
		return nil, err
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Show the targeted tip", func() { _ = targeted.SetIsOpen(true) })
		},
		func() (*uixaml.Button, error) {
			return button("Show the untargeted tip", func() { _ = untargeted.SetIsOpen(true) })
		},
	)
	if err != nil {
		return nil, err
	}

	panel, err := stack(10, anchor.AsUIElement, row.AsUIElement)
	if err != nil {
		return nil, err
	}

	// The tips are parented in a GRID, overlaying the content, not stacked after it.
	//
	// A TeachingTip needs a parent in the tree to open at all, and the obvious place —
	// the end of the StackPanel holding the page — is wrong: a tip in a stack still
	// takes a layout slot in that stack, and it is measured and positioned from that
	// slot. Both tips rendered, and both rendered in the wrong place, which is a
	// defect no automation peer can see. A Grid cell overlays instead of consuming
	// space, so the targeted tip anchors on its Target and the untargeted one centres
	// on the window, which is what each is for.
	root, err := gridOf([]uixaml.GridLength{star(1)}, nil)
	if err != nil {
		return nil, err
	}
	if err := app.Append(root.AsPanel,
		panel.AsUIElement, targeted.AsUIElement, untargeted.AsUIElement); err != nil {
		return nil, err
	}
	return root.AsUIElement()
}

// TeachingTipInXamlPage: the tip declared in markup, which is how most applications write
// one — the content is a tree, and a tree is what markup is for.
func buildTeachingTipInXamlPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tip, err := app.LoadMarkup[uixaml.ITeachingTip](app.Markup(
		`<TeachingTip Title="Declared in XAML"
			Subtitle="Content, buttons and all, from markup.">
			<TeachingTip.Content>
				<StackPanel Spacing="6" Margin="0,8,0,0">
					<TextBlock Text="A tip can hold a whole tree." TextWrapping="Wrap"/>
					<CheckBox Content="Do not show this again"/>
				</StackPanel>
			</TeachingTip.Content>
		</TeachingTip>`), &uixaml.IID_ITeachingTip)
	if err != nil {
		return nil, err
	}

	element, err := winrt.QueryInterface[uixaml.IUIElement](
		unsafe.Pointer(tip), &uixaml.IID_IUIElement)
	if err != nil {
		return nil, err
	}
	defer element.Release()

	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Show the tip", func() { _ = tip.SetIsOpen(true) })
	})
	if err != nil {
		return nil, err
	}
	caption, err := label("A TeachingTip parsed from markup, content and all.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, caption.AsUIElement, row.AsUIElement, func() (*uixaml.IUIElement, error) {
		// stack takes ownership of what its getters return, so hand it its own
		// reference rather than the one this function already defers a Release for.
		return winrt.QueryInterface[uixaml.IUIElement](
			unsafe.Pointer(tip), &uixaml.IID_IUIElement)
	})
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TeachingTipFocusPage: where focus goes when a tip opens.
//
// PreferredPlacement decides where the tip sits; the focus question is whether opening one
// steals focus from the page behind it, which is what the source checks with a focusable
// control either side.
func buildTeachingTipFocusPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	before, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(before.AsContentControl, "Before"); err != nil {
		return nil, err
	}
	frame, err := before.AsFrameworkElement()
	if err != nil {
		return nil, err
	}
	defer frame.Release()

	tip, err := newTeachingTip("Focus", "Opening this should not steal focus.", frame)
	if err != nil {
		return nil, err
	}
	if err := tip.SetPreferredPlacement(uixaml.TeachingTipPlacementModeBottom); err != nil {
		return nil, err
	}

	after, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(after.AsContentControl, "After"); err != nil {
		return nil, err
	}

	status, err := label("Tab between the buttons, then open the tip.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(tip.AddClosed,
		uixaml.NewTypedEventHandlerOfTeachingTipAndTeachingTipClosedEventArgs,
		func(_ *uixaml.ITeachingTip, _ *uixaml.ITeachingTipClosedEventArgs) {
			_ = status.SetText("The tip closed; focus returns to the page.")
		}); err != nil {
		return nil, err
	}

	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Open the tip", func() { _ = tip.SetIsOpen(true) })
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, status.AsUIElement, before.AsUIElement, row.AsUIElement,
		after.AsUIElement)
	if err != nil {
		return nil, err
	}
	// Overlaid, not stacked — see buildTeachingTipPage. A tip added to the end of a
	// StackPanel takes a layout slot there and is positioned from it, which put the
	// tips on TeachingTipPage in visibly wrong places.
	root, err := gridOf([]uixaml.GridLength{star(1)}, nil)
	if err != nil {
		return nil, err
	}
	if err := app.Append(root.AsPanel, panel.AsUIElement, tip.AsUIElement); err != nil {
		return nil, err
	}
	return root.AsUIElement()
}

// RatingControlPage: the control in each of the states the source varies.
//
// PlaceholderValue is the one worth knowing: it is what the control shows when Value has
// not been set — an average rating, say — and it is visually distinct from a rating the
// user gave, which is the whole reason the property exists.
func buildRatingControlPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	cases := []struct {
		caption     string
		value       float64
		placeholder float64
		readOnly    bool
		maxRating   int32
		captionText string
	}{
		{"Unset, with a placeholder of 3.5", 0, 3.5, false, 5, "Average"},
		{"Set to 4", 4, 0, false, 5, ""},
		{"Read-only at 2", 2, 0, true, 5, "Locked"},
		{"Ten stars, at 7", 7, 0, false, 10, ""},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range cases {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		control, err := uixaml.NewRatingControl()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			control.SetMaxRating(entry.maxRating),
			control.SetIsReadOnly(entry.readOnly),
			control.SetIsClearEnabled(true),
			control.SetCaption(entry.captionText),
		); err != nil {
			return nil, err
		}
		// Value must be set AFTER PlaceholderValue: a control with both shows Value, and
		// setting the placeholder afterwards would look like it had cleared it.
		if entry.placeholder > 0 {
			if err := control.SetPlaceholderValue(entry.placeholder); err != nil {
				return nil, err
			}
		}
		if entry.value > 0 {
			if err := control.SetValue(entry.value); err != nil {
				return nil, err
			}
		}
		children = append(children, caption.AsUIElement, control.AsUIElement)
	}

	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

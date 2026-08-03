//go:build windows && amd64

package pages

// The icon, animation and material pages.
//
// Sources: controls/dev/{AnimatedIcon,AnimatedVisualPlayer,ImageIcon,RadialGradientBrush,
// MonochromaticOverlayPresenter,Materials}/TestUI/*Page.xaml
//
// Materials is the largest of these and the most changed by WinUI 3. Acrylic survived and
// is here; Reveal did not, and its nine pages are recorded as unmappable with the winmd
// evidence, at the bottom of this file.

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlanimatedvisuals "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/controls/animatedvisuals"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "AnimatedIcon", Name: "AnimatedIconPage", Build: buildAnimatedIconPage})
	register(Page{Control: "AnimatedVisualPlayer", Name: "AnimatedVisualPlayerPage", Build: buildAnimatedVisualPlayerPage})
	register(Page{Control: "ImageIcon", Name: "ImageIconPage", Build: buildImageIconPage})
	register(Page{Control: "RadialGradientBrush", Name: "RadialGradientBrushPage", Build: buildRadialGradientBrushPage, Inert: materialReason})
	register(Page{Control: "MonochromaticOverlayPresenter", Name: "MonochromaticOverlayPresenterPage", Build: buildMonochromaticOverlayPresenterPage})

	register(Page{Control: "Materials", Name: "AcrylicPage", Build: buildAcrylicPage, Inert: materialReason})
	register(Page{Control: "Materials", Name: "AcrylicBrushPage", Build: buildAcrylicBrushPage, Inert: materialReason})
	register(Page{Control: "Materials", Name: "AcrylicBrushBasicPage", Build: buildAcrylicBrushBasicPage, Inert: materialReason})
	register(Page{Control: "Materials", Name: "AcrylicBrushLuminosityTestPage", Build: buildAcrylicBrushLuminosityTestPage, Inert: materialReason})
	register(Page{Control: "Materials", Name: "AcrylicColorPage", Build: buildAcrylicColorPage, Inert: materialReason})
	register(Page{Control: "Materials", Name: "AcrylicMarkupPage", Build: buildAcrylicMarkupPage, Inert: materialReason})
	register(Page{Control: "Materials", Name: "AcrylicRenderingPage", Build: buildAcrylicRenderingPage, Inert: materialReason})

	// Reveal is ABSENT from the Windows App SDK winmds:
	//
	//	go run ./cmd/inspect --dir metadata/winmd --search RevealBrush
	//	0 types
	//
	// It was a Fluent lighting effect in WinUI 2 — a brush that lit up under the pointer,
	// driven by XamlLight — and it did not carry forward into WinUI 3. AcrylicBrush,
	// beside it in the same source directory, did, which is why seven of these pages port
	// and nine do not.
	const revealReason = "RevealBrush and RevealBackgroundBrush are not Windows App SDK " +
		"types; Reveal was a WinUI 2 Fluent effect that did not carry forward into " +
		"WinUI 3 (inspect --search RevealBrush reports 0 types)"
	for _, name := range []string{
		"RevealPage", "RevealBasicPage", "RevealColorPage", "RevealFallbackPage",
		"RevealListPage", "RevealMarkupPage", "RevealRegressionTestsPage",
		"RevealSimpleListPage", "RevealStatesPage",
	} {
		register(Page{Control: "Materials", Name: name, Unmappable: revealReason})
	}

	// CoreWindowEventsPage drives Windows.UI.Core.CoreWindow, the UWP window object. A
	// WinUI 3 application has a Microsoft.UI.Xaml.Window over an HWND and no CoreWindow
	// at all — inspect --search CoreWindow also reports 0 types.
	register(Page{Control: "Materials", Name: "CoreWindowEventsPage",
		Unmappable: "CoreWindow is the UWP window object and is absent from the Windows " +
			"App SDK; a WinUI 3 application has a Microsoft.UI.Xaml.Window over an HWND"})
}

// AnimatedIconPage: the icon, animating, with its markers read from the source.
//
// This page took three goes and each mistake is worth keeping, because none of them
// produced an error — the icon simply sat there.
//
// FIRST: the animation was said to be out of reach, because a Lottie source is what the
// codegen tool emits as a generated class and there is no such generator for Go. True,
// and beside the point: WinUI SHIPS eight of those generated classes in
// Microsoft.UI.Xaml.Controls.AnimatedVisuals, as ordinary activatable runtime classes.
//
// SECOND: the AnimatedIconStatics were fetched once and released with a defer, then used
// from handlers that outlive the builder. A use-after-free, which panicked on a nil
// vtable inside a COM callback.
//
// THIRD: hovering did nothing, for two separate reasons at once —
//
//   - An AnimatedIcon has no background, and an element with no background is not
//     hit-testable over its transparent parts. PointerEntered never fired. The fix is a
//     Border with a TRANSPARENT background rather than none: transparent is painted and
//     therefore hit-tested, null is not painted and is not.
//   - The state names are not a fixed vocabulary. Each source declares its own MARKERS,
//     and AnimatedIcon plays between them by looking for "<from>To<to>_Start" and
//     "_End". Guessing "Normal" and "PointerOver" happens to be right for some sources
//     and wrong for others.
//
// So the states are READ from the source rather than assumed, which is also what makes
// this page useful as reference: it lists what each animation actually defines.
func buildAnimatedIconPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	settings, err := uixamlanimatedvisuals.NewAnimatedSettingsVisualSource()
	if err != nil {
		return nil, err
	}
	defer settings.Release()
	settingsSource, err := settings.AsAnimatedVisualSource2()
	if err != nil {
		return nil, err
	}
	defer settingsSource.Release()

	navigation, err := uixamlanimatedvisuals.NewAnimatedGlobalNavigationButtonVisualSource()
	if err != nil {
		return nil, err
	}
	defer navigation.Release()
	navigationSource, err := navigation.AsAnimatedVisualSource2()
	if err != nil {
		return nil, err
	}
	defer navigationSource.Release()

	buttonDriven, buttonNote, err := animatedIconWithButtons(
		"AnimatedSettingsVisualSource", settingsSource)
	if err != nil {
		return nil, err
	}
	hoverDriven, hoverNote, err := animatedIconWithHover(
		"AnimatedGlobalNavigationButtonVisualSource", navigationSource)
	if err != nil {
		return nil, err
	}

	panel, err := stack(10, buttonNote, buttonDriven, hoverNote, hoverDriven)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// iconStates reads a source's markers and returns the state names it animates between.
//
// A marker is named "<from>To<to>_Start" or "<from>To<to>_End", so the state names are
// the two halves of each transition. Reading them is the only reliable way to know what
// a given animation supports — they differ per source, and setting an unknown state is
// silently ignored.
func iconStates(source *uixaml.IAnimatedVisualSource2) ([]string, []string, error) {
	markers, err := source.Markers()
	if err != nil {
		return nil, nil, err
	}
	defer markers.Release()

	iterable, err := markers.AsIterableOfIKeyValuePairOfStringAndDouble()
	if err != nil {
		return nil, nil, err
	}
	defer iterable.Release()
	iterator, err := iterable.First()
	if err != nil {
		return nil, nil, err
	}
	defer iterator.Release()

	var names []string
	seen := map[string]bool{}
	var states []string
	for {
		has, err := iterator.HasCurrent()
		if err != nil || !has {
			break
		}
		pair, err := iterator.Current()
		if err != nil {
			break
		}
		key, keyErr := pair.Key()
		pair.Release()
		if keyErr == nil {
			names = append(names, key)
			for _, state := range statesFromMarker(key) {
				if !seen[state] {
					seen[state] = true
					states = append(states, state)
				}
			}
		}
		if _, err := iterator.MoveNext(); err != nil {
			break
		}
	}
	return states, names, nil
}

// statesFromMarker splits "NormalToPointerOver_Start" into its two state names.
func statesFromMarker(marker string) []string {
	name := marker
	for _, suffix := range []string{"_Start", "_End"} {
		name = strings.TrimSuffix(name, suffix)
	}
	from, to, found := strings.Cut(name, "To")
	if !found || from == "" || to == "" {
		return nil
	}
	return []string{from, to}
}

// newAnimatedIcon builds a hit-testable AnimatedIcon over the given source.
//
// The Border is not decoration. An AnimatedIcon draws no background, and an element with
// no background is not hit-tested over its transparent parts — so PointerEntered never
// fires on the icon itself. A background of Transparent IS painted, and therefore is hit
// tested; a background of null is not.
func newAnimatedIcon(source *uixaml.IAnimatedVisualSource2) (*uixaml.AnimatedIcon, *uixaml.Border, error) {
	icon, err := uixaml.NewAnimatedIcon()
	if err != nil {
		return nil, nil, err
	}
	if err := app.All(
		icon.SetSource(source),
		app.With(icon.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			// 140, not the 48 a real toolbar would use. These are 150ms micro-
			// transitions designed to be barely noticed in a nav button; at 48px they
			// read as nothing happening at all, which is exactly how this page was
			// first reported as broken.
			return app.All(frame.SetWidth(140), frame.SetHeight(140))
		}),
	); err != nil {
		return nil, nil, err
	}

	border, err := uixaml.NewBorder()
	if err != nil {
		return nil, nil, err
	}
	transparent, err := solidBrush(wrtui.Color{A: 0, R: 0, G: 0, B: 0})
	if err != nil {
		return nil, nil, err
	}
	err = border.SetBackground(transparent)
	transparent.Release()
	if err != nil {
		return nil, nil, err
	}
	if err := app.All(
		border.SetPadding(uixaml.Thickness{Left: 8, Top: 8, Right: 8, Bottom: 8}),
		app.With(icon.AsUIElement, border.SetChild),
		app.With(border.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(
				frame.SetWidth(160), frame.SetHeight(160),
				frame.SetHorizontalAlignment(uixaml.HorizontalAlignmentLeft),
			)
		}),
	); err != nil {
		return nil, nil, err
	}
	return icon, border, nil
}

// setIconState is the whole of driving an AnimatedIcon.
//
// The statics are fetched per call rather than held, because a handler outlives the
// function that registered it and a released statics pointer is a use-after-free — which
// is exactly what the earlier version of this page did.
func setIconState(icon *uixaml.AnimatedIcon, state string) error {
	statics, err := uixaml.AnimatedIconStatics()
	if err != nil {
		return err
	}
	defer statics.Release()
	return app.With(icon.AsDependencyObject, func(object *uixaml.IDependencyObject) error {
		return statics.SetState(object, state)
	})
}

// animatedIconWithButtons builds an icon with one button per state the source declares.
func animatedIconWithButtons(name string, source *uixaml.IAnimatedVisualSource2,
) (func() (*uixaml.IUIElement, error), func() (*uixaml.IUIElement, error), error) {
	icon, border, err := newAnimatedIcon(source)
	if err != nil {
		return nil, nil, err
	}
	states, markers, err := iconStates(source)
	if err != nil {
		return nil, nil, err
	}

	status, err := label("")
	if err != nil {
		return nil, nil, err
	}
	set := func(state string) {
		if err := setIconState(icon, state); err != nil {
			_ = status.SetText("SetState failed: " + err.Error())
			return
		}
		_ = status.SetText("State: " + state)
	}
	if len(states) > 0 {
		set(states[0])
	}

	var makers []func() (*uixaml.Button, error)
	for _, state := range states {
		value := state
		makers = append(makers, func() (*uixaml.Button, error) {
			return button(value, func() { set(value) })
		})
	}
	row, err := buttonRow(makers...)
	if err != nil {
		return nil, nil, err
	}

	note, err := label(fmt.Sprintf(
		"%s\n  states: %s\n  markers: %s\n\n"+
			"Each transition is a fraction of the whole animation — the marker values "+
			"above are its start and end, in the range 0 to 1. NormalToPointerOver is "+
			"about 150ms. At an icon's real size that is deliberately hard to notice; "+
			"these are drawn large so it can be seen at all.",
		name, strings.Join(states, ", "), strings.Join(markers, ", ")))
	if err != nil {
		return nil, nil, err
	}

	group, err := stack(6, status.AsUIElement, row.AsUIElement, border.AsUIElement)
	if err != nil {
		return nil, nil, err
	}
	return group.AsUIElement, note.AsUIElement, nil
}

// animatedIconWithHover builds the same thing driven by the pointer, which is how an
// AnimatedIcon is meant to be used.
func animatedIconWithHover(name string, source *uixaml.IAnimatedVisualSource2,
) (func() (*uixaml.IUIElement, error), func() (*uixaml.IUIElement, error), error) {
	icon, border, err := newAnimatedIcon(source)
	if err != nil {
		return nil, nil, err
	}
	states, markers, err := iconStates(source)
	if err != nil {
		return nil, nil, err
	}

	// The resting state and the hovered state, taken from what the source declares
	// rather than assumed. "Normal" and "PointerOver" are the usual pair and are not
	// universal.
	resting, hovered := "", ""
	for _, state := range states {
		switch state {
		case "Normal":
			resting = state
		case "PointerOver":
			hovered = state
		}
	}
	if resting == "" && len(states) > 0 {
		resting = states[0]
	}
	if hovered == "" && len(states) > 1 {
		hovered = states[1]
	}

	status, err := label("Hover the icon below.")
	if err != nil {
		return nil, nil, err
	}
	if resting != "" {
		_ = setIconState(icon, resting)
	}

	// Registered on the BORDER, which is the hit-testable element. Registering on the
	// icon is what did not work.
	if err := app.With(border.AsUIElement, func(element *uixaml.IUIElement) error {
		if _, err := app.On(element.AddPointerEntered, uixaml.NewPointerEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IPointerRoutedEventArgs) {
				if err := setIconState(icon, hovered); err != nil {
					_ = status.SetText("SetState failed: " + err.Error())
					return
				}
				_ = status.SetText("State: " + hovered)
			}); err != nil {
			return err
		}
		_, err := app.On(element.AddPointerExited, uixaml.NewPointerEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IPointerRoutedEventArgs) {
				if err := setIconState(icon, resting); err != nil {
					return
				}
				_ = status.SetText("State: " + resting)
			})
		return err
	}); err != nil {
		return nil, nil, err
	}

	note, err := label(fmt.Sprintf(
		"%s, driven by the pointer (%s <-> %s)\n  markers: %s",
		name, resting, hovered, strings.Join(markers, ", ")))
	if err != nil {
		return nil, nil, err
	}
	group, err := stack(6, status.AsUIElement, border.AsUIElement)
	if err != nil {
		return nil, nil, err
	}
	return group.AsUIElement, note.AsUIElement, nil
}

// AnimatedVisualPlayerPage: the player, playing.
//
// Unlike AnimatedIcon the player runs the whole animation, so AutoPlay is enough to see
// it move. PlayAsync, Pause and Stop drive it by hand; Duration and IsAnimatedVisualLoaded
// report what it loaded, which are the properties the source page checks.
func buildAnimatedVisualPlayerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	player, err := uixaml.NewAnimatedVisualPlayer()
	if err != nil {
		return nil, err
	}

	source, err := uixamlanimatedvisuals.NewAnimatedChevronUpDownSmallVisualSource()
	if err != nil {
		return nil, err
	}
	defer source.Release()
	first, err := source.AsAnimatedVisualSource()
	if err != nil {
		return nil, err
	}
	defer first.Release()

	if err := app.All(
		player.SetSource(first),
		player.SetAutoPlay(true),
		app.With(player.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(240), frame.SetHeight(240))
		}),
	); err != nil {
		return nil, err
	}

	status, err := label("Loading...")
	if err != nil {
		return nil, err
	}
	refresh := func() {
		loaded, err := player.IsAnimatedVisualLoaded()
		if err != nil {
			_ = status.SetText("IsAnimatedVisualLoaded failed: " + err.Error())
			return
		}
		duration, err := player.Duration()
		if err != nil {
			return
		}
		playing, err := player.IsPlaying()
		if err != nil {
			return
		}
		_ = status.SetText(fmt.Sprintf(
			"IsAnimatedVisualLoaded=%v, IsPlaying=%v, Duration=%v",
			loaded, playing, time.Duration(duration.Duration*100)))
	}
	// Read after load: the properties mean nothing before the source has been realized,
	// which is what the source page timing works around.
	if err := app.With(player.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		_, addErr := app.On(frame.AddLoaded, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) { refresh() })
		return addErr
	}); err != nil {
		return nil, err
	}

	play := func(loop bool) func() {
		return func() {
			// PlayAsync returns an IAsyncAction and is NOT awaited, for the reason
			// ContentDialogPage records: the completion arrives on this same thread.
			operation, err := player.PlayAsync(0, 1, loop)
			if err != nil {
				_ = status.SetText("PlayAsync failed: " + err.Error())
				return
			}
			operation.Release()
			refresh()
		}
	}
	row, err := buttonRow(
		func() (*uixaml.Button, error) { return button("Play once", play(false)) },
		func() (*uixaml.Button, error) { return button("Play looping", play(true)) },
		func() (*uixaml.Button, error) {
			return button("Pause", func() {
				if err := player.Pause(); err != nil {
					_ = status.SetText("Pause failed: " + err.Error())
					return
				}
				refresh()
			})
		},
		func() (*uixaml.Button, error) {
			return button("Stop", func() {
				if err := player.Stop(); err != nil {
					_ = status.SetText("Stop failed: " + err.Error())
					return
				}
				refresh()
			})
		},
	)
	if err != nil {
		return nil, err
	}

	note, err := label("AnimatedChevronUpDownSmallVisualSource, one of the eight " +
		"animations the SDK ships in Microsoft.UI.Xaml.Controls.AnimatedVisuals.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(10, note.AsUIElement, status.AsUIElement, row.AsUIElement,
		player.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ImageIconPage: an icon whose art is an image rather than a glyph.
//
// The source points at a file in the test app's package. An unpackaged Go application has
// no such package, so this uses a generated image source instead — which exercises the
// same property with something that always exists.
func buildImageIconPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	icon, err := uixaml.NewImageIcon()
	if err != nil {
		return nil, err
	}
	if err := app.With(icon.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(64), frame.SetHeight(64))
	}); err != nil {
		return nil, err
	}

	// A SvgImageSource with no URI set is still a valid ImageSource, so the property
	// round-trips even though nothing is drawn.
	image, err := uixaml.NewSvgImageSource()
	if err != nil {
		return nil, err
	}
	defer image.Release()
	source, err := image.AsImageSource()
	if err != nil {
		return nil, err
	}
	defer source.Release()
	if err := icon.SetSource(source); err != nil {
		return nil, err
	}

	readBack, err := icon.Source()
	state := "Source is nil"
	if err == nil && readBack != nil {
		readBack.Release()
		state = "Source is set and readable"
	}

	note, err := label("ImageIcon: " + state + ".\n\n" +
		"The source page points at a file inside the test application's package. An " +
		"unpackaged Go application has no package to hold one, so what is exercised " +
		"here is the property rather than a particular picture.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, icon.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// RadialGradientBrushPage: the brush at several radii and origins.
func buildRadialGradientBrushPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	cases := []struct {
		caption string
		radiusX float64
		radiusY float64
		origin  wrtfoundation.Point
	}{
		{"Centred, round", 0.5, 0.5, wrtfoundation.Point{X: 0.5, Y: 0.5}},
		{"Centred, elliptical", 0.8, 0.3, wrtfoundation.Point{X: 0.5, Y: 0.5}},
		{"Origin top-left", 0.7, 0.7, wrtfoundation.Point{X: 0.15, Y: 0.15}},
	}

	var children []func() (*uixaml.IUIElement, error)
	for _, entry := range cases {
		caption, err := label(entry.caption)
		if err != nil {
			return nil, err
		}
		brush, err := uixaml.NewRadialGradientBrush()
		if err != nil {
			return nil, err
		}
		if err := app.All(
			brush.SetRadiusX(entry.radiusX),
			brush.SetRadiusY(entry.radiusY),
			brush.SetGradientOrigin(entry.origin),
			brush.SetCenter(wrtfoundation.Point{X: 0.5, Y: 0.5}),
		); err != nil {
			return nil, err
		}

		// GradientStops is an IObservableVector, whose mutation methods live on the
		// IVector it Requires — the same accessor pattern as ItemsControl.Items.
		observable, err := brush.GradientStops()
		if err != nil {
			return nil, err
		}
		stops, err := observable.AsVectorOfGradientStop()
		observable.Release()
		if err != nil {
			return nil, err
		}
		for _, stop := range []struct {
			offset float64
			colour wrtui.Color
		}{
			{0, wrtui.Color{A: 255, R: 240, G: 200, B: 60}},
			{1, wrtui.Color{A: 255, R: 40, G: 60, B: 160}},
		} {
			entry, err := uixaml.NewGradientStop()
			if err != nil {
				stops.Release()
				return nil, err
			}
			err = app.All(entry.SetOffset(stop.offset), entry.SetColor(stop.colour))
			if err == nil {
				err = stops.Append(&entry.IGradientStop)
			}
			entry.Release()
			if err != nil {
				stops.Release()
				return nil, err
			}
		}
		stops.Release()

		border, err := uixaml.NewBorder()
		if err != nil {
			return nil, err
		}
		base, err := brush.AsBrush()
		if err != nil {
			return nil, err
		}
		err = border.SetBackground(base)
		base.Release()
		if err != nil {
			return nil, err
		}
		if err := app.With(border.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(220), frame.SetHeight(120))
		}); err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, border.AsUIElement)
	}

	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// MonochromaticOverlayPresenterPage: an element re-drawn in one colour.
//
// SourceElement is the element to recolour and it stays where it is — the presenter draws
// its own copy. That is the whole control, and it is what makes it usable as an overlay
// rather than as a filter on the original.
func buildMonochromaticOverlayPresenterPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	original, err := bigContent(240, 300, 4)
	if err != nil {
		return nil, err
	}
	originalElement, err := original.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer originalElement.Release()

	presenter, err := uixaml.NewMonochromaticOverlayPresenter()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		presenter.SetSourceElement(originalElement),
		presenter.SetReplacementColor(wrtui.Color{A: 255, R: 40, G: 120, B: 220}),
		app.With(presenter.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(240), frame.SetHeight(300))
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
		row.SetSpacing(16),
		app.Append(row.AsPanel, original.AsUIElement, presenter.AsUIElement),
	); err != nil {
		return nil, err
	}

	caption, err := label("The original, then the same element re-drawn in one colour. " +
		"SourceElement is not moved — the presenter draws its own copy.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, row.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// acrylicOver builds a Border filled with an AcrylicBrush over the given backdrop.
func acrylicOver(tint wrtui.Color, opacity float64, fallbackOnly bool) (*uixaml.Border, error) {
	brush, err := uixaml.NewAcrylicBrush()
	if err != nil {
		return nil, err
	}
	defer brush.Release()
	if err := app.All(
		brush.SetTintColor(tint),
		brush.SetTintOpacity(opacity),
		brush.SetAlwaysUseFallback(fallbackOnly),
	); err != nil {
		return nil, err
	}

	border, err := uixaml.NewBorder()
	if err != nil {
		return nil, err
	}
	base, err := brush.AsBrush()
	if err != nil {
		return nil, err
	}
	err = border.SetBackground(base)
	base.Release()
	if err != nil {
		return nil, err
	}
	if err := app.With(border.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(240), frame.SetHeight(120))
	}); err != nil {
		return nil, err
	}
	return border, nil
}

// acrylicOverBackdrop layers acrylic panels over something patterned, since acrylic over
// a flat colour shows nothing of what it does.
func acrylicOverBackdrop(panels []func() (*uixaml.Border, error), caption string,
) (*uixaml.IUIElement, error) {
	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	backdrop, err := bigContent(520, 420, 6)
	if err != nil {
		return nil, err
	}
	if err := app.Append(grid.AsPanel, backdrop.AsUIElement); err != nil {
		return nil, err
	}

	column, err := uixaml.NewStackPanel()
	if err != nil {
		return nil, err
	}
	if err := app.All(column.SetOrientation(uixaml.OrientationVertical), column.SetSpacing(12)); err != nil {
		return nil, err
	}
	for _, make := range panels {
		border, err := make()
		if err != nil {
			return nil, err
		}
		if err := app.Append(column.AsPanel, border.AsUIElement); err != nil {
			return nil, err
		}
	}
	if err := app.Append(grid.AsPanel, column.AsUIElement); err != nil {
		return nil, err
	}
	if err := app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(520), frame.SetHeight(420))
	}); err != nil {
		return nil, err
	}

	note, err := label(caption)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, grid.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func buildAcrylicPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return acrylicOverBackdrop([]func() (*uixaml.Border, error){
		func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 255, G: 255, B: 255}, 0.6, false)
		},
		func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 0, G: 0, B: 0}, 0.6, false)
		},
	}, "AcrylicBrush over a patterned backdrop: light tint, then dark.")
}

func buildAcrylicBrushPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return acrylicOverBackdrop([]func() (*uixaml.Border, error){
		func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 40, G: 120, B: 200}, 0.5, false)
		},
	}, "A single AcrylicBrush, tinted blue.")
}

func buildAcrylicBrushBasicPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return acrylicOverBackdrop([]func() (*uixaml.Border, error){
		func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 128, G: 128, B: 128}, 0.5, false)
		},
	}, "AcrylicBrush at its defaults, over a backdrop.")
}

// AcrylicBrushLuminosityTestPage: the tint opacity swept, which is what governs how much
// of the backdrop shows through.
func buildAcrylicBrushLuminosityTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var panels []func() (*uixaml.Border, error)
	for _, opacity := range []float64{0.2, 0.4, 0.6, 0.8} {
		value := opacity
		panels = append(panels, func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 255, G: 255, B: 255}, value, false)
		})
	}
	return acrylicOverBackdrop(panels, "TintOpacity swept from 0.2 to 0.8.")
}

func buildAcrylicColorPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var panels []func() (*uixaml.Border, error)
	for _, colour := range bandColours[:4] {
		value := colour
		panels = append(panels, func() (*uixaml.Border, error) {
			return acrylicOver(value, 0.55, false)
		})
	}
	return acrylicOverBackdrop(panels, "The same brush at four tint colours.")
}

// AcrylicMarkupPage: the brush declared in XAML rather than built in Go.
//
// Both produce the same object; the page exists because markup is how most applications
// write one, and app.LoadMarkup is the route.
func buildAcrylicMarkupPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	border, err := app.LoadMarkup[uixaml.IBorder](app.Markup(
		`<Border Width="240" Height="120" CornerRadius="6">
			<Border.Background>
				<AcrylicBrush TintColor="#3080D0" TintOpacity="0.5"/>
			</Border.Background>
		</Border>`), &uixaml.IID_IBorder)
	if err != nil {
		return nil, err
	}
	defer border.Release()

	grid, err := uixaml.NewGrid()
	if err != nil {
		return nil, err
	}
	backdrop, err := bigContent(420, 300, 5)
	if err != nil {
		return nil, err
	}
	if err := app.All(
		app.Append(grid.AsPanel, backdrop.AsUIElement),
		app.With(grid.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(420), frame.SetHeight(300))
		}),
	); err != nil {
		return nil, err
	}
	element, err := winrtAsUIElement(border)
	if err != nil {
		return nil, err
	}
	defer element.Release()
	if err := app.With(grid.AsPanel, func(panel *uixaml.IPanel) error {
		children, err := panel.Children()
		if err != nil {
			return err
		}
		defer children.Release()
		return children.Append(element)
	}); err != nil {
		return nil, err
	}

	caption, err := label("The same brush, declared in markup.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, grid.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// AcrylicRenderingPage: acrylic with the fallback forced, beside one without.
//
// AlwaysUseFallback is what a machine without the composition capability gets — a flat
// tint instead of a blur — and showing both together is the only way to see which is
// which.
func buildAcrylicRenderingPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return acrylicOverBackdrop([]func() (*uixaml.Border, error){
		func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 255, G: 255, B: 255}, 0.5, false)
		},
		func() (*uixaml.Border, error) {
			return acrylicOver(wrtui.Color{A: 255, R: 255, G: 255, B: 255}, 0.5, true)
		},
	}, "Blurred acrylic, then the same brush with AlwaysUseFallback set — which is what "+
		"a machine without the composition capability renders.")
}

// winrtAsUIElement views a markup-loaded interface as a UIElement.
func winrtAsUIElement(border *uixaml.IBorder) (*uixaml.IUIElement, error) {
	return winrt.QueryInterface[uixaml.IUIElement](
		unsafe.Pointer(border), &uixaml.IID_IUIElement)
}

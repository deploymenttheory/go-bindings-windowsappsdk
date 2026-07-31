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
	"unsafe"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
	wrtui "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/ui"
)

func init() {
	register(Page{Control: "AnimatedIcon", Name: "AnimatedIconPage", Build: buildAnimatedIconPage})
	register(Page{Control: "AnimatedVisualPlayer", Name: "AnimatedVisualPlayerPage", Build: buildAnimatedVisualPlayerPage})
	register(Page{Control: "ImageIcon", Name: "ImageIconPage", Build: buildImageIconPage})
	register(Page{Control: "RadialGradientBrush", Name: "RadialGradientBrushPage", Build: buildRadialGradientBrushPage})
	register(Page{Control: "MonochromaticOverlayPresenter", Name: "MonochromaticOverlayPresenterPage", Build: buildMonochromaticOverlayPresenterPage})

	register(Page{Control: "Materials", Name: "AcrylicPage", Build: buildAcrylicPage})
	register(Page{Control: "Materials", Name: "AcrylicBrushPage", Build: buildAcrylicBrushPage})
	register(Page{Control: "Materials", Name: "AcrylicBrushBasicPage", Build: buildAcrylicBrushBasicPage})
	register(Page{Control: "Materials", Name: "AcrylicBrushLuminosityTestPage", Build: buildAcrylicBrushLuminosityTestPage})
	register(Page{Control: "Materials", Name: "AcrylicColorPage", Build: buildAcrylicColorPage})
	register(Page{Control: "Materials", Name: "AcrylicMarkupPage", Build: buildAcrylicMarkupPage})
	register(Page{Control: "Materials", Name: "AcrylicRenderingPage", Build: buildAcrylicRenderingPage})

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

// AnimatedIconPage: the icon, and the fallback it uses when no animation is supplied.
//
// AnimatedIcon's Source is an IAnimatedVisualSource2, which is what the Lottie codegen
// tool produces — a generated C# or C++ class per animation, not a file loaded at run
// time. There is no such generator for Go, so the page shows the control with its
// FallbackIconSource, which is the path every AnimatedIcon takes when its animation
// cannot be loaded.
func buildAnimatedIconPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	icon, err := uixaml.NewAnimatedIcon()
	if err != nil {
		return nil, err
	}

	fallback, err := uixaml.NewFontIconSource()
	if err != nil {
		return nil, err
	}
	defer fallback.Release()
	if err := app.All(
		fallback.SetGlyph(""),
		fallback.SetFontSize(28),
	); err != nil {
		return nil, err
	}
	source, err := fallback.AsIconSource()
	if err != nil {
		return nil, err
	}
	defer source.Release()
	if err := icon.SetFallbackIconSource(source); err != nil {
		return nil, err
	}
	if err := app.With(icon.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetWidth(48), frame.SetHeight(48))
	}); err != nil {
		return nil, err
	}

	note, err := label("AnimatedIcon showing its FallbackIconSource.\n\n" +
		"Source is an IAnimatedVisualSource2, which the Lottie codegen tool emits as a " +
		"generated class per animation. There is no such generator for Go, so the " +
		"animation itself is out of reach — but the fallback is the path every " +
		"AnimatedIcon takes when one cannot be loaded, so it is the honest thing to show.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, icon.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// AnimatedVisualPlayerPage: the player with no source, reporting what it knows.
//
// Same constraint as AnimatedIcon and the same honest answer: the player's properties are
// readable and its FallbackContent renders, which is what an application sees when the
// animation is missing.
func buildAnimatedVisualPlayerPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	player, err := uixaml.NewAnimatedVisualPlayer()
	if err != nil {
		return nil, err
	}
	if err := app.All(
		player.SetAutoPlay(true),
		app.With(player.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetWidth(200), frame.SetHeight(200))
		}),
	); err != nil {
		return nil, err
	}

	fallback, err := dataTemplate(
		`<DataTemplate><Border Background="#33888888" CornerRadius="8">` +
			`<TextBlock Text="FallbackContent" HorizontalAlignment="Center" ` +
			`VerticalAlignment="Center"/></Border></DataTemplate>`)
	if err != nil {
		return nil, err
	}
	defer fallback.Release()
	if err := player.SetFallbackContent(fallback); err != nil {
		return nil, err
	}

	loaded, err := player.IsAnimatedVisualLoaded()
	if err != nil {
		return nil, err
	}
	duration, err := player.Duration()
	if err != nil {
		return nil, err
	}
	status, err := label(fmt.Sprintf(
		"IsAnimatedVisualLoaded=%v, Duration=%v — no Source is set, so the "+
			"FallbackContent is what renders.", loaded, duration.Duration))
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, player.AsUIElement)
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

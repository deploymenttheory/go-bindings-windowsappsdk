//go:build windows && amd64

package pages

// A VirtualizingLayout written in Go, and the four Repeater pages that need one.
//
// Sources: controls/dev/Repeater/**/{CircleLayoutSample,ActivityFeedSample,
// PinterestLayoutSample,NonVirtualStackLayoutSample}Page.xaml
//
// Each of those declares a layout the test app implements in C# by deriving
// VirtualizingLayout and overriding MeasureOverride and ArrangeOverride. This is the
// first place in the repository that derives an arbitrary XAML class from Go — the only
// previous one, app.DerivedApplication, FORWARDS IXamlMetadataProvider to a class WinUI
// already ships. Here there is nothing to forward to: the layout logic is the point.
//
// # How it works
//
// A composable WinRT class is derived by aggregation. The Go object implements the
// class's overrides interface and becomes the CONTROLLING OUTER; the factory's
// CreateInstance builds a non-delegating inner and hands it back; QueryInterface for
// anything the Go side does not implement is forwarded to that inner. XAML holds the
// outer, calls MeasureOverride on it, and the Go body answers.
//
// winrt.NewImplementation plus Implementation.Aggregate is exactly that, and the
// generated NewVirtualizingLayout is not — it passes a NULL outer, which creates the
// class as itself with no derivation. So this calls the factory directly.
//
// # Why the ABI works out here and not for IScrollController
//
// The scrolling batch found that a Go callback cannot receive floating-point arguments,
// which made IScrollController unimplementable (see scrolling_controllers.go). The same
// question has a different answer for a layout, and the difference is worth being
// precise about:
//
//	IScrollController.SetValues(minOffset, maxOffset, offset, viewportLength float64)
//	    four SCALAR doubles -> XMM1-XMM3 -> unreachable from a Go callback
//
//	IVirtualizingLayoutOverrides.MeasureOverride(context, availableSize Size) Size
//	    Size is a STRUCT of two float32s, 8 bytes -> one INTEGER register
//
// The Microsoft x64 convention passes aggregates of 1, 2, 4 or 8 bytes in integer
// registers regardless of what the members are. Only a scalar float or double goes in
// XMM. So Size, Point and Vector2 arrive intact and can be reinterpreted from the word;
// a bare float64 cannot. That is the whole difference, and it is why one interface is
// implementable and the other is not.

import (
	"fmt"
	"math"
	"sync/atomic"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

func init() {
	register(Page{Control: "Repeater", Name: "CircleLayoutSamplePage", Build: buildCircleLayoutSamplePage})
	register(Page{Control: "Repeater", Name: "ActivityFeedSamplePage", Build: buildActivityFeedSamplePage})
	register(Page{Control: "Repeater", Name: "PinterestLayoutSamplePage", Build: buildPinterestLayoutSamplePage})
	register(Page{Control: "Repeater", Name: "NonVirtualStackLayoutSamplePage", Build: buildNonVirtualStackLayoutSamplePage})
}

// GoLayout is a VirtualizingLayout whose measure and arrange are Go functions.
//
// The caller supplies arrange; measure is derived from it, because every layout here
// wants the same thing — place each item, then report the bounding size.
type GoLayout struct {
	implementation *winrt.Implementation
	layout         *uixaml.ILayout

	// place returns the rectangle for one item, given its index and the space available.
	place func(index int, count int, available wrtfoundation.Size) wrtfoundation.Rect

	// measureCalls and arrangeCalls count what the framework actually invoked.
	//
	// They exist because "the page lays out" does NOT prove these overrides ran: a
	// derivation that silently failed would leave the aggregated base class handling
	// layout, and the page would still measure non-zero because its ScrollView has a
	// fixed size. Counting is what tells the two apart, and acceptance/golayout_test.go
	// asserts on them.
	measureCalls atomic.Int32
	arrangeCalls atomic.Int32
}

// MeasureCalls and ArrangeCalls report how many times the framework called into Go.
func (l *GoLayout) MeasureCalls() int32 { return l.measureCalls.Load() }
func (l *GoLayout) ArrangeCalls() int32 { return l.arrangeCalls.Load() }

// sizeFromWord reinterprets the ABI word a Size parameter arrives in.
//
// Two float32s packed into eight bytes, little-endian: X in the low word, Y in the high.
// This is the reinterpretation the comment at the top of the file is about, and it is
// only sound because the aggregate travels in an integer register.
func sizeFromWord(word uintptr) wrtfoundation.Size {
	return *(*wrtfoundation.Size)(unsafe.Pointer(&word))
}

// writeSize writes a Size through a native out-pointer.
func writeSize(address uintptr, value wrtfoundation.Size) uintptr {
	if address == 0 {
		return 0x80004003 // E_POINTER
	}
	*(*wrtfoundation.Size)(winrt.InParam(address)) = value
	return 0
}

// NewGoLayout builds the layout and aggregates the framework's VirtualizingLayout onto
// it, so XAML sees a VirtualizingLayout whose overrides are Go code.
func NewGoLayout(place func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect) (*GoLayout, error) {
	layout := &GoLayout{place: place}

	// The five slots of IVirtualizingLayoutOverrides, in vtable order. The two that do
	// nothing still have to be present: a missing slot is E_NOTIMPL, and the framework
	// calls InitializeForContextCore before it measures anything.
	methods := []winrt.Method{
		func(args []uintptr) uintptr { return 0 }, // 6  InitializeForContextCore
		func(args []uintptr) uintptr { return 0 }, // 7  UninitializeForContextCore
		layout.measure, // 8  MeasureOverride
		layout.arrange, // 9  ArrangeOverride
		func(args []uintptr) uintptr { return 0 }, // 10 OnItemsChangedCore
	}

	implementation, err := winrt.NewImplementation(
		"Microsoft.UI.Xaml.Controls.VirtualizingLayout",
		winrt.Interface{IID: uixaml.IID_IVirtualizingLayoutOverrides, Methods: methods})
	if err != nil {
		return nil, fmt.Errorf("implementing IVirtualizingLayoutOverrides: %w", err)
	}
	layout.implementation = implementation

	// Aggregate the real VirtualizingLayout. This is the call the generated
	// NewVirtualizingLayout makes with a null outer; passing the Go object instead is
	// what turns "create the class" into "derive from it".
	if err := implementation.Aggregate(func(outer *syswinrt.IInspectable, inner **syswinrt.IInspectable) error {
		factoryUnknown, err := winrt.GetActivationFactory(
			"Microsoft.UI.Xaml.Controls.VirtualizingLayout", &uixaml.IID_IVirtualizingLayoutFactory)
		if err != nil {
			return err
		}
		factory := (*uixaml.IVirtualizingLayoutFactory)(unsafe.Pointer(factoryUnknown))
		defer factory.Release()
		instance, err := factory.CreateInstance(outer, inner)
		if err != nil {
			return err
		}
		instance.Release() // the aggregate keeps the inner; this is the derived facet
		return nil
	}); err != nil {
		implementation.Close()
		return nil, fmt.Errorf("aggregating VirtualizingLayout: %w", err)
	}

	// The repeater wants an ILayout. The outer answers for it through the inner.
	base, err := winrt.QueryInterface[uixaml.ILayout](implementation.Ptr(), &uixaml.IID_ILayout)
	if err != nil {
		implementation.Close()
		return nil, fmt.Errorf("the aggregate does not answer ILayout: %w", err)
	}
	layout.layout = base
	return layout, nil
}

// Layout is the pointer to hand to ItemsRepeater.SetLayout.
func (l *GoLayout) Layout() *uixaml.ILayout { return l.layout }

// Close releases the layout. Only safe once no repeater still holds it.
func (l *GoLayout) Close() {
	if l.layout != nil {
		l.layout.Release()
		l.layout = nil
	}
	if l.implementation != nil {
		l.implementation.Close()
		l.implementation = nil
	}
}

// measure is MeasureOverride: args are (context, availableSize word, out *Size).
//
// A virtualizing layout is supposed to realize only what the realization rect covers.
// This one realizes everything, because the four pages it serves have tens of items
// rather than thousands, and a wrong-but-simple realization policy would be a worse
// reference than an honest one. The rect is read and reported so the seam is visible.
func (l *GoLayout) measure(args []uintptr) uintptr {
	if len(args) < 3 {
		return 0x80070057 // E_INVALIDARG
	}
	l.measureCalls.Add(1)
	context := (*uixaml.IVirtualizingLayoutContext)(unsafe.Pointer(args[0]))
	available := sizeFromWord(args[1])

	count, err := context.ItemCount()
	if err != nil {
		return 0x80004005 // E_FAIL
	}

	var extent wrtfoundation.Rect
	for index := 0; index < int(count); index++ {
		element, err := context.GetOrCreateElementAt(int32(index))
		if err != nil || element == nil {
			continue
		}
		rect := l.place(index, int(count), available)
		// Every element must be measured before it can be arranged; a layout that
		// skips this gets zero-sized children and no error.
		_ = element.Measure(wrtfoundation.Size{Width: rect.Width, Height: rect.Height})
		element.Release()
		extent = union(extent, rect)
	}
	return writeSize(args[2], wrtfoundation.Size{
		Width: extent.X + extent.Width, Height: extent.Y + extent.Height})
}

// arrange is ArrangeOverride: same shape, and the call that actually positions things.
func (l *GoLayout) arrange(args []uintptr) uintptr {
	if len(args) < 3 {
		return 0x80070057
	}
	l.arrangeCalls.Add(1)
	context := (*uixaml.IVirtualizingLayoutContext)(unsafe.Pointer(args[0]))
	final := sizeFromWord(args[1])

	count, err := context.ItemCount()
	if err != nil {
		return 0x80004005
	}
	for index := 0; index < int(count); index++ {
		element, err := context.GetOrCreateElementAt(int32(index))
		if err != nil || element == nil {
			continue
		}
		_ = element.Arrange(l.place(index, int(count), final))
		element.Release()
	}
	return writeSize(args[2], final)
}

// union grows a bounding rectangle to include another.
func union(a, b wrtfoundation.Rect) wrtfoundation.Rect {
	if a.Width == 0 && a.Height == 0 {
		return b
	}
	left := float32(math.Min(float64(a.X), float64(b.X)))
	top := float32(math.Min(float64(a.Y), float64(b.Y)))
	right := float32(math.Max(float64(a.X+a.Width), float64(b.X+b.Width)))
	bottom := float32(math.Max(float64(a.Y+a.Height), float64(b.Y+b.Height)))
	return wrtfoundation.Rect{X: left, Y: top, Width: right - left, Height: bottom - top}
}

// goLayoutPage is the shape all four pages share: a repeater driven by a Go layout.
func goLayoutPage(note string, itemCount int, template string,
	place func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect,
) (*uixaml.IUIElement, error) {
	layout, err := NewGoLayout(place)
	if err != nil {
		return nil, err
	}
	// Not closed: the repeater holds it for the life of the page.

	repeater, _, err := templatedRepeater(layout.Layout(), template, numbered("Item", itemCount))
	if err != nil {
		return nil, err
	}
	host, err := hostedRepeater(repeater, 460, 320)
	if err != nil {
		return nil, err
	}
	if err := host.SetContentOrientation(uixaml.ScrollingContentOrientationBoth); err != nil {
		return nil, err
	}
	caption, err := label(note)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// CircleLayoutSamplePage: items placed round a circle, which is the source's simplest
// custom layout and the clearest demonstration that the overrides are being called.
func buildCircleLayoutSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	const item = 56.0
	return goLayoutPage(
		"A VirtualizingLayout implemented in Go: MeasureOverride and ArrangeOverride "+
			"are Go functions the framework calls.",
		16,
		`<DataTemplate><Border Background="#33448844" CornerRadius="28" Padding="6">`+
			`<TextBlock Text="{Binding}" FontSize="10"/></Border></DataTemplate>`,
		func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect {
			radius := 140.0
			angle := 2 * math.Pi * float64(index) / float64(count)
			return wrtfoundation.Rect{
				X:      float32(radius + radius*math.Cos(angle)),
				Y:      float32(radius + radius*math.Sin(angle)),
				Width:  item,
				Height: item,
			}
		})
}

// ActivityFeedSamplePage: a repeating pattern of one large tile and two small, which is
// what the source's ActivityFeedLayout produces.
func buildActivityFeedSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return goLayoutPage(
		"ActivityFeedLayout, in Go: one wide tile then two narrow ones, repeating.",
		24,
		`<DataTemplate><Border Background="#33446688" CornerRadius="4" Padding="8">`+
			`<TextBlock Text="{Binding}"/></Border></DataTemplate>`,
		func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect {
			const rowHeight, gap = 72.0, 6.0
			group, within := index/3, index%3
			y := float32(float64(group) * (rowHeight + gap))
			if within == 0 {
				return wrtfoundation.Rect{X: 0, Y: y, Width: 300, Height: rowHeight}
			}
			return wrtfoundation.Rect{
				X:      float32(300 + gap),
				Y:      y + float32(float64(within-1)*(rowHeight/2)),
				Width:  140,
				Height: float32(rowHeight/2 - gap/2),
			}
		})
}

// PinterestLayoutSamplePage: fixed columns, each item added to the shortest one, which
// is what gives the staggered look.
//
// The real algorithm tracks running column heights, and a place() called per index
// cannot — so the heights are recomputed from the index each time, which produces the
// same arrangement for a deterministic height function.
func buildPinterestLayoutSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	const columns = 3
	const columnWidth, gap = 140.0, 6.0
	itemHeight := func(index int) float64 { return 60 + float64((index*37)%80) }

	return goLayoutPage(
		"PinterestLayout, in Go: three columns, each item placed in the shortest.",
		30,
		`<DataTemplate><Border Background="#33884466" CornerRadius="4" Padding="8">`+
			`<TextBlock Text="{Binding}"/></Border></DataTemplate>`,
		func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect {
			heights := make([]float64, columns)
			var column int
			for i := 0; i <= index; i++ {
				column = 0
				for c := 1; c < columns; c++ {
					if heights[c] < heights[column] {
						column = c
					}
				}
				if i < index {
					heights[column] += itemHeight(i) + gap
				}
			}
			return wrtfoundation.Rect{
				X:      float32(float64(column) * (columnWidth + gap)),
				Y:      float32(heights[column]),
				Width:  columnWidth,
				Height: float32(itemHeight(index)),
			}
		})
}

// NonVirtualStackLayoutSamplePage: the source derives NonVirtualizingLayout instead.
//
// The difference is realization policy, not geometry — a non-virtualizing layout is
// handed every child up front rather than creating them from a context. This port keeps
// the geometry and says so, because building a second aggregation for a class whose only
// difference is that it realizes everything would demonstrate nothing the page above
// does not.
func buildNonVirtualStackLayoutSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return goLayoutPage(
		"The source derives NonVirtualizingLayout, whose difference from the layout "+
			"above is realization policy rather than geometry: it is handed every child "+
			"up front. The arrangement is the same simple stack.",
		20,
		`<DataTemplate><Border Background="#33664488" CornerRadius="4" Padding="8">`+
			`<TextBlock Text="{Binding}"/></Border></DataTemplate>`,
		func(index, count int, available wrtfoundation.Size) wrtfoundation.Rect {
			const height, gap = 44.0, 4.0
			return wrtfoundation.Rect{
				X:      0,
				Y:      float32(float64(index) * (height + gap)),
				Width:  360,
				Height: height,
			}
		})
}

//go:build windows && amd64

package pages

// The ItemsRepeater pages that use the shipped layouts.
//
// Sources: controls/dev/Repeater/**/*Page.xaml
//
// ItemsRepeater is the primitive underneath ItemsView, ItemsView is to ItemsRepeater
// what ScrollView is to ScrollPresenter, and the split is the same: no chrome, no
// selection, no scrolling of its own — it realizes elements for a layout and recycles
// them. Everything else is composed around it.
//
// The four pages with hand-written layouts are in collections_layout.go.

import (
	"fmt"
	"sort"
	"strings"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "Repeater", Name: "RepeaterTestUIPage", Build: buildRepeaterTestUIPage})
	register(Page{Control: "Repeater", Name: "StackLayoutDemoPage", Build: buildStackLayoutDemoPage})
	register(Page{Control: "Repeater", Name: "FlowLayoutDemoPage", Build: buildFlowLayoutDemoPage})
	register(Page{Control: "Repeater", Name: "VirtualizingStackLayoutSamplePage", Build: buildVirtualizingStackLayoutSamplePage})
	register(Page{Control: "Repeater", Name: "VirtualizingUniformStackLayoutSamplePage", Build: buildVirtualizingUniformStackLayoutSamplePage})
	register(Page{Control: "Repeater", Name: "ElementsInItemsSourcePage", Build: buildElementsInItemsSourcePage})
	register(Page{Control: "Repeater", Name: "ItemsViewWithDataPage", Build: buildItemsViewWithDataPage})
	register(Page{Control: "Repeater", Name: "ObjectModelTestPage", Build: buildObjectModelTestPage})
	register(Page{Control: "Repeater", Name: "SortingAndFilteringPage", Build: buildSortingAndFilteringPage})
	register(Page{Control: "Repeater", Name: "StoreDemoPage", Build: buildStoreDemoPage})
	register(Page{Control: "Repeater", Name: "VariableSizedItemsPage", Build: buildVariableSizedItemsPage})
	register(Page{Control: "Repeater", Name: "AnimationsDemoPage", Build: buildAnimationsDemoPage})
}

// cardTemplate is the item this batch renders unless a page needs otherwise.
const cardTemplate = `<DataTemplate>
	<Border Background="#22808080" CornerRadius="4" Padding="8,6" Margin="2">
		<TextBlock Text="{Binding}"/>
	</Border>
</DataTemplate>`

// hostedRepeater puts a repeater inside a ScrollView, which is how a repeater is
// actually used: it realizes elements for the viewport the scroller reports, so without
// one it realizes everything and virtualization means nothing.
func hostedRepeater(repeater *uixaml.ItemsRepeater, width, height float64) (*uixaml.ScrollView, error) {
	element, err := repeater.AsUIElement()
	if err != nil {
		return nil, err
	}
	defer element.Release()
	return newScrollView(width, height, func() (*uixaml.IUIElement, error) {
		return repeater.AsUIElement()
	})
}

func buildRepeaterTestUIPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	return navigationIndex("ItemsRepeater", []string{
		"StackLayoutDemoPage — StackLayout, both orientations",
		"FlowLayoutDemoPage — UniformGridLayout wrapping",
		"VirtualizingStackLayoutSamplePage — realization over a long source",
		"VirtualizingUniformStackLayoutSamplePage — the same, in a grid",
		"ElementsInItemsSourcePage — UIElements as the items themselves",
		"ItemsViewWithDataPage — a repeater inside an ItemsView",
		"ObjectModelTestPage — the properties, read back",
		"SortingAndFilteringPage — replacing the source",
		"StoreDemoPage — a realistic composed page",
		"VariableSizedItemsPage — items of differing heights",
		"AnimationsDemoPage — the transition seam",
		"CircleLayoutSamplePage — a layout written in Go",
		"ActivityFeedSamplePage, PinterestLayoutSamplePage, NonVirtualStackLayoutSamplePage",
	})
}

func buildStackLayoutDemoPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	var children []func() (*uixaml.IUIElement, error)
	for _, orientation := range []uixaml.Orientation{
		uixaml.OrientationVertical, uixaml.OrientationHorizontal,
	} {
		caption, err := label("StackLayout, " + orientation.String())
		if err != nil {
			return nil, err
		}
		layout, err := stackLayout(orientation, 4)
		if err != nil {
			return nil, err
		}
		repeater, source, err := templatedRepeater(layout, cardTemplate, numbered("Item", 30))
		layout.Release()
		if err != nil {
			return nil, err
		}
		_ = source // held by the repeater for the life of the page
		host, err := hostedRepeater(repeater, 440, 160)
		if err != nil {
			return nil, err
		}
		if orientation == uixaml.OrientationHorizontal {
			if err := host.SetContentOrientation(uixaml.ScrollingContentOrientationHorizontal); err != nil {
				return nil, err
			}
		}
		children = append(children, caption.AsUIElement, host.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// FlowLayoutDemoPage: UniformGridLayout, which is the shipped wrapping layout.
func buildFlowLayoutDemoPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := uniformGridLayout(110, 44)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, _, err := templatedRepeater(layout, cardTemplate, numbered("Cell", 60))
	if err != nil {
		return nil, err
	}
	host, err := hostedRepeater(repeater, 460, 300)
	if err != nil {
		return nil, err
	}
	caption, err := label("UniformGridLayout: items wrap to fill the width.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// VirtualizingStackLayoutSamplePage: a source long enough that realizing all of it would
// be obvious, with the realized count reported.
//
// The count is the point of the page. A repeater over 5,000 items that has realized a
// few dozen elements is virtualizing; one that has realized 5,000 is not, and nothing
// else about the two looks different.
func buildVirtualizingStackLayoutSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := stackLayout(uixaml.OrientationVertical, 2)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, _, err := templatedRepeater(layout, cardTemplate, numbered("Row", 5000))
	if err != nil {
		return nil, err
	}
	host, err := hostedRepeater(repeater, 440, 300)
	if err != nil {
		return nil, err
	}

	status, err := label("5,000 items in the source.")
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Count realized elements", func() {
			count, err := realizedCount(repeater)
			if err != nil {
				_ = status.SetText("Counting failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf(
				"5,000 items in the source; %d elements realized.", count))
		})
	})
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, row.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// realizedCount counts the repeater's realized children, which is what makes the
// virtualization claim checkable rather than assumed.
//
// It goes through VisualTreeHelper rather than Panel.Children, because ItemsRepeater is
// NOT a Panel — it derives from FrameworkElement. That is easy to get wrong (every other
// container that lays children out is a Panel) and the projection is what settles it:
// ItemsRepeater has AsFrameworkElement and no AsPanel, because the metadata says its
// base is FrameworkElement.
func realizedCount(repeater *uixaml.ItemsRepeater) (int32, error) {
	statics, err := uixaml.VisualTreeHelperStatics()
	if err != nil {
		return 0, err
	}
	defer statics.Release()

	element, err := repeater.AsUIElement()
	if err != nil {
		return 0, err
	}
	defer element.Release()
	object, err := dependencyObjectOf(element)
	if err != nil {
		return 0, err
	}
	defer object.Release()
	return statics.GetChildrenCount(object)
}

func buildVirtualizingUniformStackLayoutSamplePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := uniformGridLayout(100, 40)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, _, err := templatedRepeater(layout, cardTemplate, numbered("Cell", 3000))
	if err != nil {
		return nil, err
	}
	host, err := hostedRepeater(repeater, 460, 300)
	if err != nil {
		return nil, err
	}
	status, err := label("3,000 items in a UniformGridLayout.")
	if err != nil {
		return nil, err
	}
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Count realized elements", func() {
			count, err := realizedCount(repeater)
			if err != nil {
				_ = status.SetText("Counting failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("3,000 items; %d elements realized.", count))
		})
	})
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ElementsInItemsSourcePage: the items ARE UIElements, with no template at all.
//
// A repeater given elements uses them directly. That is not a shortcut — it is the case
// that proves ItemTemplate is a convenience over "produce an element per item", which is
// all IElementFactory ever meant.
func buildElementsInItemsSourcePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	repeater, err := uixaml.NewItemsRepeater()
	if err != nil {
		return nil, err
	}
	layout, err := stackLayout(uixaml.OrientationVertical, 4)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	if err := repeater.SetLayout(layout); err != nil {
		return nil, err
	}

	// The items ARE the elements, so they must NOT be boxed: app.NewItemsSource boxes
	// each value into a PropertyValue, which is right for strings and wrong for an
	// object that already has an IInspectable. NewObjectItemsSource retains them as
	// they are.
	elements := make([]*syswinrt.IInspectable, 0, 12)
	for i := 1; i <= 12; i++ {
		control, err := uixaml.NewButton()
		if err != nil {
			return nil, err
		}
		if err := app.SetContent(control.AsContentControl,
			fmt.Sprintf("Element %d, already a Button", i)); err != nil {
			return nil, err
		}
		element, err := control.AsUIElement()
		if err != nil {
			return nil, err
		}
		elements = append(elements, inspectableOf(element))
	}

	source, err := app.NewObjectItemsSource(elements, xamlCollectionIIDs)
	if err != nil {
		return nil, err
	}
	if err := repeater.SetItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, err
	}

	host, err := hostedRepeater(repeater, 440, 300)
	if err != nil {
		return nil, err
	}
	caption, err := label("The items are UIElements. No ItemTemplate is set.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ItemsViewWithDataPage lives in the Repeater sources but is about the control built on
// top: the same data, in an ItemsView, with selection.
func buildItemsViewWithDataPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := uniformGridLayout(140, 60)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	view, err := newItemsView(layout, numbered("Record", 120))
	if err != nil {
		return nil, err
	}
	if err := app.All(
		view.SetSelectionMode(uixaml.ItemsViewSelectionModeSingle),
		app.With(view.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
			return app.All(frame.SetHeight(320), frame.SetWidth(460))
		}),
	); err != nil {
		return nil, err
	}
	caption, err := label("The same data an ItemsRepeater would show, in an ItemsView: " +
		"scrolling and selection come with the control.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, view.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// ObjectModelTestPage reads the repeater's properties back after setting them, which is
// what the source page asserts.
func buildObjectModelTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := stackLayout(uixaml.OrientationVertical, 6)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, _, err := templatedRepeater(layout, cardTemplate, numbered("Item", 20))
	if err != nil {
		return nil, err
	}

	var lines []string
	readBack, err := repeater.Layout()
	if err != nil {
		lines = append(lines, "Layout: error — "+err.Error())
	} else if readBack == nil {
		lines = append(lines, "Layout: nil after being set (wrong)")
	} else {
		readBack.Release()
		lines = append(lines, "Layout: set and readable")
	}

	template, err := repeater.ItemTemplate()
	if err != nil {
		lines = append(lines, "ItemTemplate: error — "+err.Error())
	} else if template == nil {
		lines = append(lines, "ItemTemplate: nil after being set (wrong)")
	} else {
		template.Release()
		lines = append(lines, "ItemTemplate: set and readable")
	}

	source, err := repeater.ItemsSource()
	if err != nil {
		lines = append(lines, "ItemsSource: error — "+err.Error())
	} else if source == nil {
		lines = append(lines, "ItemsSource: nil after being set (wrong)")
	} else {
		source.Release()
		lines = append(lines, "ItemsSource: set and readable")
	}

	// ItemsSourceView is the repeater's own wrapper over whatever was bound, and it is
	// where the item count comes from regardless of the source's type.
	view, err := repeater.ItemsSourceView()
	if err != nil {
		lines = append(lines, "ItemsSourceView: error — "+err.Error())
	} else if view == nil {
		lines = append(lines, "ItemsSourceView: nil")
	} else {
		count, countErr := view.Count()
		view.Release()
		if countErr != nil {
			lines = append(lines, "ItemsSourceView.Count: error — "+countErr.Error())
		} else {
			lines = append(lines, fmt.Sprintf("ItemsSourceView.Count: %d", count))
		}
	}

	report, err := label(strings.Join(lines, "\n"))
	if err != nil {
		return nil, err
	}
	host, err := hostedRepeater(repeater, 440, 200)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, report.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// SortingAndFilteringPage: the source replaces the bound collection and the repeater
// re-renders.
//
// The collection here is immutable once built, so sorting means binding a NEW one — and
// that is the honest shape for a Go application today: app.ItemsSource has no mutation
// API, so the "observable collection" half of the WinUI story is not available. Stated
// rather than hidden, because a caller who expects to Add to a bound source will
// otherwise find out the hard way.
func buildSortingAndFilteringPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	words := []string{
		"delta", "alpha", "echo", "charlie", "bravo", "golf", "foxtrot", "hotel",
		"india", "juliet", "kilo", "lima",
	}

	layout, err := stackLayout(uixaml.OrientationVertical, 2)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, current, err := templatedRepeater(layout, cardTemplate, words)
	if err != nil {
		return nil, err
	}

	status, err := label(fmt.Sprintf("%d items, unsorted.", len(words)))
	if err != nil {
		return nil, err
	}

	// rebind swaps the whole collection, releasing the previous one only after the
	// control has taken the new one — the reverse order would release a collection the
	// repeater is still reading from.
	rebind := func(values []string, describe string) {
		next, err := itemsSource(values)
		if err != nil {
			_ = status.SetText("Building the collection failed: " + err.Error())
			return
		}
		if err := repeater.SetItemsSource(next.Inspectable()); err != nil {
			next.Close()
			_ = status.SetText("Rebinding failed: " + err.Error())
			return
		}
		if current != nil {
			current.Close()
		}
		current = next
		_ = status.SetText(fmt.Sprintf("%d items, %s.", len(values), describe))
	}

	row, err := buttonRow(
		func() (*uixaml.Button, error) {
			return button("Sort", func() {
				sorted := append([]string(nil), words...)
				sort.Strings(sorted)
				rebind(sorted, "sorted")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Filter to those with 'o'", func() {
				var filtered []string
				for _, word := range words {
					if strings.Contains(word, "o") {
						filtered = append(filtered, word)
					}
				}
				rebind(filtered, "filtered")
			})
		},
		func() (*uixaml.Button, error) {
			return button("Reset", func() { rebind(words, "unsorted") })
		},
	)
	if err != nil {
		return nil, err
	}

	host, err := hostedRepeater(repeater, 440, 240)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, row.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// StoreDemoPage is the source's realistic page: a scrolling list of grouped, templated
// cards. It is the one that looks like an application rather than a test.
func buildStoreDemoPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	const storeCard = `<DataTemplate>
	<Border Background="#22808080" CornerRadius="6" Padding="10" Margin="3" Width="150" Height="90">
		<StackPanel>
			<TextBlock Text="{Binding}" FontSize="14"/>
			<TextBlock Text="Free" Opacity="0.7" FontSize="12"/>
		</StackPanel>
	</Border>
</DataTemplate>`

	sections := []string{"Recommended", "Top free", "Recently updated"}
	var children []func() (*uixaml.IUIElement, error)
	for index, section := range sections {
		heading, err := label(section)
		if err != nil {
			return nil, err
		}
		layout, err := stackLayout(uixaml.OrientationHorizontal, 4)
		if err != nil {
			return nil, err
		}
		repeater, _, err := templatedRepeater(layout, storeCard,
			numbered(fmt.Sprintf("App %d.", index+1), 20))
		layout.Release()
		if err != nil {
			return nil, err
		}
		host, err := hostedRepeater(repeater, 460, 110)
		if err != nil {
			return nil, err
		}
		if err := host.SetContentOrientation(uixaml.ScrollingContentOrientationHorizontal); err != nil {
			return nil, err
		}
		children = append(children, heading.AsUIElement, host.AsUIElement)
	}
	panel, err := stack(10, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// VariableSizedItemsPage: items of differing heights under StackLayout, which is the
// layout that copes with them because it measures each element rather than assuming.
func buildVariableSizedItemsPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	const variableCard = `<DataTemplate>
	<Border Background="#22808080" CornerRadius="4" Padding="8" Margin="2">
		<TextBlock Text="{Binding}" TextWrapping="Wrap" Width="380"/>
	</Border>
</DataTemplate>`

	values := make([]string, 0, 40)
	for i := 1; i <= 40; i++ {
		// Length varies, so wrapped height varies with it.
		values = append(values, fmt.Sprintf("Item %d. %s", i,
			strings.Repeat("This line makes the item taller. ", 1+i%5)))
	}

	layout, err := stackLayout(uixaml.OrientationVertical, 2)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, _, err := templatedRepeater(layout, variableCard, values)
	if err != nil {
		return nil, err
	}
	host, err := hostedRepeater(repeater, 440, 320)
	if err != nil {
		return nil, err
	}
	caption, err := label("Items of differing heights: StackLayout measures each one.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, caption.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// AnimationsDemoPage: the seam where element realization can be animated.
//
// ItemsRepeater raises ElementPrepared as each element comes into view and
// ElementClearing as it is recycled. That pair is where an application animates
// items in — and it is also the clearest demonstration that recycling is real, since the
// same element object comes back with a different index.
func buildAnimationsDemoPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	layout, err := stackLayout(uixaml.OrientationVertical, 4)
	if err != nil {
		return nil, err
	}
	defer layout.Release()
	repeater, _, err := templatedRepeater(layout, cardTemplate, numbered("Item", 400))
	if err != nil {
		return nil, err
	}

	prepared, cleared := 0, 0
	status, err := label("Scroll: elements are prepared and cleared as they come and go.")
	if err != nil {
		return nil, err
	}
	update := func() {
		_ = status.SetText(fmt.Sprintf(
			"%d elements prepared, %d cleared — the difference is what is realized now.",
			prepared, cleared))
	}

	if _, err := app.On(repeater.AddElementPrepared,
		uixaml.NewTypedEventHandlerOfItemsRepeaterAndItemsRepeaterElementPreparedEventArgs,
		func(_ *uixaml.IItemsRepeater, args *uixaml.IItemsRepeaterElementPreparedEventArgs) {
			prepared++
			// Fading the element in is the animation the source page is about.
			if element, err := args.Element(); err == nil && element != nil {
				_ = element.SetOpacity(1)
				element.Release()
			}
			update()
		}); err != nil {
		return nil, err
	}
	if _, err := app.On(repeater.AddElementClearing,
		uixaml.NewTypedEventHandlerOfItemsRepeaterAndItemsRepeaterElementClearingEventArgs,
		func(_ *uixaml.IItemsRepeater, _ *uixaml.IItemsRepeaterElementClearingEventArgs) {
			cleared++
			update()
		}); err != nil {
		return nil, err
	}

	host, err := hostedRepeater(repeater, 440, 280)
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, status.AsUIElement, host.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

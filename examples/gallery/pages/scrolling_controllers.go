//go:build windows && amd64

package pages

// The three pages that need a Go-implemented IScrollController, and why they do not
// have one.
//
// Sources: controls/dev/ScrollPresenter/TestUI/ScrollPresenterWith{Simple,Composition,
// BiDirectional}ScrollController*Page.xaml
//
// Each of these replaces the presenter's scroll bars with a controller the test app
// writes itself — BiDirectionalScrollController.cs and friends — so the page IS the
// implementation. Everything the equivalent needs in Go is present: the aggregation
// machinery implements a WinRT interface (winrt.NewImplementation), IScrollController
// has 17 methods against a cap of 24, and the three event-args classes it must raise are
// all constructible through their activation factories.
//
// It is blocked one level below all of that, in Go itself.
//
// # The blocker
//
// A Go callback CANNOT RECEIVE FLOATING-POINT ARGUMENTS. From runtime/syscall_windows.go,
// in Go's own source:
//
//	if k := t.Kind(); GOARCH != "386" && (k == abi.Float32 || k == abi.Float64) {
//		// In fastcall, floating-point arguments in
//		// [...] floating-point registers, which we don't
//		// [support]
//		panic("compileCallback: float arguments not supported")
//	}
//
// On the Microsoft x64 convention a double parameter travels in XMM1-XMM3 by position,
// and Go's callback trampoline reads the integer registers only. go-bindings-winrt's
// trampoline declares every slot as taking eight uintptrs, so nothing panics — the
// float words simply never arrive, and the integer positions hold whatever the caller
// happened to leave in RDX, R8 and R9.
//
// IScrollController.SetValues is:
//
//	SetValues(minOffset, maxOffset, offset, viewportLength float64) error
//
// Four doubles, and it is THE call through which the presenter tells the controller what
// range it is scrolling over. A Go implementation receives garbage for it. That is worse
// than an unimplemented method: a controller that returns E_NOTIMPL declares itself
// incapable, while one that silently mis-reads its range renders a thumb in the wrong
// place and maps a drag to the wrong offset.
//
// The three source pages are precisely about that mapping, so there is nothing left of
// them once it is removed.
//
// # What was measured, and what was not
//
// A probe implemented all 17 slots and attached the result to a real ScrollPresenter.
// The presenter did call in: get_PanningInfo returned a second Go object implementing
// IScrollControllerPanningInfo, and add_ScrollToRequested returned a registration token,
// both successfully. The process then died with an access violation still inside
// SetVerticalScrollController.
//
// That second failure is NOT explained here. The probe returned null from
// PanningElementAncestor and from GetScrollAnimation, either of which the presenter may
// dereference, and the crash is consistent with that — but consistent is not the same as
// established, and this file is not going to assert a cause it did not test. What can be
// said is narrower and enough: attaching a Go controller reaches the framework and the
// framework calls back into it, and the float limitation blocks the interface's central
// method regardless of how that crash is resolved.
//
// # What is not the answer
//
// Widening winrt.MaxImplementedArgs, adding slots, or a different IID would change
// nothing: the words are not in the registers the trampoline can read. Fixing this means
// an assembly thunk that spills XMM1-XMM3 before entering Go — real, and a change to
// go-bindings-winrt rather than to a gallery page.
//
// The related case already recorded in CLAUDE.md is the same limitation seen from the
// other side: arm64 is excluded because Go's own assembly never loads V0-V7 when CALLING
// out. This is the inbound direction of that gap.

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// unimplementableController is the reason all three pages carry. It is one string so the
// three entries cannot drift apart.
const unimplementableController = "needs an IScrollController implemented in Go; " +
	"IScrollController.SetValues takes four float64 parameters and a Go callback cannot " +
	"receive floating-point arguments (runtime: \"compileCallback: float arguments not " +
	"supported\"), so the presenter's scroll range never arrives — see the file comment " +
	"in scrolling_controllers.go"

func init() {
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterWithSimpleScrollControllersPage",
		Unmappable: unimplementableController})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterWithCompositionScrollControllersPage",
		Unmappable: unimplementableController})
	register(Page{Control: "ScrollPresenter", Name: "ScrollPresenterWithBiDirectionalScrollControllerPage",
		Unmappable: unimplementableController})
	register(Page{Control: "ScrollPresenter", Name: "ScrollControllerSurfacePage",
		Build: buildScrollControllerSurfacePage})
}

// ScrollControllerSurfacePage is not a port. It is the part of the three blocked pages
// that CAN be shown: the controller contract as the framework exposes it, driven from
// the one implementation that ships — AnnotatedScrollBar's.
//
// It is registered rather than left out so the family is not silently three pages
// shorter than its source directory, and so the blocked capability has somewhere to be
// seen working from the native side.
func buildScrollControllerSurfacePage(ready *app.Ready) (*uixaml.IUIElement, error) {
	heading, err := label("The IScrollController contract")
	if err != nil {
		return nil, err
	}

	lines := []string{
		"A ScrollPresenter delegates its scroll bars to an IScrollController.",
		"",
		"The 17 members, in vtable order:",
		"   PanningInfo, CanScroll, IsScrollingWithMouse   — read by the presenter",
		"   SetIsScrollable, SetValues                     — the presenter tells the controller",
		"   GetScrollAnimation, NotifyRequestedScrollCompleted",
		"   CanScrollChanged, IsScrollingWithMouseChanged  — the controller tells the presenter",
		"   ScrollToRequested, ScrollByRequested, AddScrollVelocityRequested",
		"",
		"SetValues(minOffset, maxOffset, offset, viewportLength) is four float64s, and a",
		"Go callback cannot receive floating-point arguments — so this interface cannot",
		"be implemented correctly in Go today. The three source pages that implement one",
		"are registered as unmappable with that reason.",
		"",
		"AnnotatedScrollBarIScrollControllerPage shows the same contract working, because",
		"there both halves are native: the bar provides the controller, the presenter",
		"consumes it, and nothing crosses into Go.",
	}

	children := []func() (*uixaml.IUIElement, error){heading.AsUIElement}
	for _, line := range lines {
		text := line
		if text == "" {
			text = " "
		}
		block, err := label(text)
		if err != nil {
			return nil, err
		}
		children = append(children, block.AsUIElement)
	}

	// The one controller that does exist, shown to be real rather than described.
	bar, err := uixaml.NewAnnotatedScrollBar()
	if err != nil {
		return nil, err
	}
	controller, err := bar.ScrollController()
	if err != nil {
		return nil, err
	}
	defer controller.Release()
	canScroll, err := controller.CanScroll()
	if err != nil {
		return nil, err
	}
	observed, err := label("")
	if err != nil {
		return nil, err
	}
	if err := observed.SetText(
		"A live AnnotatedScrollBar's controller reports CanScroll = " +
			boolText(canScroll) + " before it is attached to anything."); err != nil {
		return nil, err
	}
	children = append(children, observed.AsUIElement)

	panel, err := stack(4, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

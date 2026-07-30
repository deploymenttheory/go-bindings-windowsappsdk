// Package verify pins the ABI facts this repository's code depends on against
// the committed winmds.
//
// Nothing here tests behaviour. It tests that the metadata still says what the
// code was written against: the IIDs passed to QueryInterface and
// RoGetActivationFactory, and the vtable slots dispatched through. Both are
// numbers with no redundancy — an IID off by a nibble, or a slot off by one,
// produces a call into the wrong function rather than an error — so a servicing
// release that moves either has to fail here, naming the type, instead of at a
// crash site a long way away.
//
// The generated bindings will take these values from the metadata directly and
// so cannot disagree with it. What these tests protect is the hand-written
// layer, and the assumption the generator itself is built on: that MethodDef
// order is vtable order, offset by the six IInspectable slots.
package verify

import (
	"path/filepath"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/metaquery"
)

// winmdDir is the committed metadata, relative to this package.
const winmdDir = "../../metadata/winmd"

var loaded *metaquery.Set

func metadata(t *testing.T) *metaquery.Set {
	t.Helper()
	if loaded == nil {
		set, err := metaquery.LoadDir(filepath.FromSlash(winmdDir))
		if err != nil {
			t.Fatalf("loading the committed winmds: %v", err)
		}
		loaded = set
	}
	return loaded
}

// pinnedIIDs are the interface identifiers the runtime layer activates and
// queries through. Written out in full rather than read from the metadata,
// because a test that derives its expectation from its subject asserts nothing.
var pinnedIIDs = map[string]string{
	// The startup path: Application.Start is static, the composable factory is
	// what a derived application would use (M6), and Window is what Start's
	// callback creates.
	"Microsoft.UI.Xaml.IApplicationStatics": "4e0d09f5-4358-512c-a987-503b52848e95",
	"Microsoft.UI.Xaml.IApplicationFactory": "9fd96657-5294-5a65-a1db-4fea143597da",
	"Microsoft.UI.Xaml.IWindow":             "61f0ec79-5d52-56b5-86fb-40fa4af288b0",

	// The only legal way to move work onto the UI thread from another
	// goroutine.
	"Microsoft.UI.Dispatching.IDispatcherQueue": "f6ebf8fa-be1c-5bf6-a467-73da28738ae8",

	// The click path the acceptance test exercises: Click is on IButtonBase,
	// not on IButton, and its handler is a RoutedEventHandler.
	"Microsoft.UI.Xaml.Controls.IButton":                "216c183d-d07a-5aa5-b8a4-0300a2683e87",
	"Microsoft.UI.Xaml.Controls.Primitives.IButtonBase": "65714269-2473-5327-a652-0ea6bce7f403",
	"Microsoft.UI.Xaml.Controls.IContentControl":        "07e81761-11b2-52ae-8f8b-4d53d2b5900a",

	// Delegates carry an IID like interfaces, and the Go-implemented handler
	// answers QueryInterface with it.
	"Microsoft.UI.Xaml.RoutedEventHandler":            "dae23d85-69ca-5bdf-805b-6161a3a215cc",
	"Microsoft.UI.Dispatching.DispatcherQueueHandler": "2e0872a9-4e29-5f14-b688-fb96d5f9d5f8",
}

func TestPinnedIIDs(t *testing.T) {
	set := metadata(t)
	for fullName, want := range pinnedIIDs {
		got, err := set.IID(fullName)
		if err != nil {
			t.Errorf("%s: %v", fullName, err)
			continue
		}
		if got != want {
			t.Errorf("%s IID = %s, pinned as %s", fullName, got, want)
		}
	}
}

// TestPinnedTypesExist covers the types reached by class name rather than by
// IID — a runtime class declares none — plus the resources dictionary whose
// absence at startup leaves every control unstyled.
func TestPinnedTypesExist(t *testing.T) {
	set := metadata(t)
	for _, fullName := range []string{
		"Microsoft.UI.Xaml.Application",
		"Microsoft.UI.Xaml.Window",
		"Microsoft.UI.Xaml.Controls.Button",
		"Microsoft.UI.Xaml.Controls.XamlControlsResources",
		"Microsoft.UI.Dispatching.DispatcherQueue",
		"Microsoft.UI.Dispatching.DispatcherQueueHandler",
	} {
		if _, ok := set.Type(fullName); !ok {
			t.Errorf("%s is not in the committed metadata", fullName)
		}
	}
}

// pinnedSlots are the {interface, method} → vtable slot pairs the startup and
// click paths dispatch through.
var pinnedSlots = []struct {
	iface  string
	method string
	slot   int
}{
	// Application.Start(callback) blocks for the life of the UI, and creates
	// the DispatcherQueueController itself — which is why
	// CreateDispatcherQueueController must not be called by hand.
	{"Microsoft.UI.Xaml.IApplicationStatics", "get_Current", 6},
	{"Microsoft.UI.Xaml.IApplicationStatics", "Start", 7},

	// The composable constructor a Go-derived Application would go through
	// (M6). Its shape — controlling outer in, inner out — is what needs COM
	// aggregation support in go-bindings-winrt.
	{"Microsoft.UI.Xaml.IApplicationFactory", "CreateInstance", 6},

	{"Microsoft.UI.Xaml.IWindow", "get_Content", 8},
	{"Microsoft.UI.Xaml.IWindow", "put_Content", 9},
	{"Microsoft.UI.Xaml.IWindow", "get_DispatcherQueue", 13},
	{"Microsoft.UI.Xaml.IWindow", "put_Title", 15},
	{"Microsoft.UI.Xaml.IWindow", "Activate", 26},
	{"Microsoft.UI.Xaml.IWindow", "Close", 27},

	// TryEnqueue is overloaded; the first overload takes the bare
	// DispatcherQueueHandler, which is the parameterless delegate
	// go-bindings-winrt v0.4.0 added support for.
	{"Microsoft.UI.Dispatching.IDispatcherQueue", "TryEnqueue", 7},

	// Click lives on IButtonBase, three classes up Button's inheritance chain.
	// This is the fact the base-class projection exists for: a Button typed as
	// its own default interface cannot reach it.
	{"Microsoft.UI.Xaml.Controls.Primitives.IButtonBase", "add_Click", 14},
	{"Microsoft.UI.Xaml.Controls.Primitives.IButtonBase", "remove_Click", 15},
	{"Microsoft.UI.Xaml.Controls.IButton", "get_Flyout", 6},

	// Delegates derive from IUnknown, not IInspectable: the vtable is
	// {QueryInterface, AddRef, Release, Invoke} and Invoke is slot 3. Numbering
	// them like interfaces would point three words past the end of a four-word
	// vtable.
	{"Microsoft.UI.Xaml.RoutedEventHandler", "Invoke", 3},
	{"Microsoft.UI.Dispatching.DispatcherQueueHandler", "Invoke", 3},
}

func TestPinnedSlots(t *testing.T) {
	set := metadata(t)
	for _, pin := range pinnedSlots {
		got, err := set.Slot(pin.iface, pin.method)
		if err != nil {
			t.Errorf("%s.%s: %v", pin.iface, pin.method, err)
			continue
		}
		if got != pin.slot {
			t.Errorf("%s.%s is slot %d, pinned as %d", pin.iface, pin.method, got, pin.slot)
		}
	}
}

// TestButtonInheritsItsClick states the projection problem as an assertion, so
// the base-class work has a failing-if-removed reason to exist: everything a
// Button is used for — Click, Content — is declared on interfaces its own
// default interface does not require.
func TestButtonInheritsItsClick(t *testing.T) {
	set := metadata(t)

	button, ok := set.Type("Microsoft.UI.Xaml.Controls.IButton")
	if !ok {
		t.Fatal("IButton not found")
	}
	for _, method := range button.Methods {
		if method.Name == "add_Click" {
			t.Error("IButton declares add_Click directly; the base-class projection " +
				"may no longer be needed for it — re-check the design")
		}
	}
	if len(button.Methods) != 2 {
		t.Errorf("IButton has %d methods, want 2 (get_Flyout, put_Flyout)", len(button.Methods))
	}

	// Where it actually lives.
	if _, err := set.Slot("Microsoft.UI.Xaml.Controls.Primitives.IButtonBase", "add_Click"); err != nil {
		t.Errorf("add_Click is not on IButtonBase either: %v", err)
	}
	if _, err := set.Slot("Microsoft.UI.Xaml.Controls.IContentControl", "put_Content"); err != nil {
		t.Errorf("put_Content is not on IContentControl: %v", err)
	}
}

// TestMembersWithoutSlotsAreRefused covers the other half of the slot
// contract. A delegate's .ctor and a runtime class's members appear in the
// MethodDef table but are not dispatched through any vtable, so asking for
// their slot has to fail: a caller that got a number back would index off the
// front of the vtable array and call whatever lay there.
func TestMembersWithoutSlotsAreRefused(t *testing.T) {
	set := metadata(t)
	for _, absent := range []struct{ fullName, method string }{
		{"Microsoft.UI.Xaml.RoutedEventHandler", ".ctor"},
		{"Microsoft.UI.Dispatching.DispatcherQueueHandler", ".ctor"},
		// A runtime class is reached through its interfaces; get_Flyout is
		// declared on Button but dispatched on IButton.
		{"Microsoft.UI.Xaml.Controls.Button", "get_Flyout"},
		{"Microsoft.UI.Xaml.Application", "Start"},
	} {
		if slot, err := set.Slot(absent.fullName, absent.method); err == nil {
			t.Errorf("%s.%s reported slot %d, but it has no vtable slot",
				absent.fullName, absent.method, slot)
		}
	}
}

// TestMethodDefOrderIsVtableOrder is the assumption the whole generator rests
// on. WinRT interfaces are laid out as the six IInspectable slots followed by
// the interface's own methods in MethodDef order, with no gaps — so a slot is
// derivable arithmetically and never has to be looked up. Assert it on the
// largest interface available rather than a convenient small one.
func TestMethodDefOrderIsVtableOrder(t *testing.T) {
	set := metadata(t)
	var widest metaquery.Type
	for _, resolved := range set.Types() {
		if resolved.Kind == metaquery.KindInterface && len(resolved.Methods) > len(widest.Methods) {
			widest = resolved
		}
	}
	if len(widest.Methods) == 0 {
		t.Fatal("no interfaces with methods in the committed metadata")
	}
	t.Logf("widest interface: %s (%d methods)", widest.FullName(), len(widest.Methods))
	for i, method := range widest.Methods {
		if want := 6 + i; method.Slot != want {
			t.Fatalf("%s.%s is slot %d, want %d — MethodDef order is no longer vtable order",
				widest.FullName(), method.Name, method.Slot, want)
		}
	}
}

// TestEveryInterfaceHasAnIID guards the generator's IID emission: an interface
// without a [Guid] cannot be queried for, and would emit a zero GUID that fails
// confusingly at run time. Report the offenders rather than a count.
func TestEveryInterfaceHasAnIID(t *testing.T) {
	set := metadata(t)
	var missing []string
	for _, resolved := range set.Types() {
		switch resolved.Kind {
		case metaquery.KindInterface, metaquery.KindDelegate:
			if resolved.IID == "" {
				missing = append(missing, resolved.FullName())
			}
		}
	}
	if len(missing) > 0 {
		limit := min(len(missing), 20)
		t.Errorf("%d interfaces/delegates declare no [Guid]: %v", len(missing), missing[:limit])
	}
}

package metaquery

import (
	"path/filepath"
	"strings"
	"testing"
)

// winmdDir is the committed metadata, relative to this package.
const winmdDir = "../../metadata/winmd"

func load(t *testing.T) *Set {
	t.Helper()
	set, err := LoadDir(filepath.FromSlash(winmdDir))
	if err != nil {
		t.Fatalf("LoadDir(%s): %v", winmdDir, err)
	}
	return set
}

func TestLoadDirMissing(t *testing.T) {
	if _, err := LoadDir(t.TempDir()); err == nil {
		t.Fatal("LoadDir on an empty directory returned no error")
	}
}

// TestKinds pins one type of each kind, because the classification decides
// which emitter a type reaches: an enum projected as a class, or a struct as an
// interface, is a whole file of wrong output.
func TestKinds(t *testing.T) {
	set := load(t)
	for fullName, want := range map[string]Kind{
		"Microsoft.UI.Xaml.IWindow":                 KindInterface,
		"Microsoft.UI.Xaml.Application":             KindClass,
		"Microsoft.UI.Xaml.Visibility":              KindEnum,
		"Microsoft.UI.Xaml.Thickness":               KindStruct,
		"Microsoft.UI.Xaml.RoutedEventHandler":      KindDelegate,
		"Microsoft.UI.Dispatching.IDispatcherQueue": KindInterface,
	} {
		resolved, ok := set.Type(fullName)
		if !ok {
			t.Errorf("%s not found", fullName)
			continue
		}
		if resolved.Kind != want {
			t.Errorf("%s kind = %q, want %q", fullName, resolved.Kind, want)
		}
	}
}

// TestTypeLookupIsCaseInsensitive matches the winmd's own tolerance: names
// arrive from command lines and hand-written call sites, where the casing is
// easy to get wrong and the failure would otherwise be "type not found".
func TestTypeLookupIsCaseInsensitive(t *testing.T) {
	set := load(t)
	upper, ok := set.Type("MICROSOFT.UI.XAML.IWINDOW")
	if !ok {
		t.Fatal("uppercased lookup failed")
	}
	if upper.Name != "IWindow" {
		t.Errorf("Name = %q, want IWindow (the metadata casing, not the query's)", upper.Name)
	}
}

func TestFullName(t *testing.T) {
	if got := (Type{Namespace: "Microsoft.UI.Xaml", Name: "IWindow"}).FullName(); got != "Microsoft.UI.Xaml.IWindow" {
		t.Errorf("FullName() = %q", got)
	}
	if got := (Type{Name: "Bare"}).FullName(); got != "Bare" {
		t.Errorf("FullName() with no namespace = %q, want Bare", got)
	}
}

func TestIID(t *testing.T) {
	set := load(t)
	iid, err := set.IID("Microsoft.UI.Xaml.IWindow")
	if err != nil {
		t.Fatalf("IID: %v", err)
	}
	if iid != "61f0ec79-5d52-56b5-86fb-40fa4af288b0" {
		t.Errorf("IID_IWindow = %s", iid)
	}
}

// TestIIDOnClassFails is the guard the doc comment promises: a runtime class
// declares no IID, and returning a zero GUID would surface as a confusing
// QueryInterface failure a long way from the cause.
func TestIIDOnClassFails(t *testing.T) {
	set := load(t)
	_, err := set.IID("Microsoft.UI.Xaml.Application")
	if err == nil {
		t.Fatal("IID of a runtime class returned no error")
	}
	if !strings.Contains(err.Error(), "declares no IID") {
		t.Errorf("error = %q, want it to say the type declares no IID", err)
	}
}

func TestIIDUnknownType(t *testing.T) {
	set := load(t)
	if _, err := set.IID("Microsoft.UI.Xaml.INoSuchInterface"); err == nil {
		t.Fatal("IID of an unknown type returned no error")
	}
}

// TestSlotsNamedOverloads covers the case Slot cannot answer: WinRT overloads
// share one metadata name, so TryEnqueue occupies two consecutive slots and a
// caller wanting the second has to ask for both.
func TestSlotsNamedOverloads(t *testing.T) {
	set := load(t)
	slots, err := set.SlotsNamed("Microsoft.UI.Dispatching.IDispatcherQueue", "TryEnqueue")
	if err != nil {
		t.Fatalf("SlotsNamed: %v", err)
	}
	if len(slots) != 2 || slots[0] != 7 || slots[1] != 8 {
		t.Fatalf("TryEnqueue slots = %v, want [7 8]", slots)
	}
	first, err := set.Slot("Microsoft.UI.Dispatching.IDispatcherQueue", "TryEnqueue")
	if err != nil {
		t.Fatalf("Slot: %v", err)
	}
	if first != slots[0] {
		t.Errorf("Slot = %d, want the first overload %d", first, slots[0])
	}
}

// TestSlotErrorListsMethods keeps the failure actionable: a mistyped method
// name is the common case, and the fix is visible in the message.
func TestSlotErrorListsMethods(t *testing.T) {
	set := load(t)
	_, err := set.Slot("Microsoft.UI.Xaml.IApplicationStatics", "Startt")
	if err == nil {
		t.Fatal("Slot on a missing method returned no error")
	}
	if !strings.Contains(err.Error(), "Start") {
		t.Errorf("error = %q, want it to list the type's real methods", err)
	}
}

// TestSiblingWinmdsAreNotExternal is the two-pass classification proof, and the
// reason Load defers the external decision until every file is read.
//
// Microsoft.UI.Xaml.winmd references Microsoft.UI.Dispatching,
// Microsoft.UI.Composition, Microsoft.UI.Input and more, none of which it
// defines — they live in Microsoft.UI.winmd, in this repository. Read one file
// at a time they look exactly like Windows.* references and would be projected
// as foreign; read as a set they are local.
func TestSiblingWinmdsAreNotExternal(t *testing.T) {
	set := load(t)
	external := set.External()

	for _, sibling := range []string{
		"Microsoft.UI",
		"Microsoft.UI.Composition",
		"Microsoft.UI.Content",
		"Microsoft.UI.Dispatching",
		"Microsoft.UI.Input",
		"Microsoft.UI.Text",
		"Microsoft.UI.Windowing",
		"Microsoft.Windows.ApplicationModel.Resources",
	} {
		if _, wrong := external[sibling]; wrong {
			t.Errorf("%s reported external, but this repository's winmds define it", sibling)
		}
		if set.Namespaces()[sibling] == 0 {
			t.Errorf("%s defines no types in the loaded set", sibling)
		}
	}

	// Genuinely external, and each resolves differently: Windows.* into
	// go-bindings-winrt, System.* not at all (metadata plumbing), and
	// Microsoft.Web.WebView2.Core into nothing — members using it must be
	// skipped with a distinct reason rather than quietly turned into uintptr.
	for _, foreign := range []string{
		"Windows.Foundation",
		"Windows.Foundation.Collections",
		"System",
		"Microsoft.Web.WebView2.Core",
	} {
		if external[foreign] == 0 {
			t.Errorf("%s not reported external", foreign)
		}
	}
}

// TestNamespacesCoverTheCommittedSurface pins the shape of the eventual package
// tree. The count is the sensor for an SDK bump adding or removing a namespace:
// it should be a reviewed change, not a silent one.
func TestNamespacesCoverTheCommittedSurface(t *testing.T) {
	set := load(t)
	namespaces := set.Namespaces()
	if len(namespaces) != 77 {
		t.Errorf("%d namespaces, want 77 (an SDK bump changed the surface — review, then update)", len(namespaces))
	}
	// The namespace WinUI 3 exists for, and the largest by some distance.
	if got := namespaces["Microsoft.UI.Xaml.Controls"]; got < 1000 {
		t.Errorf("Microsoft.UI.Xaml.Controls holds %d types, want >= 1000", got)
	}
}

// TestTypesAreOrdered guards the determinism the generator depends on: Types
// iterates a map, so it has to sort before returning.
func TestTypesAreOrdered(t *testing.T) {
	set := load(t)
	all := set.Types()
	if len(all) == 0 {
		t.Fatal("Types returned nothing")
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].FullName() >= all[i].FullName() {
			t.Fatalf("Types not sorted at %d: %q then %q", i, all[i-1].FullName(), all[i].FullName())
		}
	}
}

// TestMethodsFollowVtableOrder asserts the invariant every generated call
// depends on: methods arrive in MethodDef order, and slot numbering starts at 6
// because IInspectable occupies 0-5.
func TestMethodsFollowVtableOrder(t *testing.T) {
	set := load(t)
	window, ok := set.Type("Microsoft.UI.Xaml.IWindow")
	if !ok {
		t.Fatal("IWindow not found")
	}
	if len(window.Methods) == 0 {
		t.Fatal("IWindow has no methods")
	}
	if window.Methods[0].Slot != 6 {
		t.Errorf("first method is slot %d, want 6 (IInspectable occupies 0-5)", window.Methods[0].Slot)
	}
	for i, method := range window.Methods {
		if want := 6 + i; method.Slot != want {
			t.Errorf("%s is slot %d, want %d", method.Name, method.Slot, want)
		}
	}
}

// TestAttributesAreStrippedAndSorted covers the two things callers rely on:
// the Attribute suffix is gone, and GuidAttribute is consumed into IID rather
// than left in the list.
func TestAttributesAreStrippedAndSorted(t *testing.T) {
	set := load(t)
	window, ok := set.Type("Microsoft.UI.Xaml.IWindow")
	if !ok {
		t.Fatal("IWindow not found")
	}
	for i, attribute := range window.Attributes {
		if strings.HasSuffix(attribute, "Attribute") {
			t.Errorf("attribute %q keeps its Attribute suffix", attribute)
		}
		if attribute == "Guid" {
			t.Error("GuidAttribute leaked into Attributes instead of being consumed into IID")
		}
		if i > 0 && window.Attributes[i-1] > attribute {
			t.Errorf("attributes not sorted: %q then %q", window.Attributes[i-1], attribute)
		}
	}
	if len(window.Attributes) == 0 {
		t.Error("IWindow reports no attributes, but the metadata gives it ExclusiveTo")
	}
}

// TestSourceNamesTheWinmd matters for diagnostics: "which file did this come
// from" is the first question when a type appears twice or not at all.
func TestSourceNamesTheWinmd(t *testing.T) {
	set := load(t)
	for fullName, want := range map[string]string{
		"Microsoft.UI.Xaml.IWindow":                 "Microsoft.UI.Xaml.winmd",
		"Microsoft.UI.Dispatching.IDispatcherQueue": "Microsoft.UI.winmd",
		"Microsoft.UI.Text.FontWeights":             "Microsoft.UI.Text.winmd",
	} {
		resolved, ok := set.Type(fullName)
		if !ok {
			t.Errorf("%s not found", fullName)
			continue
		}
		if resolved.Source != want {
			t.Errorf("%s came from %q, want %q", fullName, resolved.Source, want)
		}
	}
}

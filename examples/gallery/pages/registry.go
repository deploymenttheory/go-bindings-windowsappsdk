//go:build windows && amd64

package pages

// Package pages is the gallery's page registry and the ported pages themselves.
//
// It is a library rather than part of the command, because two things consume it: the
// gallery shell in examples/gallery, and acceptance/gallery_test.go, which builds every
// registered page and asserts it lays out. A test cannot import a main package, and the
// conformance suite is the more important of the two consumers.
//
// One entry per page in microsoft/microsoft-ui-xaml's controls/dev/<Control>/TestUI —
// the pages WinUI's own test app drives. Porting them serves two ends at once: a Go
// developer can read how any control is driven, and every page is a case in
// acceptance/gallery_test.go, which builds all of them and asserts each lays out.
//
// The registry is the single source. The conformance test iterates it rather than
// listing pages of its own, so a page cannot be added without also being checked.

import (
	"fmt"
	"sort"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// Page is one ported source page.
type Page struct {
	// Control is the WinUI control directory the page came from ("Expander").
	Control string
	// Name is the source file's page name ("ExpanderPage"), so a reader can put the Go
	// beside the XAML it was ported from.
	Name string
	// Build constructs the page's content. Returning an element the caller owns and must
	// Release, exactly like every generated constructor.
	Build func(*app.Ready) (*uixaml.IUIElement, error)
	// Unmappable, when set, says why this page cannot be ported and leaves Build nil.
	// Recorded rather than omitted: a page missing from the registry looks like one
	// nobody has reached yet, which is a different thing from one that cannot be done.
	Unmappable string

	// Inert, when set, says why this page has no behaviour worth asserting.
	//
	// The bar for this corpus is FIDELITY TO ITS SOURCE: a page wires the handlers its
	// upstream TestUI page wires, no more and no fewer. Where the source has no
	// interaction — a styling fixture, an accessibility-scanner target, a page whose
	// whole subject is how something LOOKS — the port should have none either, and
	// adding one would make the port less faithful rather than more.
	//
	// Recorded rather than left blank, for the same reason Unmappable is: a page with
	// no assertion looks exactly like a page nobody has got to yet, and those need
	// different work. acceptance/coverage_test.go counts the pages that are neither
	// asserted nor excused, and that count may only fall.
	Inert string
}

// axeReason is why the *AxeTestPage ports have no behaviour to assert.
//
// Their upstream purpose is to be SCANNED, not driven: an accessibility checker walks the
// tree and reports on names, roles and contrast. The source pages wire no handlers, so a
// port that wired some would be less faithful, not more — and asserting an interaction
// here would be asserting something WinUI never claimed.
const axeReason = "an accessibility-scanner fixture: the source page is walked by a " +
	"checker rather than driven, and wires no handlers of its own"

// materialReason is why the brush pages have no behaviour to assert.
//
// Their subject is a BRUSH — what a surface looks like once acrylic or a radial gradient
// paints it. The source pages set brush properties and show the result; there is nothing
// to press, and the thing being demonstrated is not expressible through any automation
// pattern. Verified rather than assumed: the census finds zero live controls on every one
// of them.
const materialReason = "a brush page: its subject is how a surface is painted, which no " +
	"automation pattern can express, and the source wires nothing to drive"

// parallaxReason is why most ParallaxView pages have no behaviour to assert.
//
// A ParallaxView moves its layer in response to SCROLLING its source, so the effect is a
// consequence of scrolling rather than of any control. The pages that merely show that are
// inert; ParallaxViewDynamicPage, which exposes VerticalShift through buttons, is asserted
// by the behaviour suite instead.
const parallaxReason = "the parallax effect follows scrolling rather than any control, " +
	"and this page wires none — see ParallaxView/DynamicPage, which is asserted"

// pages is the registry, keyed by "Control/Name".
var pages = map[string]Page{}

// register adds a page, refusing a duplicate key rather than silently replacing one —
// two ports of the same page means one of them is not being tested.
func register(page Page) {
	key := page.Control + "/" + page.Name
	if _, exists := pages[key]; exists {
		panic("gallery: " + key + " registered twice")
	}
	if page.Build == nil && page.Unmappable == "" {
		panic("gallery: " + key + " has neither a Build nor a reason it has none")
	}
	pages[key] = page
}

// Pages returns every registered page in a stable order: by control, then by name.
func Pages() []Page {
	keys := make([]string, 0, len(pages))
	for key := range pages {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make([]Page, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, pages[key])
	}
	return ordered
}

// Buildable returns the pages that can actually be constructed.
func Buildable() []Page {
	var out []Page
	for _, page := range Pages() {
		if page.Build != nil {
			out = append(out, page)
		}
	}
	return out
}

// Lookup finds one page by its "Control/Name" key.
func Lookup(key string) (Page, error) {
	page, ok := pages[key]
	if !ok {
		return Page{}, fmt.Errorf("gallery: no page %q", key)
	}
	return page, nil
}

// Key is the page's registry key.
func (p Page) Key() string { return p.Control + "/" + p.Name }

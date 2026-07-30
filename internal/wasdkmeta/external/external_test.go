package external

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

var loaded *Set

// pinned loads the Windows.* universe out of the module the go.mod pins. It is
// cached because the read is a few hundred files.
func pinned(t *testing.T) *Set {
	t.Helper()
	if loaded == nil {
		set, err := Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		loaded = set
	}
	return loaded
}

// TestLocateResolvesThePin is the assumption everything else here rests on: the
// version go.mod pins is the version whose metadata gets read, so the bindings
// this module imports and the metadata it generates against cannot drift apart.
func TestLocateResolvesThePin(t *testing.T) {
	dir, version, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if version == "" {
		t.Error("Locate returned no version")
	}
	if !strings.Contains(filepath.ToSlash(dir), "go-bindings-winrt") {
		t.Errorf("dir = %q, want it inside the go-bindings-winrt module", dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the located directory does not exist: %v", err)
	}
}

func TestLoadOverrideDirectory(t *testing.T) {
	dir, _, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	set, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%q): %v", dir, err)
	}
	if set.Version != "(local)" {
		t.Errorf("Version = %q, want (local) for an explicit directory", set.Version)
	}
	if set.Len() == 0 {
		t.Error("the set defines no types")
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("Load on a missing directory returned no error")
	}
}

// TestLoadRejectsAnIncompatibleSchema pins the failure mode that matters when
// the dependency is bumped: if go-bindings-winrt changes its IR format, this has
// to say so rather than silently read a subset of the fields it expects.
func TestLoadRejectsAnIncompatibleSchema(t *testing.T) {
	dir := t.TempDir()
	stale := `{"namespace":"Windows.Nope","schema_version":99}`
	if err := os.WriteFile(filepath.Join(dir, "Windows.Nope"+fileExtension), []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil {
		t.Fatal("Load accepted an incompatible schema version")
	}
	if !errors.Is(err, wasdkmeta.ErrSchemaMismatch) {
		t.Errorf("error = %v, want it to wrap ErrSchemaMismatch", err)
	}
}

// TestKindsCoverEveryConstruct checks the classification the ingest stage relies
// on: a reference whose kind is wrong reaches the wrong emitter.
func TestKindsCoverEveryConstruct(t *testing.T) {
	set := pinned(t)
	for fullName, want := range map[string]string{
		// The types Windows App SDK metadata leans on hardest.
		"Windows.Foundation.TypedEventHandler`2":      "Delegate",
		"Windows.Foundation.IAsyncAction":             "Interface",
		"Windows.Foundation.Rect":                     "Struct",
		"Windows.Foundation.Point":                    "Struct",
		"Windows.Foundation.Uri":                      "Class",
		"Windows.Foundation.Collections.IVector`1":    "Interface",
		"Windows.UI.Xaml.Interop.TypeKind":            "Enum",
		"Windows.Storage.Streams.IRandomAccessStream": "Interface",
	} {
		if got := set.Kind(fullName); got != want {
			t.Errorf("Kind(%s) = %q, want %q", fullName, got, want)
		}
	}
}

func TestKindOfUnknownType(t *testing.T) {
	if got := pinned(t).Kind("Windows.Foundation.INoSuchThing"); got != "" {
		t.Errorf("Kind of an unknown type = %q, want empty", got)
	}
}

// TestDefinesDistinguishesTheRoots is the check that keeps the two "Windows"
// prefixes apart. Windows.Storage is go-bindings-winrt's; Microsoft.Windows.
// Storage is this repository's, and confusing them would resolve a local type to
// a foreign package.
func TestDefinesDistinguishesTheRoots(t *testing.T) {
	set := pinned(t)
	for _, external := range []string{
		"Windows.Foundation",
		"Windows.Foundation.Collections",
		"Windows.Storage",
		"Windows.UI.Xaml.Interop",
	} {
		if !set.Defines(external) {
			t.Errorf("Defines(%s) = false, want true", external)
		}
	}
	for _, local := range []string{
		"Microsoft.UI.Xaml",
		"Microsoft.UI.Xaml.Controls",
		"Microsoft.Windows.Storage",
		"Microsoft.Web.WebView2.Core",
	} {
		if set.Defines(local) {
			t.Errorf("Defines(%s) = true, but that namespace is not go-bindings-winrt's", local)
		}
	}
}

// TestResolversAgreeWithKind guards against an index being populated by one
// path and read by another: Kind and the typed lookups must never disagree.
func TestResolversAgreeWithKind(t *testing.T) {
	set := pinned(t)
	cases := []struct {
		namespace, name, kind string
		found                 func() bool
	}{
		{"Windows.Foundation", "IAsyncAction", "Interface", func() bool {
			return set.Interface("Windows.Foundation", "IAsyncAction") != nil
		}},
		{"Windows.Foundation", "Uri", "Class", func() bool {
			return set.Class("Windows.Foundation", "Uri") != nil
		}},
		{"Windows.Foundation", "Rect", "Struct", func() bool {
			return set.Struct("Windows.Foundation", "Rect") != nil
		}},
		{"Windows.Foundation", "TypedEventHandler`2", "Delegate", func() bool {
			return set.Delegate("Windows.Foundation", "TypedEventHandler`2") != nil
		}},
		{"Windows.UI.Xaml.Interop", "TypeKind", "Enum", func() bool {
			return set.Enum("Windows.UI.Xaml.Interop", "TypeKind") != nil
		}},
	}
	for _, c := range cases {
		if got := set.Kind(c.namespace + "." + c.name); got != c.kind {
			t.Errorf("Kind(%s.%s) = %q, want %q", c.namespace, c.name, got, c.kind)
		}
		if !c.found() {
			t.Errorf("the %s resolver did not find %s.%s", c.kind, c.namespace, c.name)
		}
	}
}

// TestShapeIsUsableForEmission checks the fields the emit stage will actually
// read off an external type. Reading the metadata is only useful if it carries
// enough to reproduce go-bindings-winrt's own projection decisions.
func TestShapeIsUsableForEmission(t *testing.T) {
	set := pinned(t)

	// A closed instantiation of this is what every XAML event carries, so its
	// Invoke signature has to be there to ground a handler from.
	handler := set.Delegate("Windows.Foundation", "TypedEventHandler`2")
	if handler == nil {
		t.Fatal("TypedEventHandler`2 not found")
	}
	if handler.Arity != 2 {
		t.Errorf("TypedEventHandler`2 arity = %d, want 2", handler.Arity)
	}
	if handler.GUID == "" {
		t.Error("TypedEventHandler`2 has no GUID; a grounded handler could not answer QueryInterface")
	}
	if len(handler.Invoke.Params) != 2 {
		t.Errorf("Invoke takes %d params, want 2 (sender, args)", len(handler.Invoke.Params))
	}

	// A class reference lowers to its default interface, so that has to be
	// recorded or every such reference would degrade.
	uri := set.Class("Windows.Foundation", "Uri")
	if uri == nil {
		t.Fatal("Windows.Foundation.Uri not found")
	}
	if uri.DefaultInterface == nil {
		t.Error("Uri has no default interface; references to it could not be typed")
	}

	// Structs are embedded by value, so their fields must be present to size
	// them for the calling convention.
	rect := set.Struct("Windows.Foundation", "Rect")
	if rect == nil {
		t.Fatal("Windows.Foundation.Rect not found")
	}
	if len(rect.Fields) != 4 {
		t.Errorf("Rect has %d fields, want 4 (X, Y, Width, Height)", len(rect.Fields))
	}
}

// TestNamespacesAreSortedAndComplete keeps the ordering deterministic, since
// callers use it to report and to iterate.
func TestNamespacesAreSortedAndComplete(t *testing.T) {
	set := pinned(t)
	namespaces := set.Namespaces()
	if len(namespaces) == 0 {
		t.Fatal("no namespaces")
	}
	for i := 1; i < len(namespaces); i++ {
		if namespaces[i-1] >= namespaces[i] {
			t.Fatalf("namespaces not sorted at %d: %q then %q", i, namespaces[i-1], namespaces[i])
		}
	}
	for _, namespace := range namespaces {
		if set.Meta(namespace) == nil {
			t.Errorf("Meta(%s) is nil although it is listed", namespace)
		}
		if !strings.HasPrefix(namespace, "Windows.") {
			t.Errorf("%s is not under the Windows. root", namespace)
		}
	}
}

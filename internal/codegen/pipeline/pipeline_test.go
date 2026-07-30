package pipeline

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

const metadataDir = "../../../metadata/wasdk"

var cachedRegistry *Registry

func registry(t *testing.T) *Registry {
	t.Helper()
	if cachedRegistry != nil {
		return cachedRegistry
	}
	externalSet, err := external.Load("")
	if err != nil {
		t.Fatalf("loading the Windows.* universe: %v", err)
	}
	loaded, err := Load(metadataDir, externalSet)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cachedRegistry = loaded
	return cachedRegistry
}

func TestLoadRejectsAnEmptyDirectory(t *testing.T) {
	if _, err := Load(t.TempDir(), nil); err == nil {
		t.Fatal("Load on an empty directory returned no error")
	}
}

func TestLoadIndexesEveryConstruct(t *testing.T) {
	reg := registry(t)
	if len(reg.Namespaces) != 77 {
		t.Errorf("%d namespaces, want 77", len(reg.Namespaces))
	}
	if reg.Interface("Microsoft.UI.Xaml", "IWindow") == nil {
		t.Error("IWindow not indexed")
	}
	if reg.Class("Microsoft.UI.Xaml.Controls", "Button") == nil {
		t.Error("Button not indexed")
	}
	if reg.Struct("Microsoft.UI.Xaml", "Thickness") == nil {
		t.Error("Thickness not indexed")
	}
	if reg.Delegate("Microsoft.UI.Xaml", "RoutedEventHandler") == nil {
		t.Error("RoutedEventHandler not indexed")
	}
	if got := reg.EnumBase("Microsoft.UI.Xaml", "Visibility"); got != "int32" {
		t.Errorf("Visibility base = %q, want int32", got)
	}
}

// TestResolutionSpansBothModules is what the Registry exists for: a caller asks
// what a type is without first deciding which module owns it, while IsExternal
// answers the separate question of which package to import.
func TestResolutionSpansBothModules(t *testing.T) {
	reg := registry(t)

	if reg.Interface("Windows.Foundation", "IAsyncAction") == nil {
		t.Error("an external interface did not resolve")
	}
	if reg.Struct("Windows.Foundation", "Rect") == nil {
		t.Error("an external struct did not resolve")
	}
	if reg.Delegate("Windows.Foundation", "TypedEventHandler`2") == nil {
		t.Error("an external delegate did not resolve")
	}

	if !reg.IsLocal("Microsoft.UI.Xaml") || reg.IsExternal("Microsoft.UI.Xaml") {
		t.Error("Microsoft.UI.Xaml is not reported local-only")
	}
	if !reg.IsExternal("Windows.Foundation") || reg.IsLocal("Windows.Foundation") {
		t.Error("Windows.Foundation is not reported external-only")
	}
	// The prefix trap: Microsoft.Windows.Storage is ours, Windows.Storage is not.
	if !reg.IsLocal("Microsoft.Windows.Storage") {
		t.Error("Microsoft.Windows.Storage should be local")
	}
	if reg.IsLocal("Windows.Storage") {
		t.Error("Windows.Storage should not be local")
	}
	// Neither module has it.
	if reg.IsLocal("Microsoft.Web.WebView2.Core") || reg.IsExternal("Microsoft.Web.WebView2.Core") {
		t.Error("a WebView2 namespace is claimed by one of the modules")
	}
}

func TestUnknownTypesResolveToNil(t *testing.T) {
	reg := registry(t)
	if reg.Interface("Microsoft.UI.Xaml", "INoSuchThing") != nil {
		t.Error("an unknown interface resolved")
	}
	if reg.EnumBase("Microsoft.UI.Xaml", "NoSuchEnum") != "" {
		t.Error("an unknown enum reported a base type")
	}
}

// TestBlockedImportsBreakEveryCycle is the property that has to hold: Go rejects
// an import cycle outright, so after severing, the remaining graph must be
// acyclic. Rebuild the graph minus the severed edges and confirm.
func TestBlockedImportsBreakEveryCycle(t *testing.T) {
	reg := registry(t)
	blocked := ComputeBlockedImports(reg)

	edges := map[string]map[string]int{}
	for _, meta := range reg.Namespaces {
		weights := map[string]int{}
		wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
			if ref.Kind == "Native" || ref.Namespace == "" || ref.Namespace == meta.Namespace {
				return
			}
			if !reg.IsLocal(ref.Namespace) || blocked[meta.Namespace][ref.Namespace] {
				return
			}
			weights[ref.Namespace]++
		})
		if len(weights) > 0 {
			edges[meta.Namespace] = weights
		}
	}
	if cycle := findCycle(edges); cycle != nil {
		t.Errorf("a cycle survives the severed edges: %v", cycle)
	}
	t.Logf("%d source namespaces have severed edges", len(blocked))
}

// TestBlockedImportsAreDeterministic guards regeneration: the same metadata must
// sever the same edges, or the emitted tree would differ between runs.
func TestBlockedImportsAreDeterministic(t *testing.T) {
	reg := registry(t)
	first := ComputeBlockedImports(reg)
	second := ComputeBlockedImports(reg)
	if len(first) != len(second) {
		t.Fatalf("%d vs %d source namespaces", len(first), len(second))
	}
	for src, targets := range first {
		for dst := range targets {
			if !second[src][dst] {
				t.Errorf("%s → %s was severed on one run only", src, dst)
			}
		}
	}
}

// TestExternalEdgesAreNeverSevered matters because an import into
// go-bindings-winrt cannot close a cycle — nothing there imports this module — so
// degrading such a reference would lose a member for no reason.
func TestExternalEdgesAreNeverSevered(t *testing.T) {
	reg := registry(t)
	for src, targets := range ComputeBlockedImports(reg) {
		for dst := range targets {
			if reg.IsExternal(dst) {
				t.Errorf("%s → %s was severed, but an external import cannot form a cycle", src, dst)
			}
			if !reg.IsLocal(src) || !reg.IsLocal(dst) {
				t.Errorf("%s → %s involves a namespace this module does not emit", src, dst)
			}
		}
	}
}

// TestDefaultInterfaceEdgesResistSevering checks the weighting. Severing the edge
// a class's default interface crosses demotes the whole class, since the generated
// struct embeds that interface — so those edges carry a large bonus and should
// survive whenever a lighter edge in the same cycle can go instead.
func TestDefaultInterfaceEdgesResistSevering(t *testing.T) {
	reg := registry(t)
	blocked := ComputeBlockedImports(reg)

	var severedEmbeds int
	for _, meta := range reg.Namespaces {
		for name := range meta.Classes {
			class := meta.Classes[name]
			if class.DefaultInterface == nil {
				continue
			}
			target := class.DefaultInterface.Namespace
			if target == "" || target == meta.Namespace || !reg.IsLocal(target) {
				continue
			}
			if blocked[meta.Namespace][target] {
				severedEmbeds++
			}
		}
	}
	// Not zero-by-construction — a cycle made only of embedding edges would force
	// one — but it should be a small fraction of the classes, not routine.
	if severedEmbeds > 50 {
		t.Errorf("%d classes lost their default-interface import; the embed weight is not doing its job", severedEmbeds)
	}
	t.Logf("%d classes affected by a severed default-interface edge", severedEmbeds)
}

func TestReadRootsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "emit-roots.txt")
	content := "# a comment\n\nMicrosoft.UI.Xaml\n  Microsoft.UI.Xaml.Controls  \n# another\nMicrosoft.UI.Text\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	roots, err := ReadRootsFile(path)
	if err != nil {
		t.Fatalf("ReadRootsFile: %v", err)
	}
	want := []string{"Microsoft.UI.Xaml", "Microsoft.UI.Xaml.Controls", "Microsoft.UI.Text"}
	if len(roots) != len(want) {
		t.Fatalf("got %v, want %v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Errorf("root %d = %q, want %q", i, roots[i], want[i])
		}
	}
}

func TestReadRootsFileMissing(t *testing.T) {
	_, err := ReadRootsFile(filepath.Join(t.TempDir(), "absent.txt"))
	if !os.IsNotExist(err) {
		t.Errorf("error = %v, want a not-exist error the caller can distinguish", err)
	}
}

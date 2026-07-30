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

// xamlCluster is the one mutually recursive group in the Windows App SDK metadata: the
// XAML core, whose namespaces reference each other in every direction.
var xamlCluster = []string{
	"Microsoft.UI.Xaml",
	"Microsoft.UI.Xaml.Automation",
	"Microsoft.UI.Xaml.Automation.Peers",
	"Microsoft.UI.Xaml.Automation.Provider",
	"Microsoft.UI.Xaml.Controls",
	"Microsoft.UI.Xaml.Controls.Primitives",
	"Microsoft.UI.Xaml.Data",
	"Microsoft.UI.Xaml.Documents",
	"Microsoft.UI.Xaml.Input",
	"Microsoft.UI.Xaml.Media",
	"Microsoft.UI.Xaml.Media.Animation",
	"Microsoft.UI.Xaml.Media.Imaging",
	"Microsoft.UI.Xaml.Media.Media3D",
	"Microsoft.UI.Xaml.Navigation",
}

// TestXamlNamespacesFormOneCluster pins the shape of the metadata that motivates
// clustering at all. Written out in full rather than derived, so a servicing release
// that changes the recursion is a reviewed diff and not a silent repackaging: a
// namespace joining or leaving this list MOVES types between Go packages.
func TestXamlNamespacesFormOneCluster(t *testing.T) {
	clusters := ComputeClusters(registry(t))

	merged := clusters.Merged()
	if len(merged) != 1 {
		t.Fatalf("%d merged packages, want exactly 1: %v", len(merged), merged)
	}
	if merged[0] != "Microsoft.UI.Xaml" {
		t.Errorf("the cluster is named %s, want Microsoft.UI.Xaml — the common root, which is\n"+
			"also the package name users import", merged[0])
	}

	members := clusters.Members(merged[0])
	if len(members) != len(xamlCluster) {
		t.Fatalf("cluster has %d namespaces, want %d:\n got %v\nwant %v",
			len(members), len(xamlCluster), members, xamlCluster)
	}
	for i := range xamlCluster {
		if members[i] != xamlCluster[i] {
			t.Errorf("member %d is %s, want %s", i, members[i], xamlCluster[i])
		}
	}
	for _, namespace := range xamlCluster {
		if got := clusters.PackageOf(namespace); got != "Microsoft.UI.Xaml" {
			t.Errorf("PackageOf(%s) = %s, want Microsoft.UI.Xaml", namespace, got)
		}
	}
}

// TestAcyclicNamespacesKeepTheirOwnPackage is the other half: clustering must not
// collapse namespaces that had no reason to merge. 63 of the 77 stay independent.
func TestAcyclicNamespacesKeepTheirOwnPackage(t *testing.T) {
	reg := registry(t)
	clusters := ComputeClusters(reg)

	inCluster := map[string]bool{}
	for _, namespace := range xamlCluster {
		inCluster[namespace] = true
	}
	var independent int
	for _, meta := range reg.Namespaces {
		if inCluster[meta.Namespace] {
			continue
		}
		independent++
		if got := clusters.PackageOf(meta.Namespace); got != meta.Namespace {
			t.Errorf("%s was merged into %s, but it is not part of a reference cycle",
				meta.Namespace, got)
		}
	}
	if independent != len(reg.Namespaces)-len(xamlCluster) {
		t.Errorf("%d independent namespaces, want %d", independent, len(reg.Namespaces)-len(xamlCluster))
	}
}

// TestClusteringLeavesNothingToSever is the payoff, stated as an invariant.
//
// Collapsing every strongly-connected component makes the package graph acyclic by
// construction, so the cycle breaker finds nothing. That matters because severing was
// never cost-free: the cheapest edge by reference count was
// Microsoft.UI.Xaml -> Microsoft.UI.Xaml.Input, and cutting it removed every pointer,
// keyboard and manipulation event on UIElement, because their argument types live
// there.
//
// If this ever fails, some reference edge is not being counted when components are
// computed — which is a bug in localReferenceGraph, not a reason to start severing
// again.
func TestClusteringLeavesNothingToSever(t *testing.T) {
	reg := registry(t)
	clusters := ComputeClusters(reg)
	blocked := ComputeBlockedImports(reg, clusters)
	if len(blocked) != 0 {
		for src, targets := range blocked {
			for dst := range targets {
				t.Errorf("package %s -> %s was severed; an edge is missing from the component "+
					"computation", src, dst)
			}
		}
	}
}

// TestClustersAreDeterministic guards regeneration: the same metadata must produce the
// same packages, or the committed tree would differ between runs.
func TestClustersAreDeterministic(t *testing.T) {
	reg := registry(t)
	first, second := ComputeClusters(reg), ComputeClusters(reg)
	for _, meta := range reg.Namespaces {
		if a, b := first.PackageOf(meta.Namespace), second.PackageOf(meta.Namespace); a != b {
			t.Errorf("%s mapped to %s then %s", meta.Namespace, a, b)
		}
	}
	if len(first.Merged()) != len(second.Merged()) {
		t.Error("the merged-package set differs between runs")
	}
}

// TestExternalNamespacesAreNeverClustered matters because a reference into
// go-bindings-winrt cannot close a cycle here — nothing in that module imports this one
// — and merging one of its namespaces into a local package would be nonsense: this
// module cannot add files to it.
func TestExternalNamespacesAreNeverClustered(t *testing.T) {
	reg := registry(t)
	clusters := ComputeClusters(reg)
	for _, namespace := range []string{
		"Windows.Foundation",
		"Windows.Foundation.Collections",
		"Windows.UI.Xaml.Interop",
	} {
		if got := clusters.PackageOf(namespace); got != namespace {
			t.Errorf("external namespace %s was clustered into %s", namespace, got)
		}
	}
}

// TestSamePackageIsWhatMakesReferencesLocal covers the predicate the typemap keys on:
// two namespaces in one cluster need no import between them, which is the whole
// mechanism by which the cycles stop mattering.
func TestSamePackageIsWhatMakesReferencesLocal(t *testing.T) {
	clusters := ComputeClusters(registry(t))
	if !clusters.SamePackage("Microsoft.UI.Xaml", "Microsoft.UI.Xaml.Input") {
		t.Error("Microsoft.UI.Xaml and .Input are not in one package, so UIElement's input " +
			"events cannot name their argument types")
	}
	if !clusters.SamePackage("Microsoft.UI.Xaml.Controls", "Microsoft.UI.Xaml.Controls.Primitives") {
		t.Error("Controls and Controls.Primitives are not in one package, so Button cannot " +
			"reach ButtonBase")
	}
	if clusters.SamePackage("Microsoft.UI.Xaml", "Microsoft.UI.Dispatching") {
		t.Error("Microsoft.UI.Dispatching was merged with the XAML cluster; it has no cycle with it")
	}
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

// TestBaseChainWalk covers the traversal both the emitter and the cycle breaker
// depend on. Walking it wrong in either place produces an import cycle in generated
// code that the breaker believed did not exist.
func TestBaseChainWalk(t *testing.T) {
	reg := registry(t)
	controls := reg.ByNamespace["Microsoft.UI.Xaml.Controls"]
	if controls == nil {
		t.Fatal("Microsoft.UI.Xaml.Controls is not loaded")
	}
	button, ok := controls.Classes["Button"]
	if !ok {
		t.Fatal("Button is not in the registry")
	}
	if button.BaseClass == nil {
		t.Fatal("Button records no base class, so nothing inherited is reachable")
	}

	var chain []string
	problems := reg.WalkBaseChain(&button, func(fullName string, _ *wasdkmeta.Class) {
		chain = append(chain, fullName)
	})
	if len(problems) > 0 {
		t.Errorf("Button's chain could not be followed: %v", problems)
	}
	// Nearest first, all the way to the root.
	want := []string{
		"Microsoft.UI.Xaml.Controls.Primitives.ButtonBase",
		"Microsoft.UI.Xaml.Controls.ContentControl",
		"Microsoft.UI.Xaml.Controls.Control",
		"Microsoft.UI.Xaml.FrameworkElement",
		"Microsoft.UI.Xaml.UIElement",
		"Microsoft.UI.Xaml.DependencyObject",
	}
	if len(chain) != len(want) {
		t.Fatalf("chain = %v, want %v", chain, want)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Errorf("chain[%d] = %q, want %q", i, chain[i], want[i])
		}
	}
}

// TestBaseChainTerminates guards against a walk that never ends. Every class in the
// tree is walked, so a cycle or an unbounded chain anywhere would hang the generator
// rather than fail it.
func TestBaseChainTerminates(t *testing.T) {
	reg := registry(t)
	var walked, chains int
	var problems []string
	for _, meta := range reg.Namespaces {
		for name := range meta.Classes {
			class := meta.Classes[name]
			if class.BaseClass == nil {
				continue
			}
			chains++
			found := reg.WalkBaseChain(&class, func(string, *wasdkmeta.Class) { walked++ })
			for _, problem := range found {
				problems = append(problems, meta.Namespace+"."+name+": "+problem)
			}
		}
	}
	if chains == 0 {
		t.Fatal("no class records a base class")
	}
	if len(problems) > 0 {
		limit := min(len(problems), 10)
		t.Errorf("%d chains could not be followed to the root: %v", len(problems), problems[:limit])
	}
	t.Logf("%d chains, %d base classes visited", chains, walked)
}

// TestClassWithoutABaseWalksNothing covers the root case: a class extending
// System.Object has no chain, and the walk must not invent one.
func TestClassWithoutABaseWalksNothing(t *testing.T) {
	reg := registry(t)
	var found *wasdkmeta.Class
	for _, meta := range reg.Namespaces {
		for name := range meta.Classes {
			class := meta.Classes[name]
			if class.BaseClass == nil && !class.Composable {
				found = &class
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		t.Skip("every class in this metadata is composable")
	}
	var visits int
	if problems := reg.WalkBaseChain(found, func(string, *wasdkmeta.Class) { visits++ }); len(problems) > 0 {
		t.Errorf("a class with no base reported problems: %v", problems)
	}
	if visits != 0 {
		t.Errorf("visited %d base classes for a class that extends System.Object", visits)
	}
}

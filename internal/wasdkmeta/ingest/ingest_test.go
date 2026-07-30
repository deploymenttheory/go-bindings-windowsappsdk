package ingest

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
	winmd "github.com/deploymenttheory/go-winmd"
	"github.com/deploymenttheory/go-winmd/nuget"
)

const winmdDir = "../../../metadata/winmd"

type ingested struct {
	ingester   *Ingester
	namespaces []*wasdkmeta.NamespaceMeta
	byName     map[string]*wasdkmeta.NamespaceMeta
}

var cached *ingested

// run projects the committed winmds once. These tests deliberately work against
// the real metadata rather than fixtures: the questions being asked — do sibling
// winmds resolve, do Windows.* references resolve — are questions about that
// metadata, and a fixture answering them would be asserting its own contents.
func run(t *testing.T) *ingested {
	t.Helper()
	if cached != nil {
		return cached
	}
	records, err := nuget.ReadProvenance(filepath.Join(winmdDir, "PROVENANCE.json"))
	if err != nil {
		t.Fatalf("reading PROVENANCE.json: %v", err)
	}
	sources := make([]Source, 0, len(records))
	for _, record := range records {
		name := filepath.Base(filepath.FromSlash(record.File))
		file, err := winmd.Open(filepath.Join(winmdDir, name))
		if err != nil {
			t.Fatalf("opening %s: %v", name, err)
		}
		sources = append(sources, Source{Name: name, Version: record.Version, File: file})
	}
	externalSet, err := external.Load("")
	if err != nil {
		t.Fatalf("loading the Windows.* universe: %v", err)
	}
	ingester, err := New(sources, externalSet)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	namespaces, err := ingester.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	byName := make(map[string]*wasdkmeta.NamespaceMeta, len(namespaces))
	for _, meta := range namespaces {
		byName[meta.Namespace] = meta
	}
	cached = &ingested{ingester: ingester, namespaces: namespaces, byName: byName}
	return cached
}

// TestSurfaceIsComplete cross-checks the projection against an independent count
// of the same winmds. Ingest silently dropping a construct kind is the failure
// with the least visible symptom: the output still looks reasonable.
func TestSurfaceIsComplete(t *testing.T) {
	result := run(t)
	if len(result.namespaces) != 77 {
		t.Errorf("%d namespaces, want 77", len(result.namespaces))
	}
	var interfaces, classes, enums, structs, delegates int
	for _, meta := range result.namespaces {
		interfaces += len(meta.Interfaces)
		classes += len(meta.Classes)
		enums += len(meta.Enums)
		structs += len(meta.Structs)
		delegates += len(meta.Delegates)
	}
	// These match `go run ./cmd/inspect --dir metadata/winmd`, which counts the
	// same TypeDefs by a different route.
	for _, check := range []struct {
		what      string
		got, want int
	}{
		{"interfaces", interfaces, 2568},
		{"classes", classes, 1286},
		{"enums", enums, 392},
		{"structs", structs, 69},
		{"delegates", delegates, 59},
	} {
		if check.got != check.want {
			t.Errorf("%d %s, want %d", check.got, check.what, check.want)
		}
	}
}

// TestNoUnresolvedReferences is the headline result of the two-pass design.
//
// Every reference in the Windows App SDK metadata must land somewhere: a sibling
// winmd in this repository, or go-bindings-winrt, or the one namespace with no
// Go equivalent. An unresolved-typeref means the pinned winmd set is missing a
// component, and generating from it would produce bindings for a surface that
// does not exist.
func TestNoUnresolvedReferences(t *testing.T) {
	result := run(t)
	var unresolved, missing []string
	for _, diagnostic := range result.ingester.Diagnostics {
		switch {
		case strings.HasPrefix(diagnostic, "unresolved-typeref:"):
			unresolved = append(unresolved, diagnostic)
		case strings.HasPrefix(diagnostic, "external-type-missing:"):
			missing = append(missing, diagnostic)
		}
	}
	if len(unresolved) > 0 {
		t.Errorf("%d unresolved references; the winmd pin is incomplete:\n  %s",
			len(unresolved), strings.Join(unresolved[:min(len(unresolved), 10)], "\n  "))
	}
	if len(missing) > 0 {
		t.Errorf("%d references into go-bindings-winrt did not resolve; bump the pin:\n  %s",
			len(missing), strings.Join(missing[:min(len(missing), 10)], "\n  "))
	}
}

// TestSiblingReferencesAreLocal is the two-pass proof at the reference level.
// Microsoft.UI.Xaml.winmd references types defined in Microsoft.UI.winmd; read
// one file at a time they are indistinguishable from foreign references, and
// projecting them as external would send them to go-bindings-winrt, which has
// no Microsoft.* packages at all.
func TestSiblingReferencesAreLocal(t *testing.T) {
	result := run(t)
	xaml := result.byName["Microsoft.UI.Xaml"]
	if xaml == nil {
		t.Fatal("Microsoft.UI.Xaml was not ingested")
	}

	// The namespaces Microsoft.UI.Xaml.winmd references but does not define.
	siblings := map[string]bool{
		"Microsoft.UI":             true,
		"Microsoft.UI.Composition": true,
		"Microsoft.UI.Dispatching": true,
		"Microsoft.UI.Input":       true,
		"Microsoft.UI.Text":        true,
		"Microsoft.UI.Windowing":   true,
		"Microsoft.UI.Xaml.Media":  true,
		"Microsoft.UI.Xaml.Input":  true,
	}
	seen := map[string]bool{}
	for _, meta := range result.namespaces {
		wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
			if !siblings[ref.Namespace] {
				return
			}
			seen[ref.Namespace] = true
			if ref.External {
				t.Errorf("%s.%s is marked external, but this repository defines it", ref.Namespace, ref.Name)
			}
			if ref.TargetKind == "" {
				t.Errorf("%s.%s has no target kind, so ingest failed to classify a sibling", ref.Namespace, ref.Name)
			}
		})
	}
	for namespace := range siblings {
		if !seen[namespace] {
			t.Errorf("no reference to %s was found; the test's premise has gone stale", namespace)
		}
	}
}

// TestWindowsReferencesAreExternal covers the other side: a Windows.* reference
// must carry the flag, because that is what tells the emit stage to import
// go-bindings-winrt rather than look for a local definition.
func TestWindowsReferencesAreExternal(t *testing.T) {
	result := run(t)
	var checked int
	for _, meta := range result.namespaces {
		wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
			if !strings.HasPrefix(ref.Namespace, "Windows.") {
				return
			}
			checked++
			if !ref.External {
				t.Errorf("[%s] %s.%s is not marked external", meta.Namespace, ref.Namespace, ref.Name)
			}
			if ref.TargetKind == "" {
				t.Errorf("[%s] %s.%s has no target kind", meta.Namespace, ref.Namespace, ref.Name)
			}
		})
	}
	if checked == 0 {
		t.Fatal("no Windows.* references found at all")
	}
	t.Logf("%d Windows.* references, all external and classified", checked)
}

// TestForeignReferencesAreUnresolvedOnPurpose pins the WebView2 case. Its
// namespace ships in a separate NuGet package with no Go bindings, so the
// reference must arrive at emit unresolved AND unmarked — an emitter that saw
// External would import a package that does not exist, and one that saw a
// TargetKind would name a type that was never generated.
func TestForeignReferencesAreUnresolvedOnPurpose(t *testing.T) {
	result := run(t)
	var found int
	for _, meta := range result.namespaces {
		wasdkmeta.WalkRefs(meta, func(ref *wasdkmeta.TypeRef) {
			if _, foreign := KnownForeignNamespaces[ref.Namespace]; !foreign {
				return
			}
			found++
			if ref.External {
				t.Errorf("%s.%s is marked external, but nothing projects it", ref.Namespace, ref.Name)
			}
			if ref.TargetKind != "" {
				t.Errorf("%s.%s has target kind %q, but no Go type exists for it",
					ref.Namespace, ref.Name, ref.TargetKind)
			}
		})
	}
	if found == 0 {
		t.Error("no reference to a known-foreign namespace was found; is the list still needed?")
	}
	// Every one is accounted for by a diagnostic, so the emit stage's skip count
	// can be reconciled against this number.
	var diagnosed int
	for _, diagnostic := range result.ingester.Diagnostics {
		if strings.HasPrefix(diagnostic, "foreign-namespace-ref:") {
			diagnosed++
		}
	}
	if diagnosed == 0 {
		t.Error("foreign references were not diagnosed")
	}
	t.Logf("%d foreign references, %d diagnostics", found, diagnosed)
}

// TestButtonRecordsItsCompositionShape checks the class projection on the type
// the whole repository is aimed at, and records the fact the base-class
// projection exists for: Button's own interface list is just IButton, so
// nothing here reaches Click or Content.
func TestButtonRecordsItsCompositionShape(t *testing.T) {
	result := run(t)
	controls := result.byName["Microsoft.UI.Xaml.Controls"]
	if controls == nil {
		t.Fatal("Microsoft.UI.Xaml.Controls was not ingested")
	}
	button, ok := controls.Classes["Button"]
	if !ok {
		t.Fatal("Button was not projected")
	}
	if !button.Composable {
		t.Error("Button is not marked composable, but it extends ButtonBase")
	}
	if button.DefaultInterface == nil || button.DefaultInterface.Name != "IButton" {
		t.Errorf("default interface = %+v, want IButton", button.DefaultInterface)
	}
	if len(button.ComposableFactories) == 0 {
		t.Error("Button records no composable factory, so it could not be constructed")
	}
	if len(button.Interfaces) != 1 {
		t.Errorf("Button lists %d interfaces, want 1 — inherited interfaces are NOT in InterfaceImpl, "+
			"which is why the base-class chain has to be walked separately", len(button.Interfaces))
	}
	// The chain is the only route to those inherited interfaces, so the base has to
	// be recorded: Button extends Primitives.ButtonBase, where Click lives.
	if button.BaseClass == nil {
		t.Fatal("Button records no base class")
	}
	if got := button.BaseClass.Namespace + "." + button.BaseClass.Name; got != "Microsoft.UI.Xaml.Controls.Primitives.ButtonBase" {
		t.Errorf("Button extends %s, want Microsoft.UI.Xaml.Controls.Primitives.ButtonBase", got)
	}
	if button.BaseClass.TargetKind != "Class" {
		t.Errorf("the base reference is classified as %q, want Class", button.BaseClass.TargetKind)
	}
}

// TestRootClassHasNoBase is the other end of the chain: a class extending
// System.Object must record no base, or the walk would chase a marker type that is
// not a runtime class at all.
func TestRootClassHasNoBase(t *testing.T) {
	result := run(t)
	xaml := result.byName["Microsoft.UI.Xaml"]
	root, ok := xaml.Classes["DependencyObject"]
	if !ok {
		t.Fatal("DependencyObject was not projected")
	}
	if root.Composable {
		t.Error("DependencyObject is marked composable, but it extends System.Object")
	}
	if root.BaseClass != nil {
		t.Errorf("DependencyObject records a base class (%s.%s)", root.BaseClass.Namespace, root.BaseClass.Name)
	}
}

// TestEveryInterfaceIsQueryable guards the generated QueryInterface calls: an
// interface without an IID would be queried for with a zero GUID.
func TestEveryInterfaceIsQueryable(t *testing.T) {
	result := run(t)
	var missing []string
	for _, meta := range result.namespaces {
		for name, definition := range meta.Interfaces {
			if definition.GUID == "" {
				missing = append(missing, meta.Namespace+"."+name)
			}
		}
		for name, delegate := range meta.Delegates {
			if delegate.GUID == "" {
				missing = append(missing, meta.Namespace+"."+name)
			}
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d interfaces/delegates have no IID: %v", len(missing), missing[:min(len(missing), 10)])
	}
}

// TestMethodsKeepTheirSlots asserts that no member is reordered or dropped.
// Vtable slots are positional, so a method omitted at ingest would shift every
// method after it onto the wrong function.
func TestMethodsKeepTheirSlots(t *testing.T) {
	result := run(t)
	window := result.byName["Microsoft.UI.Xaml"].Interfaces["IWindow"]
	names := make([]string, len(window.Methods))
	for i, method := range window.Methods {
		names[i] = method.Name
	}
	// MethodDef order, as cmd/inspect reports it: slot = 6 + index.
	want := []string{
		"get_Bounds", "get_Visible", "get_Content", "put_Content", "get_CoreWindow",
		"get_Compositor", "get_Dispatcher", "get_DispatcherQueue", "get_Title", "put_Title",
	}
	if len(names) < len(want) {
		t.Fatalf("IWindow has %d methods, want at least %d", len(names), len(want))
	}
	for i, name := range want {
		if names[i] != name {
			t.Errorf("method %d (slot %d) is %s, want %s", i, 6+i, names[i], name)
		}
	}
}

// TestDelegateInvokeIsProjected matters because every XAML event needs a
// grounded handler, and grounding one needs the Invoke signature.
func TestDelegateInvokeIsProjected(t *testing.T) {
	result := run(t)
	handler, ok := result.byName["Microsoft.UI.Xaml"].Delegates["RoutedEventHandler"]
	if !ok {
		t.Fatal("RoutedEventHandler was not projected")
	}
	if handler.Invoke.Name != "Invoke" {
		t.Errorf("Invoke.Name = %q, want Invoke", handler.Invoke.Name)
	}
	if len(handler.Invoke.Params) != 2 {
		t.Fatalf("Invoke takes %d params, want 2 (sender, e)", len(handler.Invoke.Params))
	}
	if handler.Invoke.Return != nil {
		t.Error("Invoke has a logical return value; WinRT delegate Invokes return void")
	}
}

// TestPropertiesPairTheirAccessors checks the MethodSemantics pairing. Without
// it a property would emit as two bare methods named get_X and put_X.
func TestPropertiesPairTheirAccessors(t *testing.T) {
	result := run(t)
	window := result.byName["Microsoft.UI.Xaml"].Interfaces["IWindow"]
	var title *wasdkmeta.Property
	for i := range window.Properties {
		if window.Properties[i].Name == "Title" {
			title = &window.Properties[i]
		}
	}
	if title == nil {
		t.Fatal("IWindow has no Title property")
	}
	if title.Getter != "get_Title" || title.Setter != "put_Title" {
		t.Errorf("Title accessors = %q/%q, want get_Title/put_Title", title.Getter, title.Setter)
	}
	if title.Type.Kind != "Native" || title.Type.Name != "HString" {
		t.Errorf("Title type = %+v, want the HString native", title.Type)
	}
}

// TestProvenanceIsRecordedPerNamespace matters because components version
// independently of the meta-package: in 2.3.1, WinUI is 2.3.0 while Foundation
// is 2.3.5. One version for the whole tree would be wrong for most of it.
func TestProvenanceIsRecordedPerNamespace(t *testing.T) {
	result := run(t)
	for _, check := range []struct{ namespace, winmd string }{
		{"Microsoft.UI.Xaml", "Microsoft.UI.Xaml.winmd"},
		{"Microsoft.UI.Dispatching", "Microsoft.UI.winmd"},
		{"Microsoft.UI.Text", "Microsoft.UI.Text.winmd"},
		{"Microsoft.Windows.AppLifecycle", "Microsoft.Windows.AppLifecycle.winmd"},
	} {
		meta := result.byName[check.namespace]
		if meta == nil {
			t.Errorf("%s was not ingested", check.namespace)
			continue
		}
		if meta.WinmdFile != check.winmd {
			t.Errorf("%s came from %q, want %q", check.namespace, meta.WinmdFile, check.winmd)
		}
		if meta.WinmdVersion == "" {
			t.Errorf("%s records no component version", check.namespace)
		}
		if meta.SchemaVersion != wasdkmeta.CurrentSchemaVersion {
			t.Errorf("%s has schema version %d, want %d", check.namespace, meta.SchemaVersion, wasdkmeta.CurrentSchemaVersion)
		}
	}
}

// TestNamespacesAreSorted guards the determinism the committed output depends
// on: Ingest builds its result from a map.
func TestNamespacesAreSorted(t *testing.T) {
	result := run(t)
	for i := 1; i < len(result.namespaces); i++ {
		if result.namespaces[i-1].Namespace >= result.namespaces[i].Namespace {
			t.Fatalf("not sorted at %d: %q then %q", i,
				result.namespaces[i-1].Namespace, result.namespaces[i].Namespace)
		}
	}
}

// TestLocalNamespacesMatchTheProjection keeps the classification pass and the
// projection pass in step. They walk the TypeDefs separately, so a filter
// applied in one and not the other would misclassify sibling references.
func TestLocalNamespacesMatchTheProjection(t *testing.T) {
	result := run(t)
	local := result.ingester.LocalNamespaces()
	if len(local) != len(result.namespaces) {
		t.Errorf("classification found %d namespaces, projection produced %d", len(local), len(result.namespaces))
	}
	for _, namespace := range local {
		if result.byName[namespace] == nil {
			t.Errorf("%s was classified as local but never projected", namespace)
		}
	}
}

// TestDuplicateTypeIsRejected covers the mispinned-file-set case: two
// components shipping overlapping metadata. Keeping one silently would generate
// bindings for a surface that does not exist.
func TestDuplicateTypeIsRejected(t *testing.T) {
	path := filepath.Join(winmdDir, "Microsoft.UI.Text.winmd")
	first, err := winmd.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	second, err := winmd.Open(path)
	if err != nil {
		t.Fatalf("re-opening %s: %v", path, err)
	}
	_, err = New([]Source{
		{Name: "a.winmd", File: first},
		{Name: "b.winmd", File: second},
	}, nil)
	if err == nil {
		t.Fatal("New accepted the same winmd twice")
	}
	if !strings.Contains(err.Error(), "defined in both") {
		t.Errorf("error = %q, want it to name both files", err)
	}
}

// TestLocalShadowingExternalIsRejected guards the boundary between the two
// modules. A winmd defining a Windows.* type would give this module a second,
// incompatible definition of a type every signature already shares, and the
// resulting mismatch would only appear as a confusing type error much later.
func TestLocalShadowingExternalIsRejected(t *testing.T) {
	externalSet, err := external.Load("")
	if err != nil {
		t.Fatalf("loading the Windows.* universe: %v", err)
	}
	// go-bindings-winrt's own contract winmds define Windows.* types, so one of
	// them is exactly the input this check exists to refuse.
	dir, _, err := external.Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	contract := filepath.Join(filepath.Dir(dir), "winmd", "Windows.Foundation.FoundationContract.winmd")
	file, err := winmd.Open(contract)
	if err != nil {
		t.Skipf("the pinned module's contract winmds are not readable here: %v", err)
	}
	_, err = New([]Source{{Name: "FoundationContract.winmd", File: file}}, externalSet)
	if err == nil {
		t.Fatal("New accepted a winmd defining Windows.* types")
	}
	if !strings.Contains(err.Error(), "must never shadow") {
		t.Errorf("error = %q, want it to say the local winmd shadows the Windows.* surface", err)
	}
}

// TestDiagnosticSummaryIsSortedAndCounted keeps the operator-facing output
// stable; it is the first thing read after an SDK bump.
func TestDiagnosticSummaryIsSortedAndCounted(t *testing.T) {
	lines := DiagnosticSummary([]string{
		"zebra: one",
		"alpha: two",
		"alpha: three",
	})
	if len(lines) != 2 {
		t.Fatalf("%d summary lines, want 2: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "alpha") || !strings.HasSuffix(lines[0], "2") {
		t.Errorf("first line = %q, want alpha with count 2", lines[0])
	}
	if !strings.Contains(lines[1], "zebra") {
		t.Errorf("second line = %q, want zebra", lines[1])
	}
}

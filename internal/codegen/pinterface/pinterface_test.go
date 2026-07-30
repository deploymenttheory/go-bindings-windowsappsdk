package pinterface

import (
	"strings"
	"testing"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/pipeline"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta/external"
)

const metadataDir = "../../../metadata/wasdk"

var cachedRegistry *pipeline.Registry

func registry(t *testing.T) *pipeline.Registry {
	t.Helper()
	if cachedRegistry != nil {
		return cachedRegistry
	}
	externalSet, err := external.Load("")
	if err != nil {
		t.Fatalf("loading the Windows.* universe: %v", err)
	}
	loaded, err := pipeline.Load(metadataDir, externalSet)
	if err != nil {
		t.Fatalf("loading the committed metadata: %v", err)
	}
	cachedRegistry = loaded
	return cachedRegistry
}

func native(name string) wasdkmeta.TypeRef {
	return wasdkmeta.TypeRef{Kind: "Native", Name: name}
}

// TestKnownIIDsFromOtherProjections is the test that matters, and the only one
// that can catch a wrong signature grammar.
//
// The expectations are not derived here. They are the values go-bindings-winrt
// committed for the same instantiations — IID_IVectorOfString and friends in its
// generated *_pinterfaces.go — reached by different code from the same
// specification, and exercised by its acceptance tests against live WinRT. Every
// projection (C++/WinRT, CsWinRT, windows-rs) derives the same values, which is
// exactly what makes cross-language QueryInterface on a parameterized interface
// work.
//
// A signature string wrong by one character yields a perfectly plausible GUID
// that nothing in the system agrees with, and the failure is an E_NOINTERFACE a
// long way from the cause — so the expectations have to come from outside this
// package.
func TestKnownIIDsFromOtherProjections(t *testing.T) {
	reg := registry(t)
	for _, check := range []struct {
		what string
		ref  wasdkmeta.TypeRef
		want string
	}{
		{
			what: "IVector<String>",
			ref: wasdkmeta.TypeRef{
				Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IVector`1",
				Args: []wasdkmeta.TypeRef{native("HString")},
			},
			want: "98b9acc1-4b56-532e-ac73-03d5291cca90",
		},
		{
			what: "IVectorView<String>",
			ref: wasdkmeta.TypeRef{
				Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IVectorView`1",
				Args: []wasdkmeta.TypeRef{native("HString")},
			},
			want: "2f13c006-a03a-5f69-b090-75a43e33423e",
		},
		{
			what: "IIterable<String>",
			ref: wasdkmeta.TypeRef{
				Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IIterable`1",
				Args: []wasdkmeta.TypeRef{native("HString")},
			},
			want: "e2fcc7c1-3bfc-5a0b-b2b0-72e769d1cb7e",
		},
		{
			what: "IAsyncOperation<Boolean>",
			ref: wasdkmeta.TypeRef{
				Kind: "GenericInst", Namespace: "Windows.Foundation", Name: "IAsyncOperation`1",
				Args: []wasdkmeta.TypeRef{native("Bool")},
			},
			want: "cdb5efb3-5788-509d-9be1-71ccb8a3362a",
		},
		{
			what: "IReference<Int32>",
			ref: wasdkmeta.TypeRef{
				Kind: "GenericInst", Namespace: "Windows.Foundation", Name: "IReference`1",
				Args: []wasdkmeta.TypeRef{native("I4")},
			},
			want: "548cefbd-bc8a-5fa0-8df2-957440fc8bf4",
		},
	} {
		got, err := InstanceIID(&check.ref, reg)
		if err != nil {
			t.Errorf("%s: %v", check.what, err)
			continue
		}
		if got != check.want {
			signature, _ := Signature(&check.ref, reg)
			t.Errorf("%s IID = %s, want %s (signature was %q)", check.what, got, check.want, signature)
		}
	}
}

// TestSignatureGrammar pins each production of the WinMD signature grammar
// separately, so a failure in the IID test above points at which one is wrong.
func TestSignatureGrammar(t *testing.T) {
	reg := registry(t)
	for _, check := range []struct {
		what string
		ref  wasdkmeta.TypeRef
		want string
	}{
		{"string", native("HString"), "string"},
		{"int32", native("I4"), "i4"},
		{"bool", native("Bool"), "b1"},
		{"guid", native("Guid"), "g16"},
		{"object", native("Object"), "cinterface(IInspectable)"},
		{
			"a local enum",
			wasdkmeta.TypeRef{Kind: "ApiRef", Namespace: "Microsoft.UI.Xaml", Name: "Visibility", TargetKind: "Enum"},
			"enum(Microsoft.UI.Xaml.Visibility;i4)",
		},
		{
			"an external struct, fields expanded",
			wasdkmeta.TypeRef{Kind: "ApiRef", Namespace: "Windows.Foundation", Name: "Point", TargetKind: "Struct", External: true},
			"struct(Windows.Foundation.Point;f4;f4)",
		},
	} {
		got, err := Signature(&check.ref, reg)
		if err != nil {
			t.Errorf("%s: %v", check.what, err)
			continue
		}
		if got != check.want {
			t.Errorf("%s signature = %q, want %q", check.what, got, check.want)
		}
	}
}

// TestRuntimeClassSignature covers the rc(...) production, which is what a XAML
// event's args class goes through. It spans both modules: the open delegate is
// go-bindings-winrt's and the argument class is this repository's.
func TestRuntimeClassSignature(t *testing.T) {
	reg := registry(t)
	ref := wasdkmeta.TypeRef{
		Kind: "ApiRef", Namespace: "Microsoft.UI.Xaml", Name: "WindowEventArgs", TargetKind: "Class",
	}
	got, err := Signature(&ref, reg)
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if !strings.HasPrefix(got, "rc(Microsoft.UI.Xaml.WindowEventArgs;{") || !strings.HasSuffix(got, "})") {
		t.Errorf("signature = %q, want rc(<name>;{<default interface IID>})", got)
	}
}

// TestCrossModuleInstantiationGrounds is the shape every XAML event actually
// carries: go-bindings-winrt's TypedEventHandler closed over one of this
// repository's args classes. Grounding it needs both halves of the registry, and
// this is the case that would fail if either were missing.
func TestCrossModuleInstantiationGrounds(t *testing.T) {
	reg := registry(t)
	ref := wasdkmeta.TypeRef{
		Kind: "GenericInst", Namespace: "Windows.Foundation", Name: "TypedEventHandler`2",
		TargetKind: "Delegate", External: true,
		Args: []wasdkmeta.TypeRef{
			native("Object"),
			{Kind: "ApiRef", Namespace: "Microsoft.UI.Xaml", Name: "WindowEventArgs", TargetKind: "Class"},
		},
	}
	signature, err := Signature(&ref, reg)
	if err != nil {
		t.Fatalf("Signature: %v", err)
	}
	if !strings.HasPrefix(signature, "pinterface({") {
		t.Errorf("signature = %q, want a pinterface(...) production", signature)
	}
	if !strings.Contains(signature, "cinterface(IInspectable)") {
		t.Error("the Object sender did not render as cinterface(IInspectable)")
	}
	if !strings.Contains(signature, "rc(Microsoft.UI.Xaml.WindowEventArgs;") {
		t.Error("the local args class did not render as a runtime class")
	}
	iid, err := InstanceIID(&ref, reg)
	if err != nil {
		t.Fatalf("InstanceIID: %v", err)
	}
	if len(iid) != 36 {
		t.Errorf("IID = %q, want a canonical 36-character GUID", iid)
	}
}

// TestUngroundableRefsFail covers what must NOT silently produce an IID. A
// plausible GUID derived from an incomplete signature is worse than an error,
// because it fails at QueryInterface with nothing to point at.
func TestUngroundableRefsFail(t *testing.T) {
	reg := registry(t)
	elem := native("I4")
	for what, ref := range map[string]wasdkmeta.TypeRef{
		"an unbound generic parameter": {Kind: "GenericParamRef", Index: 0},
		"an array":                     {Kind: "Array", Elem: &elem},
		"an unresolved reference":      {Kind: "ApiRef", Namespace: "Microsoft.Nope", Name: "IGone", TargetKind: "Interface"},
		"a reference with no kind":     {Kind: "ApiRef", Namespace: "Microsoft.UI.Xaml", Name: "Visibility"},
		"a WebView2 type":              {Kind: "ApiRef", Namespace: "Microsoft.Web.WebView2.Core", Name: "CoreWebView2", TargetKind: "Class"},
	} {
		if _, err := Signature(&ref, reg); err == nil {
			t.Errorf("%s produced a signature", what)
		}
	}
	// And an instantiation containing one must fail too, rather than grounding
	// the parts it can.
	partial := wasdkmeta.TypeRef{
		Kind: "GenericInst", Namespace: "Windows.Foundation.Collections", Name: "IVector`1",
		Args: []wasdkmeta.TypeRef{{Kind: "GenericParamRef", Index: 0}},
	}
	if _, err := InstanceIID(&partial, reg); err == nil {
		t.Error("an instantiation over an unbound parameter produced an IID")
	}
}

func TestInstanceIIDRejectsNonInstantiations(t *testing.T) {
	ref := wasdkmeta.TypeRef{Kind: "ApiRef", Namespace: "Microsoft.UI.Xaml", Name: "IWindow", TargetKind: "Interface"}
	if _, err := InstanceIID(&ref, registry(t)); err == nil {
		t.Error("InstanceIID accepted a plain interface reference")
	}
}

// TestUUIDv5FieldsAreSet checks the bits RFC 4122 fixes, since getting them wrong
// produces a GUID that differs from every other projection's in exactly two
// nibbles — the hardest kind of mismatch to spot by eye.
func TestUUIDv5FieldsAreSet(t *testing.T) {
	got := uuidV5(wrtPinterfaceNamespace, "anything")
	if len(got) != 36 {
		t.Fatalf("uuidV5 = %q", got)
	}
	if got[14] != '5' {
		t.Errorf("version nibble = %q, want 5 (uuidV5 = %s)", got[14], got)
	}
	switch got[19] {
	case '8', '9', 'a', 'b':
	default:
		t.Errorf("variant nibble = %q, want one of 8/9/a/b (uuidV5 = %s)", got[19], got)
	}
	// Deterministic: the same name always yields the same IID, or regeneration
	// would not reproduce.
	if again := uuidV5(wrtPinterfaceNamespace, "anything"); again != got {
		t.Errorf("uuidV5 is not deterministic: %s then %s", got, again)
	}
}

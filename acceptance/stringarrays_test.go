//go:build windows && amd64

package acceptance

import (
	"strings"
	"testing"
	"unsafe"

	wasdkglobalization "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/windows/globalization"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
	wrtglobalization "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/globalization"
)

// TestFillStringArrayAgainstTheLiveRuntime drives this module's generated GetMany with
// a real IVectorView<String> and real HSTRINGs written by the Windows runtime.
//
// This is a live test because the failure modes are invisible to the compiler and to
// any assertion on generated text. A []string cannot be a view of a buffer of HSTRING
// handles, so the body allocates a parallel handle buffer, hands it over, and then
// takes each handle the callee wrote — reading it and deleting it. Get the ownership
// wrong and it leaks or double-frees; get the buffer wrong and it reads whatever
// follows it in memory. All three compile.
//
// The vector comes from go-bindings-winrt's Calendar, and is queried into THIS module's
// monomorphized type. That works — and is worth doing rather than calling the sibling's
// own binding — because both modules derive the same pinterface IID from the same
// signature grammar and lay the vtable out from the same open interface. Distinct Go
// types, one ABI. If that ever stopped holding, this test would fail at the
// QueryInterface rather than silently calling the wrong slot.
//
// Windows.Globalization.Calendar is registered with the operating system, so this needs
// an apartment but not the Windows App SDK bootstrap.
func TestFillStringArrayAgainstTheLiveRuntime(t *testing.T) {
	if err := winrt.Initialize(); err != nil {
		t.Skipf("no WinRT apartment: %v", err)
	}

	calendar, err := wrtglobalization.NewCalendar()
	if err != nil {
		t.Skipf("Windows.Globalization.Calendar is unavailable: %v", err)
	}
	defer calendar.Release()

	languages, err := calendar.Languages()
	if err != nil {
		t.Fatalf("Calendar.Languages: %v", err)
	}
	defer languages.Release()

	// Into this module's own IVectorOfString-family type, through the IID both modules
	// derive independently.
	view, err := winrt.QueryInterface[wasdkglobalization.IVectorViewOfString](
		unsafe.Pointer(languages), &wasdkglobalization.IID_IVectorViewOfString)
	if err != nil {
		t.Fatalf("querying the live vector for this module's IVectorViewOfString: %v", err)
	}
	defer view.Release()

	size, err := view.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size == 0 {
		t.Skip("the calendar reports no languages, so there is nothing to read")
	}

	// GetAt gives the expected values through the already-working single-string path,
	// so the array path is compared against something independent rather than against
	// itself.
	expected := make([]string, size)
	for i := uint32(0); i < size; i++ {
		if expected[i], err = view.GetAt(i); err != nil {
			t.Fatalf("GetAt(%d): %v", i, err)
		}
	}

	// The fill path: this side allocates, the callee writes handles in, and the
	// generated body takes each one.
	items := make([]string, size)
	written, err := view.GetMany(0, items)
	if err != nil {
		t.Fatalf("GetMany through the converting fill path: %v", err)
	}
	if written != size {
		t.Errorf("GetMany wrote %d elements, want %d", written, size)
	}
	for i := range items {
		if items[i] != expected[i] {
			t.Errorf("items[%d] = %q, want %q", i, items[i], expected[i])
		}
		if items[i] == "" {
			t.Errorf("items[%d] is empty; the handle was not read before it was deleted", i)
		}
	}
	t.Logf("read %d language tags through the converting fill path: %s",
		written, strings.Join(items, ", "))

	// A buffer LARGER than the vector: the callee writes fewer handles than the buffer
	// holds, and the unwritten slots stay null. Taking a null handle yields "" and
	// deletes nothing, which is what makes the conversion safe to run across the whole
	// slice without knowing the count in advance.
	oversized := make([]string, size+4)
	written, err = view.GetMany(0, oversized)
	if err != nil {
		t.Fatalf("GetMany into an oversized buffer: %v", err)
	}
	if written != size {
		t.Errorf("GetMany into an oversized buffer wrote %d, want %d", written, size)
	}
	for i := size; i < uint32(len(oversized)); i++ {
		if oversized[i] != "" {
			t.Errorf("oversized[%d] = %q, want empty: that slot was never written", i, oversized[i])
		}
	}

	// Repeated calls. A double free shows up here rather than on the first pass, and an
	// empty result on a later iteration would mean the handles were consumed once and
	// read again.
	for round := range 50 {
		repeat := make([]string, size)
		if _, err := view.GetMany(0, repeat); err != nil {
			t.Fatalf("GetMany on round %d: %v", round, err)
		}
		for i := range repeat {
			if repeat[i] != expected[i] {
				t.Fatalf("round %d: items[%d] = %q, want %q — the handles are not being "+
					"read and released cleanly on every call", round, i, repeat[i], expected[i])
			}
		}
	}
}

// TestFillStringArrayAcrossManyElements is the multi-element case, with contents this
// test chooses.
//
// The live Calendar reports one language on most machines, which exercises the
// conversion loop exactly once — enough to catch a wrong ownership rule, not enough to
// catch an indexing one. A Go-implemented IVectorView<String> from go-bindings-winrt's
// runtime gives a known list of any length, and it is still a real WinRT object on the
// other side of the ABI: the handles arrive through WindowsCreateString and are read
// back through the same vtable slots.
//
// Values chosen to be distinguishable if the loop ever reads the wrong slot, and to
// include the two shapes an HSTRING conversion can get wrong on its own: the empty
// string, which IS the null handle canonically, and a non-ASCII string, whose UTF-16
// length differs from its Go byte length.
func TestFillStringArrayAcrossManyElements(t *testing.T) {
	if err := winrt.Initialize(); err != nil {
		t.Skipf("no WinRT apartment: %v", err)
	}

	want := []string{"alpha", "beta", "", "delta-éè", "epsilon", "zeta"}
	backing := winrt.NewStringVectorView(want)
	defer backing.Release()

	// The object's address IS its COM pointer — the embedded VectorView is the first
	// field for exactly that reason — so this takes it directly rather than going
	// through Ptr() and back, which is a uintptr round trip vet rightly objects to.
	view, err := winrt.QueryInterface[wasdkglobalization.IVectorViewOfString](
		unsafe.Pointer(backing), &wasdkglobalization.IID_IVectorViewOfString)
	if err != nil {
		t.Fatalf("querying the Go-implemented vector for IVectorViewOfString: %v", err)
	}
	defer view.Release()

	items := make([]string, len(want))
	written, err := view.GetMany(0, items)
	if err != nil {
		t.Fatalf("GetMany: %v", err)
	}
	if int(written) != len(want) {
		t.Fatalf("GetMany wrote %d elements, want %d", written, len(want))
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("items[%d] = %q, want %q", i, items[i], want[i])
		}
	}

	// A non-zero start index, which is where an off-by-one in the buffer indexing
	// would show: the callee writes from startIndex into slot 0 of the buffer.
	const start = 2
	tail := make([]string, len(want)-start)
	written, err = view.GetMany(start, tail)
	if err != nil {
		t.Fatalf("GetMany from index %d: %v", start, err)
	}
	if int(written) != len(tail) {
		t.Fatalf("GetMany from index %d wrote %d, want %d", start, written, len(tail))
	}
	for i := range tail {
		if tail[i] != want[start+i] {
			t.Errorf("tail[%d] = %q, want %q", i, tail[i], want[start+i])
		}
	}
}

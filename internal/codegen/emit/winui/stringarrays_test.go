package emitwinui

import (
	"strings"
	"testing"
)

// A WinRT string array crosses the ABI as a count and a buffer of HSTRING handles, so
// a []string can never be a direct view of it. The emitter builds a parallel handle
// buffer and converts across it, and WHO OWNS THE HANDLES differs per shape. These
// three tests pin one shape each, because getting the ownership wrong leaks or
// double-frees rather than failing to compile.

// TestInputStringArrayOwnsTheHandlesItCreates covers [in]. This side creates a handle
// per element, so this side deletes them — after the call, not before.
func TestInputStringArrayOwnsTheHandlesItCreates(t *testing.T) {
	source := source(t, "windows/applifecycle/applifecycle_interfaces.go")
	body := methodBody(source, "func (self *IActivationRegistrationManagerStatics) RegisterForFileTypeActivation(")
	if body == "" {
		t.Fatal("RegisterForFileTypeActivation is not emitted")
	}
	if !strings.Contains(body, "supportedFileTypes []string") {
		t.Error("the array parameter is not exposed as []string")
	}
	if !strings.Contains(body, "_supportedFileTypesRaw := make([]syswinrt.HSTRING, len(supportedFileTypes))") {
		t.Error("no parallel handle buffer is built")
	}
	if !strings.Contains(body, "winrt.NewHString(") {
		t.Error("the strings are not converted into handles")
	}
	// Deferred, so the handles outlive the call and are released whichever way the
	// function returns — including the early return when a later element fails to
	// convert, which would otherwise leak every handle built before it.
	if !strings.Contains(body, "defer _supportedFileTypesHandle.Close()") {
		t.Error("the created handles are not released; every call would leak one per element")
	}
	// Released, not taken: these handles belong to this side, and TakeHString here
	// would be a double free once Close ran.
	if strings.Contains(body, "winrt.TakeHString(_supportedFileTypesRaw") {
		t.Error("an input array takes handles it created; that is a double free")
	}
	// The count and the buffer, in that order.
	if !strings.Contains(body, "_supportedFileTypesSize := uintptr(len(_supportedFileTypesRaw))") {
		t.Error("the count word is not synthesized from the handle buffer")
	}
}

// TestFillStringArrayTakesTheHandlesTheCalleeWrote covers [out] fill. The CALLER
// allocates the buffer and the CALLEE writes handles into it, which the caller then
// owns — so each is taken, reading it and deleting it in one step.
func TestFillStringArrayTakesTheHandlesTheCalleeWrote(t *testing.T) {
	source := source(t, "ui/xaml/xaml_pinterfaces.go")
	body := methodBody(source, "func (self *IVectorViewOfString) GetMany(")
	if body == "" {
		t.Fatal("IVectorViewOfString.GetMany is not emitted")
	}
	if !strings.Contains(body, "items []string") {
		t.Error("the fill parameter is not exposed as []string")
	}
	if !strings.Contains(body, "_itemsRaw := make([]syswinrt.HSTRING, len(items))") {
		t.Error("no parallel handle buffer is allocated for the callee to fill")
	}
	// Nothing is created on this side, so nothing is closed on this side.
	if strings.Contains(body, "winrt.NewHString(_items") || strings.Contains(body, "defer _itemsHandle.Close()") {
		t.Error("a fill array is creating handles; the callee writes them")
	}
	if !strings.Contains(body, "items[_itemsIndex] = winrt.TakeHString(_itemsRaw[_itemsIndex])") {
		t.Error("the callee's handles are not taken into the caller's slice")
	}
	// After the HRESULT check. A failed call wrote nothing, and the caller's slice
	// must be left as it was.
	check := strings.Index(body, "if err := win32.ErrIfFailed")
	convert := strings.Index(body, "winrt.TakeHString(_itemsRaw")
	if check < 0 || convert < 0 || check > convert {
		t.Error("the conversion is not inside the success path")
	}
}

// TestReturnedStringArrayTakesHandlesThenFreesTheBuffer covers the return shape, which
// owns the most: the callee allocated the buffer AND every string in it, so each handle
// is taken and then the buffer itself is freed. Two separate allocations, two separate
// releases, in that order — freeing the buffer first would delete the handles out from
// under the reads.
func TestReturnedStringArrayTakesHandlesThenFreesTheBuffer(t *testing.T) {
	source := source(t, "windows/widgets/providers/providers_interfaces.go")
	body := methodBody(source, "func (self *IWidgetManager) GetWidgetIds(")
	if body == "" {
		t.Fatal("IWidgetManager.GetWidgetIds is not emitted")
	}
	if !strings.Contains(body, "func (self *IWidgetManager) GetWidgetIds() ([]string, error)") {
		t.Error("the return is not ([]string, error)")
	}
	// The retval pointer is typed to the HANDLE, not to string: it points at the
	// callee's buffer, which holds handles.
	if !strings.Contains(body, "result := new(*syswinrt.HSTRING)") {
		t.Error("the retval pointer is not typed to the ABI element")
	}
	if !strings.Contains(body, "items[i] = winrt.TakeHString(raw)") {
		t.Error("the elements are not taken; the strings would leak")
	}
	if !strings.Contains(body, "systemcom.CoTaskMemFree(unsafe.Pointer(*result))") {
		t.Error("the callee's buffer is not freed")
	}
	take := strings.Index(body, "winrt.TakeHString(raw)")
	free := strings.Index(body, "systemcom.CoTaskMemFree")
	if take < 0 || free < 0 || take > free {
		t.Error("the buffer is freed before its handles are read")
	}
	// And the whole thing is behind the HRESULT check and the null-buffer guard: on
	// failure there is no buffer to read or free.
	if !strings.Contains(body, "if *result == nil || *resultSize == 0 {") {
		t.Error("no guard against a null or empty buffer")
	}
}

// TestDirectViewArraysStillCopy is the control. Adding a conversion path must not have
// turned every array into one: an element whose Go form IS its ABI form is still a
// direct view copied in one go, which is the whole reason that path exists.
func TestDirectViewArraysStillCopy(t *testing.T) {
	result := emit(t)
	var copied bool
	for _, content := range result.files {
		if strings.Contains(content, "copy(items, unsafe.Slice(*result, *resultSize))") {
			copied = true
			break
		}
	}
	if !copied {
		t.Error("no returned array uses the block copy; the direct-view path has been lost")
	}
}

//go:build windows && amd64

package app

// A Go application object that WinUI accepts as its own.
//
// WinUI resolves the XAML types named inside its theme dictionaries by asking the
// application for IXamlMetadataProvider. MUXControlsFactory::VerifyInitialized in
// microsoft/microsoft-ui-xaml says so in its error text, and the consequence of not
// answering is not a missing feature: TextBox, PasswordBox, RichEditBox, SearchBox and
// ProgressBar fail to load their default styles and take the process down at layout.
//
// A native Application cannot be made to answer that QueryInterface, so the application
// has to BE a Go object — the controlling outer of an aggregated Microsoft.UI.Xaml
// .Application, per the [Composable] pattern. go-bindings-winrt v0.5.0 provides the
// machinery; this supplies the interface.
//
// The provider itself is a forwarder, not an implementation. WinUI ships
// XamlControlsXamlMetaDataProvider, which already knows every MUX type, and it is
// activatable — so the three methods pass their raw ABI words straight through to it.
// Reimplementing the XAML type system in Go would be a great deal of work to arrive at
// a worse copy of a thing already in the box.

import (
	"fmt"
	"syscall"
	"unsafe"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	uixamlmarkup "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/markup"
	uixamltypeinfo "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml/xamltypeinfo"
	"github.com/deploymenttheory/go-bindings-winrt/bindings/runtime/winrt"
)

// providerSlots are IXamlMetadataProvider's three methods, in vtable order. Each takes
// two ABI words, so forwarding needs no knowledge of their shapes:
//
//	slot 6  GetXamlType(TypeName*, IXamlType**)
//	slot 7  GetXamlTypeByFullName(HSTRING, IXamlType**)
//	slot 8  GetXmlnsDefinitions(uint32*, XmlnsDefinition**)
//
// The slots are pinned against the committed winmd in internal/verify.
const (
	providerFirstSlot = 6
	providerSlotCount = 3
	providerArgWords  = 2
)

// DerivedApplication is a Go application object aggregating Microsoft.UI.Xaml
// .Application, answering IXamlMetadataProvider on the framework's behalf.
type DerivedApplication struct {
	implementation *winrt.Implementation
	// provider is WinUI's own metadata provider, which every call forwards to. Held
	// for the object's lifetime because the forwarders dereference it.
	provider *uixamlmarkup.IXamlMetadataProvider
	// Application is the aggregate's default interface — the thing Application.Start
	// and everything else expects.
	Application *uixaml.IApplication
}

// NewDerivedApplication builds the application and aggregates the runtime class onto
// it. Call it inside the Application.Start callback, where a XAML application may be
// constructed at all.
func NewDerivedApplication() (*DerivedApplication, error) {
	metadata, err := uixamltypeinfo.NewXamlControlsXamlMetaDataProvider()
	if err != nil {
		return nil, fmt.Errorf("app: activating WinUI's XamlControlsXamlMetaDataProvider: %w", err)
	}
	// The class's default interface is IXamlMetadataProvider already, but query for it
	// explicitly rather than assume: the forwarders dispatch through this pointer's
	// vtable, and a wrong one is silent until a slot is called.
	provider, err := winrt.QueryInterface[uixamlmarkup.IXamlMetadataProvider](
		unsafe.Pointer(metadata), &uixamlmarkup.IID_IXamlMetadataProvider)
	metadata.Release()
	if err != nil {
		return nil, fmt.Errorf("app: WinUI's metadata provider does not implement IXamlMetadataProvider: %w", err)
	}

	derived := &DerivedApplication{provider: provider}
	methods := make([]winrt.Method, providerSlotCount)
	for index := range methods {
		methods[index] = derived.forward(providerFirstSlot + index)
	}

	derived.implementation, err = winrt.NewImplementation("Microsoft.UI.Xaml.Application",
		winrt.Interface{IID: uixamlmarkup.IID_IXamlMetadataProvider, Methods: methods})
	if err != nil {
		provider.Release()
		return nil, err
	}

	if err := derived.aggregate(); err != nil {
		derived.implementation.Close()
		provider.Release()
		return nil, err
	}
	return derived, nil
}

// forward returns a method body that re-dispatches its words to WinUI's provider.
//
// Raw pass-through: the arguments are already in ABI form, and the only thing this side
// knows — or needs to — is how many words the slot takes.
func (d *DerivedApplication) forward(slot int) winrt.Method {
	return func(args []uintptr) uintptr {
		target := d.provider
		result, _, _ := syscall.SyscallN(target.LpVtbl[slot],
			append([]uintptr{uintptr(unsafe.Pointer(target))}, args[:providerArgWords]...)...)
		return result
	}
}

// aggregate makes the Go object the controlling outer of Microsoft.UI.Xaml.Application.
func (d *DerivedApplication) aggregate() error {
	factoryUnknown, err := winrt.GetActivationFactory("Microsoft.UI.Xaml.Application", &uixaml.IID_IApplicationFactory)
	if err != nil {
		return fmt.Errorf("app: the Application activation factory: %w", err)
	}
	factory := (*uixaml.IApplicationFactory)(unsafe.Pointer(factoryUnknown))
	defer factory.Release()

	return d.implementation.Aggregate(func(outer *syswinrt.IInspectable, inner **syswinrt.IInspectable) error {
		application, err := factory.CreateInstance(outer, inner)
		if err != nil {
			return err
		}
		d.Application = application
		return nil
	})
}

// Close drops the caller's reference. The framework holds its own for as long as the
// application is running, so this does not tear anything down mid-flight.
func (d *DerivedApplication) Close() {
	if d.Application != nil {
		d.Application.Release()
		d.Application = nil
	}
	if d.implementation != nil {
		d.implementation.Close()
		d.implementation = nil
	}
	if d.provider != nil {
		d.provider.Release()
		d.provider = nil
	}
}

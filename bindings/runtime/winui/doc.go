//go:build windows && amd64

// Package winui is the hand-written runtime layer for the Windows App SDK
// bindings: everything generated code needs that cannot itself be generated.
//
// It is currently a placeholder. What lands here, in order:
//
//   - Bootstrapping. WinUI 3 types are not registered with the operating
//     system the way Windows.* types are, so RoGetActivationFactory cannot
//     find them until the Windows App SDK framework package has been added to
//     the process. An app that is not packaged does that by calling
//     MddBootstrapInitialize2, exported from a DLL it has to ship itself.
//   - Apartment control. XAML requires a single-threaded apartment and keeps
//     its state per UI thread, so that thread has to be locked and
//     initialized before anything is activated.
//   - The application loop, wrapping Application.Start.
//
// The layer sits on go-bindings-winrt for activation, strings, delegates and
// collections, and on go-bindings-win32 for the COM and WinRT ABI beneath
// that. Neither is re-implemented here.
//
// # Architecture
//
// amd64 only, for the reason recorded in go-bindings-winrt: Go's
// asm_windows_arm64.s never loads the V registers, so on Windows on ARM every
// float and every all-float struct crossing syscall.SyscallN would be
// silently corrupted. XAML's surface is full of both — Opacity, Width,
// Thickness, Point.
package winui

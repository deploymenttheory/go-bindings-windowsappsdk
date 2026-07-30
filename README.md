# go-bindings-windowsappsdk

Go bindings for **WinUI 3** and the rest of the [Windows App
SDK](https://learn.microsoft.com/windows/apps/windows-app-sdk/) — the
`Microsoft.*` namespaces — so a Windows desktop application with a native user
interface can be written in Go.

No Visual Studio, no .NET SDK, no XAML compiler, no cgo. Just `go build`.

> **Status: early, but it works.** 77 packages of generated Go, and a real WinUI 3
> window on screen from a Go program, in CI. See [Where this is](#where-this-is)
> for the one thing that does not work yet and why.

## The family

This is the fifth of a set of Go binding generators that share one approach:
metadata in, deterministic Go out, with CI checking that regenerating produces
exactly what is committed.

| Repository | Metadata source | Provides |
|---|---|---|
| [go-winmd](https://github.com/deploymenttheory/go-winmd) | — | the shared ECMA-335 `.winmd` reader |
| [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32) | `Microsoft.Windows.SDK.Win32Metadata` | the Win32 surface, and the COM/WinRT ABI beneath everything else |
| [go-bindings-wdk](https://github.com/deploymenttheory/go-bindings-wdk) | `microsoft/wdkmetadata` | the driver-kit surface |
| [go-bindings-winrt](https://github.com/deploymenttheory/go-bindings-winrt) | `Microsoft.Windows.SDK.Contracts` | the `Windows.*` WinRT namespaces |
| **this repository** | **`Microsoft.WindowsAppSDK`** | **the `Microsoft.*` namespaces: WinUI 3, windowing, notifications, app lifecycle** |

Each layer builds on the ones above it. This repository does not re-implement
activation, strings, delegates or collections — go-bindings-winrt already does
all of that, and its types are used directly.

## What makes this different from the others

The repositories above project metadata that ships with Windows. The Windows
App SDK does not: it is a separately versioned redistributable, and that
changes three things.

**The metadata comes from NuGet, split across packages.**
`Microsoft.WindowsAppSDK` is a meta-package with nine components, and the
winmds live under `metadata/` inside them — `Microsoft.UI.Xaml.winmd` in the
WinUI component, twenty more in Foundation.

**The types are not registered with the operating system.**
`RoGetActivationFactory` cannot find `Microsoft.UI.Xaml.Window` the way it
finds `Windows.Globalization.Calendar`. An application that is not packaged
has to add the Windows App SDK framework package to its own process first, by
calling `MddBootstrapInitialize2` from a DLL it ships beside the executable.
That DLL is not already on the machine; it exists only in the NuGet package.

**XAML has thread affinity.** It requires a single-threaded apartment and
keeps its state per UI thread, so that thread has to be locked and initialized
before anything is activated. Work arriving from any other goroutine has to be
posted to it through a `DispatcherQueue`.

The groundwork for the third point is already released. go-bindings-winrt
v0.4.0 added per-thread apartment initialization, an opt-in for running
callbacks on the thread that invoked them, and zero-parameter delegates —
without which `DispatcherQueue.TryEnqueue` could not be called at all.

## Where this is

The generator runs end to end. 36 winmds are committed, covering **77 namespaces
and 4,374 types** — 2,568 interfaces, 1,286 classes, 392 enums, 69 structs, 59
delegates — and they become **77 Go packages, 384 files, all compiling**.

```console
$ go run ./cmd/generate ingest
Windows.* universe: 12643 types from github.com/deploymenttheory/go-bindings-winrt v0.4.0
ingested 77 namespaces → metadata\wasdk (32 diagnostics)

$ go run ./cmd/generate validate --external
validate: 77 namespaces, 0 errors, 0 warnings
external: 4500 references resolved against github.com/deploymenttheory/go-bindings-winrt v0.4.0

$ go run ./cmd/generate bindings
severed 18 import edges across 8 namespaces (references over them degrade)
emitted 77 packages → bindings\winui (755 diagnostics)

$ go build ./...
```

**Every reference resolves.** There are no unresolved type references anywhere,
and 4,500 `Windows.*` references resolve into the pinned go-bindings-winrt. The
only references that go nowhere are to `Microsoft.Web.WebView2.Core`, which ships
in its own NuGet package and has no Go bindings — recorded as a permanent absence
rather than left to look like a bug.

The 755 diagnostics are members that cannot be represented, each with a reason,
and each leaving an audit comment at its own vtable slot so nothing renumbers.
The largest groups are conformant arrays (255), severed import cycles (191) and
open generics (123). CI ratchets the set: a new one fails the build.

Inherited members are reachable. A class's metadata lists only the interfaces
declared at its own level — `Button`'s is just `IButton`, carrying `Flyout` — so the
generator walks the `Extends` chain and projects each base's interfaces as query
methods. `Button` gains fifteen, reaching `ContentControl` (`Content`), `Control`
(`FontSize`), `FrameworkElement` (`Margin`), `UIElement` (`Visibility`) and
`DependencyObject`:

```go
button, _ := controls.NewButton()
content, _ := button.AsContentControl()
_ = content.SetContent(text)          // Content, three classes up
element, _ := button.AsUIElement()
_ = element.SetOpacity(0.5)           // Opacity, four classes up
```

Twenty import edges are severed to break cycles Go cannot express, so on those the
accessor is absent. The capability is not: a consuming package closes no cycle, so
`winrt.QueryInterface[T]` reaches the interface directly.

### A window on screen

`acceptance/` puts one there and CI runs it, on a runner with the Windows App SDK
runtime installed. `app.Run` handles the startup order, which is not
interchangeable — lock the thread, enter a single-threaded apartment, register the
thread delegate bodies run on, bootstrap, then `Application.Start`:

```go
app.Run(func(application *uixaml.IApplication) error {
    window, _ := uixaml.NewWindow()
    window.SetTitle("Hello from Go")

    button, _ := uixamlcontrols.NewButton()
    base, _ := winrt.QueryInterface[uixamlprimitives.IButtonBase](
        unsafe.Pointer(button), &uixamlprimitives.IID_IButtonBase)
    handler, _ := uixamlprimitives.NewRoutedEventHandler(
        func(sender *syswinrt.IInspectable, e *uixaml.IRoutedEventArgs) {
            // called by the framework, on the UI thread
        })
    base.AddClick(handler)

    element, _ := button.AsUIElement()
    window.SetContent(element)
    return window.Activate()
}, app.Options{})
```

The one thing that does not work is merging `XamlControlsResources`, so controls
render unstyled. `Application.Resources` returns `E_UNEXPECTED` on an application
created with a null controlling outer, which is the only way this module can create
one yet. It is not a projection bug — `get_Resources` is slot 6, that is pinned
against the winmd, and every other member of the same interface pointer works — it
is the first concrete thing that needs **deriving** from a WinRT class, which is
M7. A test asserts the failure, so the day it starts working is visible.

The order of work, and why:

| | Milestone | Notes |
|---|---|---|
| M0 | Module, dependencies, CI | done |
| M1 | **Feasibility spike** | done — a Go executable bootstraps the SDK and activates `Microsoft.UI.Xaml` types, in CI. |
| M2 | `fetch-metadata` | done — resolves the nine-package fan-out from the `.nuspec`; winmds and bootstrapper. |
| M3 | `ingest` | done — winmds → committed JSON, `Windows.*` resolved against go-bindings-winrt. |
| M4 | `bindings` | done — JSON → 77 packages of compiling Go. |
| M5 | Class hierarchy | done — the `Extends` chain, without which a `Button` could not reach `Content`. |
| M6 | Runtime layer | done — apartment control and the application loop; a window on screen in CI. |
| **M7** | Deriving from WinRT classes | ← you are here. Needed for custom controls, and for `Application.Resources`; requires COM aggregation in go-bindings-winrt first. |

M1 was a gate rather than a step. Everything after it depended on questions
only it could answer, so it came before any generator work: if a Go executable
could not bootstrap the Windows App SDK and reach an activation factory, the
approach would have needed rethinking — much better learned from a few hundred
lines than after building a generator.

## Requirements

- Windows, **amd64 only**. arm64 is excluded deliberately; see
  [CLAUDE.md](CLAUDE.md#architecture-support).
- Go 1.25 or newer.
- The Windows App SDK runtime, at run time. Nothing extra is needed to build.

## Licence

MIT. See [LICENSE](LICENSE).

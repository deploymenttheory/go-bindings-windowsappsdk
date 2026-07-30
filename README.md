# go-bindings-windowsappsdk

Go bindings for **WinUI 3** and the rest of the [Windows App
SDK](https://learn.microsoft.com/windows/apps/windows-app-sdk/) — the
`Microsoft.*` namespaces — so a Windows desktop application with a native user
interface can be written in Go.

No Visual Studio, no .NET SDK, no XAML compiler, no cgo. Just `go build`.

> **Status: early, and it works.** 64 packages of generated Go, and a real WinUI 3
> window with styled controls on screen from a Go program, responding to clicks, the
> mouse and the keyboard, in CI. See [Where this is](#where-this-is).

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
delegates — and they become **64 Go packages, 309 files, all compiling**.

```console
$ go run ./cmd/generate ingest
Windows.* universe: 12643 types from github.com/deploymenttheory/go-bindings-winrt v0.4.0
ingested 77 namespaces → metadata\wasdk (32 diagnostics)

$ go run ./cmd/generate validate --external
validate: 77 namespaces, 0 errors, 0 warnings
external: 4500 references resolved against github.com/deploymenttheory/go-bindings-winrt v0.4.0

$ go run ./cmd/generate bindings
merged 14 mutually recursive namespaces into one package: Microsoft.UI.Xaml
emitted 64 packages → bindings\winui (135 diagnostics)

$ go build ./...
```

**Every reference resolves.** There are no unresolved type references anywhere,
and 4,500 `Windows.*` references resolve into the pinned go-bindings-winrt. The
only references that go nowhere are to `Microsoft.Web.WebView2.Core`, which ships
in its own NuGet package and has no Go bindings — recorded as a permanent absence
rather than left to look like a bug.

The 135 diagnostics are members that cannot be represented, each with a reason, and
each leaving an audit comment at its own vtable slot so nothing renumbers. The largest
groups are delegate TypeDefs (59) and returned delegates (55). CI ratchets the set: a
new one fails the build.

Most of that is not burn-down at all. The 59 delegate TypeDefs are deliberately not
emitted in their home namespace, because consumers ground their own copies. The 55
returned delegates are `IAsyncOperation<T>.Completed`'s *getter* and its siblings:
handing a native delegate back to Go has nothing behind it to call, and the setter and
`GetResults` both lower, so the async surface is usable without it.

**Generic collections used to be the single biggest silent gap.** Thirty XAML classes
have a generic instantiation as their only interface — `UIElementCollection`'s is
`IVector<UIElement>` — and the emitter refused them, so `Panel.Children`,
`Grid.RowDefinitions`, `TextBlock.Inlines`, `ItemsControl.Items` and
`Storyboard.Children` all came back as bare `IInspectable`. Silently: a property whose
type is an un-emitted class is not itself a diagnostic. Now:

```go
children, _ := panel.Children()      // *IVectorOfUIElement
children.Append(element)             // no QueryInterface, no uintptr
```

A second bug sat underneath it. Instantiations are deduped per package by mangled name,
guarded by a check that the same name means the same type — and that check compared
whole `TypeRef`s, including the flag saying whether the definition is in
go-bindings-winrt. That flag records *whose metadata the reference was read from*, not
which type it is: `IAsyncOperation<Bool>` is external read from this module's JSON and
local when it surfaces through go-bindings-winrt's own IR, because inside that module
`Windows.Foundation` is local. So 72 identical instantiations were rejected as name
collisions. Identity is now compared field by field, deliberately excluding provenance,
and the real collision case is still caught — two `TextRange` types from different
namespaces both mangle to `IVectorOfTextRange` and must not share one Go type.

**Import cycles used to be the largest group at 185, and are now gone.** Fourteen XAML
namespaces reference each other in every direction — `Microsoft.UI.Xaml` names
`Controls.Panel`, `Controls` names `UIElement`, both name `Input.PointerRoutedEventArgs`
which names them back. One Go package per namespace made that a set of import cycles,
which Go rejects, so edges were severed and every reference over them degraded to an
opaque word.

Severing was never cheap. The cheapest edge by reference count was
`Microsoft.UI.Xaml → Microsoft.UI.Xaml.Input`, and cutting it removed **all eighteen**
pointer, keyboard and manipulation events on `UIElement`, because their argument types
live there. `Button` could not reach `ButtonBase`, so `Click` needed a hand-written
`QueryInterface`.

The fix is to stop treating the namespace as the unit of packaging. Go's package is its
unit of mutual recursion, the way C#'s assembly is — and all fourteen namespaces ship in
one assembly. So the generator computes the strongly-connected components of the
namespace reference graph and emits **one Go package per component**:

```go
import uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"

element, _ := panel.AsUIElement()
handler, _ := uixaml.NewPointerEventHandler(
    func(sender *syswinrt.IInspectable, e *uixaml.IPointerRoutedEventArgs) { ... })
element.AddPointerEntered(handler)      // Xaml -> Xaml.Input, no import to make
```

The other 63 namespaces are acyclic and keep their own package; only the XAML core
merges. `internal/codegen/pipeline` asserts both halves, and asserts that the cycle
breaker afterwards finds **nothing left to sever** — if it ever does, an edge is missing
from the component computation rather than a cut being warranted.

That one change cleared 185 `import-cycle-skipped`, all 75 `inherited-interface-skipped`
(the same limit seen from the derived class) and 70 of the 78 `event-delegate-unloweable`.
The eight that remain all take `Microsoft.Web.WebView2.Core` types as handler arguments.

**Conformant arrays used to be the largest group at 256, and are also gone.** A WinRT
array crosses as two ABI words, a count and a data pointer, and which two depends on
who allocates the buffer:

| Shape | ABI | Go |
|---|---|---|
| `[in] T[]` | `(UINT32 size, T* value)` | `M(items []T) error` |
| `[out] T[]` fill | `(UINT32 size, T* value)` | `M(items []T) (uint32, error)` |
| `T[]` returned | `(UINT32* size, T** value)` | `M() ([]T, error)` |

The metadata distinguishes the third from the second by `ELEMENT_TYPE_BYREF` alone,
and there is exactly **one** byref array parameter in the whole Windows App SDK — which
is a reason to record the bit rather than ignore it, since passing a count where the
callee writes through a pointer corrupts memory silently. That one member is refused
rather than guessed at.

**String arrays convert element by element**, because a `[]string` can never be a view
of a buffer of `HSTRING` handles. The emitter builds a parallel handle buffer beside the
caller's slice, and who owns the handles differs per shape — which is the whole of it:

| Shape | Who creates the handles | What the body does |
|---|---|---|
| `[in] String[]` | this side | `NewHString` per element, `defer Close` each |
| `[out] String[]` fill | the callee | `TakeHString` each into the caller's slice |
| `String[]` returned | the callee, buffer included | `TakeHString` each, then `CoTaskMemFree` the buffer |

Slots the callee never wrote stay null, and taking a null handle yields `""` without
deleting anything, so the fill conversion covers the whole slice without needing the
count first. `acceptance/` drives all of this against live objects — a real
`Calendar.Languages()` and a six-element vector including an empty string and a
non-ASCII one — because a leak, a double free and an off-by-one all compile.

The one array still refused is `ITextRangeProvider.GetBoundingRectangles`, the single
callee-allocated `[out]` array in the metadata.

**`[out]` parameters convert both ways now.** An `HSTRING` out-parameter transfers
ownership, so the generated body declares the raw slot, calls, and takes the handle:

```go
func (self *ITextRange) GetText(options TextGetOptions, value *string) error {
	_valueRaw := new(syswinrt.HSTRING)
	r1, _, _ := syscall.SyscallN(self.LpVtbl[40], ...)
	if err := win32.ErrIfFailed(int32(r1)); err != nil {
		return err
	}
	*value = winrt.TakeHString(*_valueRaw)
	return nil
}
```

The conversion sits inside the success path deliberately: on a failed call the callee
wrote nothing, and converting the slot anyway would report an error *and* wipe the
caller's variable. `Bool` out-parameters go through a `byte` for the same reason a
`Bool` array element cannot be a direct view — one byte, with no guarantee it is 0 or 1.

A float out-parameter was refused by simple omission, which
`ICompositionPropertySet.TryGetScalar` paid for while every `TryGetVector3` beside it
worked. An `[out]` parameter is a pointer the callee writes through, so a `float32`
crosses through memory and never through XMM0.

**A struct field is different, and lands differently.** There is no boundary inside a
struct at which a conversion could run, so an `HSTRING` field is emitted as the handle,
with a doc comment saying what that obliges the caller to do. The alternative — a shadow
struct per type, marshalled at every signature naming one — would cost the property
conformant arrays depend on, that a `[]T` over emittable structs is a direct view of the
callee's buffer.

That change surfaced a wrong assumption worth recording. This module resolved a
`Windows.*` reference on the strength of the type appearing in go-bindings-winrt's
committed metadata — but the metadata says what a type *is*, and only the emitted source
says whether there is a Go declaration to name. `Windows.UI.Xaml.Interop.TypeName` is
`{ HSTRING; TypeKind }` and that module skipped it for the same reason this one did, so
the moment this one stopped refusing `HSTRING` fields, 31 members named an undeclared type
and the tree stopped compiling. The generator now checks the dependency's emitted packages
rather than its metadata.

go-bindings-winrt v0.4.1 emits `TypeName`, and the pin here moved with it, so those 31 are
back — `Frame.Navigate` and `ControlTemplate.TargetType` among them. The check stays: it is
what turns "the dependency does not provide this" from a build failure into a diagnostic,
and `Windows.Web.Http.HttpProgress` is still skipped over there, so it keeps a live
example.

Inherited members are reachable. A class's metadata lists only the interfaces
declared at its own level — `Button`'s is just `IButton`, carrying `Flyout` — so the
generator walks the `Extends` chain and projects each base's interfaces as query
methods. `Button` gains fifteen, reaching `ButtonBase` (`Click`), `ContentControl`
(`Content`), `Control` (`FontSize`), `FrameworkElement` (`Margin`), `UIElement`
(`Visibility`) and `DependencyObject`:

```go
button, _ := uixaml.NewButton()
content, _ := button.AsContentControl()
_ = content.SetContent(text)          // Content, three classes up
element, _ := button.AsUIElement()
_ = element.SetOpacity(0.5)           // Opacity, four classes up
```

Every one of those accessors is now present, because the classes they reach are in the
same package. Before clustering, the ones crossing a severed edge were absent.

### The ergonomic layer

Generated bindings are a faithful projection, not a pleasant one: a property declared
four classes up needs a `QueryInterface` and a `Release`, every setter returns an error,
and `Button.Content` takes a boxed `IInspectable` rather than a string.

The obvious fix is to promote inherited members onto the classes that inherit them. That
was measured before it was written, and it does not survive the measurement:

| what gets promoted | members |
|---|---|
| every interface on the class and its bases | 165,561 |
| default interfaces of the class and its bases | 75,046 |
| the same, properties only | 59,863 |

`Button` alone would gain 331 methods and `IUIElement` carries 183 by itself. That is not
an ergonomic surface, it is an unusable autocomplete list attached to a generated tree
several times its current size.

So `app` removes the *ceremony* instead, with generic combinators that name no control
and need no maintenance as WinUI grows:

```go
app.All(errs...)                      // a block of setters, one error
app.With(obj.AsFoo, func(f *IFoo) …)  // query, run, release — any class, any interface
app.On(x.AddClick, NewHandler, fn)    // ground a delegate and register it
app.Box(v)                            // a Go value as the IInspectable WinRT wants
app.Append(panel.AsPanel, children…)  // into a Panel's Children
```

`examples/clicker` went from 233 lines to 140 doing exactly the same work — a window
with a styled `Button` and `TextBlock`, responding to clicks, the pointer and the
keyboard. Everything still takes and returns the generated types, so any call can be
written the long way and mixed freely; there is no parallel API to escape from.

### Known limitation: text input

`TextBox`, `PasswordBox`, `RichEditBox` and `AutoSuggestBox` **terminate the process**
when they are laid out, with `0xC000027B` — a stowed WinRT exception from inside the
framework. `TextBlock`, `Button`, `CheckBox`, `Slider`, `ListView` and `Grid` are fine.

Construction succeeds, properties set without error, and the element goes into the tree;
the process dies once layout runs. The cause is not established and is not guessed at.
`acceptance/textinput_test.go` pins it in a subprocess, with a `TextBlock` control case
beside it, so it is tracked rather than folklore.

### Try it

```sh
go run ./cmd/generate fetch-bootstrap   # the redistributable, not committed
go run ./examples/clicker
```

A window with a `TextBlock` and a `Button`. Clicking the button runs a Go closure that
updates the label; so do moving the mouse over the panel and pressing a key. The
framework invokes all three on the UI thread, and they touch XAML objects directly. About
a hundred lines, and it exercises the whole stack: composable construction, the
base-class chain (`Content` is three classes above `Button`, `Children` one above
`StackPanel`), grounded delegates, `UIElement`'s input events, and a monomorphized
`IVector<UIElement>`.

Needs the Windows App SDK 2.x runtime installed.

### A window on screen

`acceptance/` puts one there and CI runs it, on a runner with the Windows App SDK
runtime installed. `app.Run` handles the startup order, which is not
interchangeable — lock the thread, enter a single-threaded apartment, register the
thread delegate bodies run on, bootstrap, `Application.Start`, then create the first
window:

```go
app.Run(func(ready *app.Ready) error {
    window := ready.Window
    window.SetTitle("Hello from Go")

    button, _ := uixaml.NewButton()
    base, _ := button.AsButtonBase()
    handler, _ := uixaml.NewRoutedEventHandler(
        func(sender *syswinrt.IInspectable, e *uixaml.IRoutedEventArgs) {
            // called by the framework, on the UI thread
        })
    base.AddClick(handler)

    element, _ := button.AsUIElement()
    window.SetContent(element)
    return window.Activate()
}, app.Options{})
```

`Run` creates the window because the order is not discoverable:
`Application.Resources` is unavailable until the XAML core is up, and the first
`Window` is what brings it up. Getting that wrong returns `E_UNEXPECTED`, which reads
like a broken binding and is not one.

**The controls are styled.** Measured at `Loaded` — the earliest point at which a size
means anything — a `Button` has a real template and a real size, with no
`XamlControlsResources` merged. WinUI 3 ships its default styles in the framework
package, unlike WinUI 2 where they came from a NuGet library and had to be merged.

One thing genuinely does not work: activating `XamlControlsResources` returns `E_FAIL`.
The cause is unknown, but it is **not** the projection (a plain `ResourceDictionary`
and ordinary controls activate fine) and **not** a missing `IXamlMetadataProvider`
(`XamlReader` resolves and instantiates framework types from markup). Its remaining job
in WinUI 3 is `UseCompactResources`, so this is a gap to note rather than a blocker.

That conclusion cost two wrong answers before a spike settled it, which is why
`acceptance/styling_test.go` keeps the discriminators as tests. The practical upshot:
**COM aggregation is not needed for a working UI.**

The order of work, and why:

| | Milestone | Notes |
|---|---|---|
| M0 | Module, dependencies, CI | done |
| M1 | **Feasibility spike** | done — a Go executable bootstraps the SDK and activates `Microsoft.UI.Xaml` types, in CI. |
| M2 | `fetch-metadata` | done — resolves the nine-package fan-out from the `.nuspec`; winmds and bootstrapper. |
| M3 | `ingest` | done — winmds → committed JSON, `Windows.*` resolved against go-bindings-winrt. |
| M4 | `bindings` | done — JSON → 64 packages of compiling Go. |
| M5 | Class hierarchy | done — the `Extends` chain, without which a `Button` could not reach `Content`. |
| M6 | Runtime layer | done — apartment control and the application loop; a window on screen in CI. |
| M7 | Deriving from WinRT classes | Needed for custom controls and for XAML types an application defines itself — **not** for a working UI, which a spike established. Requires COM aggregation in go-bindings-winrt. |

M0 through M6 are done: the pipeline runs end to end and produces a usable UI. What is
left is not a blocker but a queue — the ergonomic layer over the generated bindings, a
thin residue of diagnostics (135, of which 114 are deliberate policy rather than gaps),
and M7 for anyone who needs a custom control.

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

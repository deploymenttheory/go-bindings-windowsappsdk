# CLAUDE.md

Guidance for Claude Code (claude.ai/code) working in this repository.

## What this is

`go-bindings-windowsappsdk` provides Go bindings for the **Windows App
SDK** — the `Microsoft.*` namespaces, of which WinUI 3
(`Microsoft.UI.Xaml.*`) is the reason the repository exists. It is the fifth
member of the deploymenttheory Windows bindings family and sits on top of
three of them:

- [go-winmd](https://github.com/deploymenttheory/go-winmd) — reads `.winmd`
  files, and (since v0.4.0) resolves NuGet meta-packages and extracts many
  files from one download.
- [go-bindings-win32](https://github.com/deploymenttheory/go-bindings-win32)
  — the COM and WinRT ABI: `HSTRING`, `IInspectable`, the `Ro*` functions,
  `HRESULT`, `GUID`, `IUnknown`.
- [go-bindings-winrt](https://github.com/deploymenttheory/go-bindings-winrt)
  — the `Windows.*` namespaces, plus the runtime this repository reuses
  wholesale: activation, `QueryInterface`, strings, Go-implemented delegates,
  collections, `Await`.

**None of that is re-implemented here.** A `Windows.*` type referenced by
Windows App SDK metadata resolves to the package go-bindings-winrt already
generated for it.

## Status

M0. The module, its pinned dependencies and CI exist. Nothing is generated
yet. `README.md` has the milestone order; the detail below describes the
design being built toward, and is written down now so it does not have to be
rediscovered.

## Commands

```sh
go build ./...
go vet ./...
go test ./...    # makes real WinRT calls; needs Windows
```

## Why this repository is not like the sibling ones

The other four project metadata that ships with Windows. The Windows App SDK
is a separately versioned redistributable, and three consequences follow.

### The metadata is a meta-package

`Microsoft.WindowsAppSDK` depends on nine component packages, and the set and
versions change with every servicing release, so they have to be read from
the `.nuspec` rather than hard-coded. Version constraints are not uniform
either: 2.3.1 pins its Runtime component exactly (`[2.3.1]`) and gives the
other eight open lower bounds (`2.3.5`), so anything that insists on exact
pins rejects the package outright.

The winmds are under `metadata/` inside each component, not `lib/uap10.0` as
the older layout suggests. Verified against 2.3.1:

| Component | Contents |
|---|---|
| `Microsoft.WindowsAppSDK.WinUI` | `Microsoft.UI.Xaml.winmd` (1.6 MB), `Microsoft.UI.Text.winmd` |
| `Microsoft.WindowsAppSDK.Foundation` | 20 winmds — AppLifecycle, ApplicationModel.*, AppNotifications, Storage.Pickers, Management.Deployment, System.* — **and the bootstrapper**, under `runtimes/win-x64/native/`, with `include/MddBootstrap.h` |
| `Microsoft.WindowsAppSDK.Base` | 90 KB, no winmds |

`Microsoft.UI.winmd` — which carries `Microsoft.UI.Dispatching`,
`.Composition`, `.Windowing`, `.Content` and `.Input` — is in neither of the
first two; it is most likely in `InteractiveExperiences`, and that needs
confirming before M2 is finished.

### The types are not registered with the operating system

`RoGetActivationFactory` finds `Windows.Globalization.Calendar` because the
operating system registered it. Nothing registers `Microsoft.UI.Xaml.Window`.
An application without an MSIX package has to add the Windows App SDK
framework package to its own process first, by calling
`MddBootstrapInitialize2`.

That function is exported from `Microsoft.WindowsAppRuntime.Bootstrap.dll`,
which **is not on the machine**. Checked directly: a recursive search of
`C:\Program Files\WindowsApps` finds no file matching `*Bootstrap*` at all,
even though the framework packages themselves are installed. It ships only in
the NuGet package and has to be redistributed beside the executable.

So it is fetched, never committed — `.gitignore` excludes `*.dll` and
`/metadata/bootstrap/` on purpose. Embedding it with `go:embed` was
considered and rejected: it would commit a Microsoft binary into a Go module
that every consumer then downloads, and writing an executable to a temporary
directory in order to load it is a poor pattern regardless.

The DLL also cannot be loaded through go-bindings-win32's loader, which only
searches `System32` as a defence against DLL preloading. The bootstrapper is
application-local by design, so this repository needs its own loader taking an
explicit absolute path, with that difference written down where it is defined.

### XAML has thread affinity

XAML requires a single-threaded apartment and keeps its state per UI thread.
Generated bindings call straight through vtable slots with no COM proxy in
between, so touching a XAML object from the wrong thread is an unmarshalled
cross-apartment call, not a slow one.

Three things in go-bindings-winrt v0.4.0 exist for this:

- **`Initialize()` is per thread.** It was a process-wide `sync.Once`, which
  left every thread after the first uninitialized.
- **`SetInlineThread(tid)`** runs delegate bodies on the thread that invoked
  them rather than handing them to a new goroutine.
- **Zero-parameter delegates.** `DispatcherQueueHandler` has a parameterless
  `Invoke`, and it is what every `TryEnqueue` takes — without it there is no
  way to move work onto the UI thread at all.

The startup order matters and is not interchangeable:

```text
runtime.LockOSThread()
RoInitialize(single-threaded apartment)   ← before anything is activated
winrt.SetInlineThread(current thread)
MddBootstrapInitialize2(...)              ← COM must already be initialized
Application.Start(callback)               ← blocks
    └── inside the callback: create the Application, then the Window
```

`Application.Start` creates the `DispatcherQueueController` itself;
`CreateDispatcherQueueController` should not be called by hand.

One trap worth knowing before drawing any conclusion from a window that looks
wrong: `XamlControlsResources` has to be added to
`Application.Current.Resources.MergedDictionaries` at startup, or controls
render unstyled and every `{ThemeResource}` lookup fails while parsing. That
is the usual cause of "XamlReader.Load does not work in WinUI 3" reports — a
resources problem wearing a parser's clothes.

## The design being built toward

### Layout

```text
cmd/generate/          fetch-metadata | fetch-bootstrap | ingest | validate | bindings | diff | list
internal/
  wasdkmeta/           the metadata model, and the code that produces it
  codegen/             typemap, naming, pipeline, fileasm, the emitter
  diagnostics/         the list of members that cannot be generated
  verify/              slot and IID checks against the committed winmds
metadata/
  winmd/               committed winmds + PROVENANCE.json
  winmd-roots.txt      which winmds to read (excludes *Internal*, *Private*)
  emit-roots.txt       which namespaces to generate
  wasdk/               committed JSON, one file per namespace
  bootstrap/           NOT committed: the bootstrapper DLL and SDK headers
bindings/
  runtime/winui/       hand-written; imports nothing generated
  winui/<namespace>/   generated; never hand-edited
app/                   the ergonomic layer; imports generated code
acceptance/            tests against live WinRT
```

Namespaces drop the `Microsoft.` prefix: `Microsoft.UI.Xaml.Controls` becomes
`bindings/winui/ui/xaml/controls`, package `controls`.

### Two different kinds of external reference

These look alike and are not.

**Sibling winmds are not external.** `Microsoft.UI.Xaml.winmd` references
`Microsoft.UI.Dispatching`, `.Composition`, `.Input`, `.Windowing` and
`.Text`, which live in other winmds *in this repository*. Ingest has to run in
two passes: classify every type in every winmd, then re-read each one with the
union of those classifications available.

**`Windows.*` is genuinely external**, and resolves into go-bindings-winrt.
The type universe for it comes from that module's committed JSON, read out of
the module cache — which means `go.mod` is the version pin, and the metadata
this repository generates against is exactly the metadata that produced the
bindings it imports.

A worry that turns out not to apply: matching contract versions. WinRT
contracts only ever add, so a newer contract always resolves an older
assembly's references. What does need checking is that every external
reference resolves at all, which `validate --external` should enforce as a
hard error.

**`Microsoft.Web.WebView2.Core`** is referenced by the XAML metadata but is
neither a Windows App SDK winmd nor part of the WinRT contracts. It has no Go
equivalent, and members using it must be skipped with a distinct reason
rather than quietly turned into `uintptr` — a binding that compiles and then
crashes is worse than one that is absent.

### An import alias collision to avoid

After the root prefix is stripped, this repository's `Microsoft.UI.Xaml.Interop`
and go-bindings-winrt's `Windows.UI.Xaml.Interop` both become the alias
`uixamlinterop`. The same is true of `UI.Xaml.Data`, `UI.Xaml.Markup`,
`UI.Text` and `UI` itself. External aliases need a distinguishing prefix, and
`validate` should check that no two aliases in a package point at different
paths — otherwise this appears as a compile failure across roughly thirty
packages on the first full run.

## Architecture support

Every file carries `//go:build windows && amd64`.

arm64 is excluded rather than generated for. Go's `asm_windows_arm64.s` loads
only R0–R7 and never touches V0–V7 — it still carries a
`TODO(rsc) floating point like amd64` note — so on Windows on ARM every
`double` (passed in V0) and every all-float struct such as `Point`, `Rect` or
`Vector3` (passed in V0–V3) would be silently corrupted. XAML's surface is
full of both: `Opacity`, `Width`, `Height`, `FontSize`, `Thickness`,
`Margin`. Generating for arm64 would mean shipping code that miscompiles
those, so the architecture is excluded until Go copies the arguments across.

CI checks the build tags rather than cross-compiling, because a wildcard
`GOARCH=arm64 go build` passes by building nothing.

## Conventions

- **Generated code is never hand-edited.** Fix the generator and regenerate.
  A generator change and its regenerated output belong in the same commit.
- **Regeneration must be reproducible** — byte for byte, or CI fails. Sort
  anything with unstable ordering, such as iteration over a Go map.
- **Never redeclare the ABI.** `HSTRING`, `IInspectable`, `HRESULT`, `GUID`
  and the rest come from go-bindings-win32 through go-bindings-winrt.
- **Write plainly.** Prefer Microsoft's own vocabulary — vtable slot,
  activation factory, apartment, `[Composable]`, controlling outer and inner
   — over invented shorthand, so the documentation can be read alongside
  Microsoft's. Say what a thing does before why it does it.
- Conventional commits, release-please, SHA-pinned GitHub Actions,
  LF-normalised text (`.gitattributes`), `*.winmd` and `*.dll` marked binary.

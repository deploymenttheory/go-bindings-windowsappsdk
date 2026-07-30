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

M6. The winmds are committed (36 files, 77 namespaces, 4,374 API types), ingest
projects them into committed JSON with every reference resolved, the emitter
produces **64 Go packages / 309 files** that compile, and `acceptance/` puts a real
WinUI 3 window on screen from Go, with the framework calling Go handlers back on the
UI thread for clicks, pointer moves and keystrokes.

The controls in it are **styled and rendered**: a `Button` measured at `Loaded` has a
template and a size. The one thing that does not work is activating
`XamlControlsResources`, which turns out not to be needed; see below.

135 diagnostics remain, down from 870. The largest categories were cleared by
[conformant arrays](#conformant-arrays) (256), [namespace
clusters](#namespace-clusters) (185 import cycles, 75 inherited interfaces, 70 event
delegates) and [generic instantiations](#generic-instantiations) (72 wrongly-rejected
duplicates, 30 collection classes).

`README.md` has the milestone order; the detail below describes the design being
built toward, and is written down now so it does not have to be rediscovered.

## Commands

```sh
go run ./cmd/generate fetch-metadata     # winmds ← NuGet meta-package fan-out
go run ./cmd/generate fetch-bootstrap    # the redistributable bootstrapper
go run ./cmd/generate ingest             # winmds → metadata/wasdk/*.json
go run ./cmd/generate validate --external
go run ./cmd/generate resolve             # every reference → a Go type, or a reason
go run ./cmd/generate bindings --diagnostics-baseline metadata/diagnostics-baseline.json
go run ./cmd/generate list
go run ./cmd/inspect --dir metadata/winmd --namespaces

go build ./...
go vet ./cmd/... ./internal/... ./bindings/runtime/...   # NOT ./... — see below
go test ./...    # makes real WinRT calls; needs Windows
```

`go vet ./...` does not pass, and should not be made to. The generated delegate
adapters convert a raw `uintptr` to a typed pointer, which trips vet's
`unsafe.Pointer` heuristic by design: that word IS a native object pointer the
event source owns, arriving through a COM callback, and there is no Go pointer
for it to have come from. Vet the hand-written packages.

The bootstrap tests need two things the repository cannot carry, and they fail
differently:

- **The bootstrapper DLL**, which is not committed. Its absence is *skipped*,
  because not having fetched it is a fact about the checkout.
- **An installed Windows App SDK framework package.** Without one,
  `MddBootstrapInitialize2` returns `0x80670016` — nothing is wrong with the
  call, the machine is missing the dependency. That gets its own error type
  naming the installer. CI installs the 2.3 runtime.

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
first two. **Confirmed: it is in `InteractiveExperiences`**, which pins to
2.1.3 while the meta-package is at 2.3.1. Components version independently, so
each namespace's provenance is recorded per namespace rather than once for the
tree.

That component also ships each winmd once per Windows SDK target version —
`metadata/10.0.17763.0/` and `metadata/10.0.18362.0/`, with differing contents,
because each describes the surface available at that minimum OS version. The
highest is kept: a caller needing an older floor can decide that for itself,
whereas API missing from the bindings cannot be recovered at all.

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

The startup order matters and is not interchangeable. `bindings/runtime/winui`
does the first four steps as `EnterUIThread`, and `app.Run` the rest:

```text
runtime.LockOSThread()
RoInitialize(single-threaded apartment)   ← before anything is activated
winrt.SetInlineThread(current thread)
MddBootstrapInitialize2(...)              ← COM must already be initialized
Application.Start(callback)               ← blocks
    └── inside the callback: create the Application,
        then the Window,          ← brings the XAML core up
        then merge resources      ← unavailable before the Window exists
```

`Application.Start` creates the `DispatcherQueueController` itself;
`CreateDispatcherQueueController` should not be called by hand.

The last two steps are why `app.Run` creates the window rather than letting the
caller do it: the ordering is invisible in the API and its violation looks like a
broken binding.

### What the live tests established

Verified against a real 2.3 runtime, in CI as well as locally. Each of these was
an open question and is now an assertion in `acceptance/`:

**The callback must create the Application.** `Application.Start` does *not*.
`Application.Current` is nil until the callback constructs one — which is what
every `Application.Start(_ => new App())` in the C# samples is doing, and easy to
misread as the framework's job.

**A plain, non-derived `Application` is enough.** This was the M1 question the
spike was written to answer, and the answer is yes: `NewApplication()` through the
composable factory with a null outer gives a working `Current`, and from it a
`Window`, a `Button`, content, activation, and a `DispatcherQueue`. Go-side
derivation is therefore not on the critical path for building a UI.

**A failed startup must arrange its own shutdown.** If the initialization callback
returns an error, nothing else can end the message loop — a cross-apartment `Exit`
is not an option — so `app.Run` calls `Exit` from inside the callback before
returning into the framework. Without that, a startup error hangs the process
instead of reporting itself.

**`Application.Resources` needs the first `Window` to exist.** It returns
`E_UNEXPECTED` before then and succeeds after: the resource dictionary is not
available until the XAML core is up, and creating the first `Window` is what brings
it up. `app.Run` therefore creates the window itself and hands it to the callback,
which is the only reason `Ready` carries one.

`TestResourcesRequireTheXamlCore` asserts **both** halves of that transition, and
that is not padding. An earlier version of this file explained the same failure as a
consequence of null-outer composition — confidently, with a citation to a pin in
`internal/verify` that did not exist — and it was simply wrong. Only the transition
distinguishes "too early" from "broken"; a test that saw the failure alone would have
supported the wrong story just as well.

**`XamlControlsResources` cannot be activated, and it does not matter.** With the
ordering correct, `Resources` succeeds and the next call fails instead — `E_FAIL`, at
every point tried. The cause is still unknown, but a spike established what it is
**not**:

- Not the projection. A plain `ResourceDictionary` and ordinary controls activate fine.
- Not a missing metadata provider. `XamlReader.Load` parses markup and instantiates
  framework types, and malformed markup fails with a genuine parser error — so XAML
  type resolution works without the application answering `IXamlMetadataProvider`.

And it is not load-bearing. WinUI 3 ships its default styles in the framework package,
unlike WinUI 2 where they came from a NuGet library and had to be merged. Measured at
`Loaded` — the earliest point at which a size means anything — a `Button` has a real
template and a real size. `XamlControlsResources`' remaining job in WinUI 3 is
`UseCompactResources`.

**This is where I was wrong twice, so it is worth being blunt about the method.** I
first explained the failure as null-outer composition, then as a missing metadata
provider, and asserted a `0×0` Button as evidence for both. The `0×0` was measured
before layout ran. Each explanation fitted the one observation I had and neither
survived a discriminator that took ten minutes to write. `acceptance/styling_test.go`
now holds those discriminators as tests, so the record cannot drift back.

The lesson worth keeping: **COM aggregation is not on the critical path for a working
UI.** Building it to fix styling would have fixed nothing. It is still what a Go
application would need to resolve types *it* defines — a real future concern, and a
different one.

The received wisdom that "controls render unstyled without `XamlControlsResources`",
and that a failing `XamlReader.Load` is a resources problem wearing a parser's clothes,
is **WinUI 2 advice**. It is what led me down the wrong path twice, and it does not
apply here.

## The design being built toward

### Layout

```text
cmd/generate/          fetch-metadata | fetch-bootstrap | ingest | validate | bindings | diff | list
cmd/inspect/           read a winmd directly: namespaces, types, IIDs, slots
internal/
  metaquery/           IID and vtable-slot lookup straight from the winmds
  wasdkmeta/           the metadata model, and the code that produces it
    external/          the Windows.* universe, read from the pinned module
    ingest/            winmds → the model
  codegen/             typemap, naming, pipeline, fileasm, the emitter
  diagnostics/         the list of members that cannot be generated
  verify/              slot and IID checks against the committed winmds
metadata/
  winmd/               committed winmds + PROVENANCE.json (which IS the pin)
  emit-roots.txt       which namespaces to generate
  wasdk/               committed JSON, one file per namespace
  bootstrap/           NOT committed: the bootstrapper DLL and SDK headers
bindings/
  runtime/winui/       hand-written; imports nothing generated
  winui/<package>/     generated; never hand-edited
app/                   the ergonomic layer; imports generated code
acceptance/            tests against live WinRT
```

Namespaces drop the `Microsoft.` prefix: `Microsoft.UI.Dispatching` becomes
`bindings/winui/ui/dispatching`, package `dispatching`. It is not one package per
namespace, though — see [Namespace clusters](#namespace-clusters): the fourteen
mutually recursive XAML namespaces share `bindings/winui/ui/xaml`, so
`Microsoft.UI.Xaml.Controls.Button` is `uixaml.Button`.

There is no `winmd-roots.txt`. An earlier sketch had one, but it would say
twice what is already decided elsewhere: `fetch-metadata` excludes
`*Internal*`/`*Private*` when downloading, and `PROVENANCE.json` records
exactly which files were kept, with the component and version each came from.
Ingest reads that list rather than globbing the directory, so a file dropped in
by hand is not silently projected.

### Two different kinds of external reference

These look alike and are not.

**Sibling winmds are not external.** `Microsoft.UI.Xaml.winmd` references
`Microsoft.UI.Dispatching`, `.Composition`, `.Input`, `.Windowing` and
`.Text`, which live in other winmds *in this repository*. Ingest classifies
every type in every winmd before projecting any of them, because a TypeRef
gives no hint whether its target is local — read one file at a time these are
indistinguishable from `Windows.*` references, and projecting a sibling as
foreign would be silent.

**`Windows.*` is genuinely external**, and resolves into go-bindings-winrt.
The type universe for it comes from that module's committed JSON, read out of
the module cache via `go list -m -f '{{.Dir}}'` — which means `go.mod` is the
version pin, and the metadata this repository generates against is exactly the
metadata that produced the bindings it imports. `go list` is asked rather than
the cache path assembled by hand, because the answer differs with `GOMODCACHE`,
with a `replace` directive and with vendoring.

That universe is loaded *first* and seeds the classification index, so both
kinds of external reference are settled in one pass. The decision is then
recorded on each `TypeRef` as `external: true` and committed, rather than
re-derived at emit from the namespace prefix: `Windows.*` is external while
`Microsoft.Windows.*` is local, and prefix logic is exactly the sort of thing
that goes subtly wrong.

A local winmd defining a `Windows.*` type is a hard error. It would give this
module a second, incompatible definition of a type every signature already
shares.

A worry that turns out not to apply: matching contract versions. WinRT
contracts only ever add, so a newer contract always resolves an older
assembly's references. What does need checking is that every external
reference resolves at all, which `validate --external` enforces as a hard
error — a miss means the Windows App SDK metadata is newer than the pin, and
the fix is to bump it rather than work around the reference.

**`Microsoft.Web.WebView2.Core`** is referenced by the XAML metadata but is
neither a Windows App SDK winmd nor part of the WinRT contracts. It has no Go
equivalent, so it is listed in `ingest.KnownForeignNamespaces` and its
references arrive at emit with neither `external` nor a `target_kind`: an
emitter seeing the first would import a package that does not exist, and one
seeing the second would name a type that was never generated. Members using it
must be skipped with a distinct reason rather than quietly turned into
`uintptr` — a binding that compiles and then crashes is worse than one that is
absent.

### Generic instantiations

A Go type parameterized at the ABI level does not exist, so `IVector`1<UIElement>`
becomes a concrete `IVectorOfUIElement` **monomorphized into the consuming package**,
never a reference into go-bindings-winrt. Discovery is demand-driven through
`typemap.Context.RequestInstantiation` and transitively closed: substituting arguments
into an open interface surfaces more instantiations — `IVector<T>.GetView` yields
`IVectorView<T>` — which are queued and drained to a fixed point.

Instantiations are deduped per package by mangled name, and the guard on that dedupe is
where two lessons live.

**Identity is not the whole struct.** The guard compared `TypeRef`s with
`reflect.DeepEqual`, which includes `External` — and that field records *whose metadata
the reference was read from*, not which type it is. `IAsyncOperation`1<Bool>` is
`External: true` read from this module's JSON, and `External: false` when it surfaces
through go-bindings-winrt's own IR for `AsyncOperationCompletedHandler`1.Invoke`,
because inside that module `Windows.Foundation` is local. The guard therefore rejected
72 identical instantiations as name collisions, and every member naming one degraded.
`sameType` compares kind, namespace, name, array element and arguments, recursively, and
deliberately excludes provenance. `TargetKind` is excluded for the same reason: for a
fixed namespace and name it cannot disagree.

**The guard is still needed.** Mangled names drop namespaces, so
`Microsoft.UI.Xaml.Documents.TextRange` and `Microsoft.UI.Text.TextRange` both give
`IVectorOfTextRange`. Letting that through aliases two distinct IIDs onto one Go type,
which compiles and then fails at `QueryInterface`. Both halves are tested.

**A generic instantiation is a valid default interface.** Thirty XAML classes have
nothing else: `UIElementCollection`'s default is `IVector`1<UIElement>`,
`TransitionCollection`'s is `IVector`1<Transition>`. Two places refused them — the class
emitter and `typemap.resolveClassRef` — and the cost was not the classes but the
properties returning them: `Panel.Children`, `Grid.RowDefinitions`,
`TextBlock.Inlines`, `ItemsControl.Items` and `Storyboard.Children` all degraded to
`IInspectable`, **silently**, because a property whose type is an un-emitted class is
not itself a diagnostic. Resolution order matters: resolve the default through the seam
first, which registers the instantiation, then ask `iidRef` for its derived IID var.

**Returned delegates degrade on purpose**, under their own key
`delegate-return-skipped` rather than a generics key. Handing a native delegate back to
Go has nothing behind it to call. This is almost entirely
`IAsyncOperation`1<T>.get_Completed`; `put_Completed` and `GetResults` both lower, so
the async surface is usable without it. `Context.IsReturn` marks the case explicitly
rather than inferring it from an unwired seam, so the diagnostic says what is true.

### Namespace clusters

**One Go package per strongly-connected component of the namespace reference graph,
not one per namespace.**

Fourteen XAML namespaces reference each other in every direction.
`Microsoft.UI.Xaml` names `Controls.Panel`; `Controls` names `UIElement`; both name
`Input.PointerRoutedEventArgs`, which names them back. One package per namespace makes
that a set of import cycles, and Go rejects import cycles.

The earlier approach severed the cheapest edge in each cycle and degraded every
reference over it to an opaque word. That was never cheap. The cheapest edge by
reference count was `Microsoft.UI.Xaml → Microsoft.UI.Xaml.Input`, and cutting it
removed **all eighteen** pointer, keyboard and manipulation events on `UIElement`,
because their argument types live there. `Button` could not reach `ButtonBase`, so
`Click` needed a hand-written `QueryInterface`.

The namespace was the wrong unit. **Go's package is its unit of mutual recursion, the
way C#'s assembly is** — and all fourteen ship in one assembly, `Microsoft.UI.Xaml.winmd`.
Collapsing each component into one package makes the package graph acyclic by
construction, so there is nothing left to sever.

`internal/codegen/pipeline/clusters.go` computes them (Tarjan, iterative — the graph is
small but recursion depth is not something to bet on). The representative is the
shortest member name, which gives `Microsoft.UI.Xaml` rather than an invented one, so
the package is at the path a reader would guess. `typemap.SamePackage` is then what
makes a cross-namespace reference need no import at all.

Three invariants, all asserted in `pipeline`:

- the fourteen members are written out **in full** in the test, so a servicing release
  that changes the recursion is a reviewed diff — a namespace joining or leaving the
  cluster *moves types between Go packages*;
- the other 63 namespaces each keep their own package, so clustering cannot quietly
  collapse things that had no cycle;
- `ComputeBlockedImports` finds **nothing to sever** afterwards. If that ever fails, an
  edge is missing from the component computation — a bug in `localReferenceGraph`, not a
  reason to start cutting again.

External namespaces are never clustered. A reference into go-bindings-winrt cannot close
a cycle here, because nothing in that module imports this one, and merging one of its
namespaces into a local package would be meaningless — this module cannot add files to it.

The cycle breaker is kept rather than deleted. It now operates on packages, and it is
the assertion that clustering worked.

### The import alias collision

After the root prefix is stripped, this repository's `Microsoft.UI.Xaml.Interop`
and go-bindings-winrt's `Windows.UI.Xaml.Interop` both become the alias
`uixamlinterop`. The same is true of `UI.Xaml.Data`, `UI.Xaml.Markup`, `UI.Text`
and `UI` itself. WinUI 3 is a fork of the UWP XAML framework, so the parallel
naming is not a coincidence and will not go away.

External aliases therefore take a `wrt` prefix: `wrtuixamlinterop`. A prefix
rather than a suffix so the alias reads as "the WinRT one", and three letters
because no local namespace can produce it — that would need a `Microsoft.Wrt*`
namespace to exist.

`generate resolve` hard-errors if two namespaces in one package land on the same
alias, or one import path appears under two aliases, and CI runs it. Left
unchecked this is a compile failure across roughly thirty packages on the first
full run, naming neither the alias nor the namespaces that collided.

### Conformant arrays

A WinRT array crosses the ABI as two words, a count and a data pointer, and carries
**no length in metadata** — the count is synthesized from `len(slice)`. Confirmed by
scanning the committed winmds for `LengthIs`/`NativeArrayInfo`: zero hits, and
go-winmd exposes no length for `SZARRAY` because there is none to expose.

Which two words depends on who allocates the buffer, and that is where the care is:

| Shape | ABI | Go |
|---|---|---|
| `[in] T[]` (pass) | `(UINT32 size, T* value)` | `M(items []T) error` |
| `[out] T[]` (fill) | `(UINT32 size, T* value)` | `M(items []T) (uint32, error)` |
| `[out] T[]&` (receive) | `(UINT32* size, T** value)` | refused, see below |
| `T[]` returned | `(UINT32* size, T** value)` | `M() ([]T, error)` |

Pass and fill lower **identically** — the difference is only who writes, which the
doc comment records. So there are two lowerings, not four.

The metadata separates receive from fill by `ELEMENT_TYPE_BYREF` alone, which
`typeRefOf` collapses — hence `Param.ByRef` (schema version 3). There is exactly
**one** byref array parameter in the whole Windows App SDK
(`ITextRangeProvider.GetBoundingRectangles`), and that is a reason to record the bit
rather than ignore it: passing a count where the callee writes through a pointer
corrupts memory silently, and one such member is the worst possible outcome. It is
refused with its own diagnostic rather than given a one-off promote-out-param-to-return
path.

Elements are admitted as a DIRECT VIEW only when the Go representation is byte-identical
to the ABI form: scalars, floats, enums, GUIDs, emittable structs, and interface, class
and Object pointers — 92% of the arrays in the metadata.

`HSTRING` elements are converted instead, one at a time, over a parallel
`[]syswinrt.HSTRING` built beside the caller's slice. Who owns the handles is the whole
of it, and it differs per shape:

| Shape | Who creates them | The body |
|---|---|---|
| `[in]` | this side | `NewHString` per element, `defer Close` each |
| `[out]` fill | the callee | `TakeHString` each into the caller's slice |
| returned | the callee, buffer included | `TakeHString` each, then `CoTaskMemFree` |

Slots the callee never wrote stay null, and `TakeHString` of a null handle yields `""`
and deletes nothing — which is what lets the fill conversion run across the whole slice
without first knowing how many elements were written.

`Bool` elements are still refused: one byte with no guarantee it is 0 or 1, so a direct
view produces Go bools that are neither true nor false in comparisons. It could be given
the same per-element treatment and is not, because no array of `Bool` occurs anywhere in
the metadata — an unexercised conversion is a worse bet than a refusal.

A returned array is the one place generated code owns native memory: the callee
allocated it with `CoTaskMemAlloc`, so the body copies out and calls `CoTaskMemFree`,
after the HRESULT check — freeing a buffer that was never allocated is the obvious way
to get this wrong.

### HSTRING and Bool in value positions

Both are shapes where the Go form is not the ABI form, and where the conversion goes
depends on whether there is a boundary to put it at.

**A parameter or a return has one.** An `[out] HSTRING*` transfers ownership to the
caller, so the body declares a raw slot, dispatches, and calls `winrt.TakeHString` —
which takes the handle and deletes it. `view.MethodModel.Postamble` carries those
statements, and the template runs them **only after the HRESULT check passes**: a failed
call wrote nothing, so converting the slot anyway would report an error and simultaneously
wipe the caller's variable. `Bool` out-parameters go through a `byte`, because a WinRT
boolean is one byte with no guarantee it is 0 or 1.

Exposing the raw handle was the alternative and it is a worse API: an out `HSTRING` must
be deleted, so a caller who merely reads it leaks, and nothing in the signature says so.

A float out-parameter was refused by plain omission — `KindFloat` was missing from the
admitted kinds. An `[out]` parameter is a pointer the callee writes through, so a
`float32` crosses through memory, never XMM0; only a by-value float parameter needs its
bit pattern taken. `ICompositionPropertySet.TryGetScalar` was the cost, while every
`TryGetVector3` beside it worked, which is what made it visible.

**A struct field has no boundary.** The struct crosses as a block of bytes, so the field
IS the handle: `syswinrt.HSTRING`, via `typemap.StructFieldGoType`, with a generated doc
note stating the ownership rule. A shadow struct per type with marshalling at every
signature would read better and would cost the property conformant arrays depend on —
that a `[]T` over emittable structs is a direct view of the callee's buffer. Two structs
in this metadata have string fields, so the trade was not close.

### What go-bindings-winrt emitted is not what its metadata says

`StructEmittable` returned true for every external type, on the reasoning that this
module never emits them so they are always representable. That is the wrong evidence.
The metadata says what a type IS; only the dependency's emitted source says whether there
is a Go declaration to name.

`Windows.UI.Xaml.Interop.TypeName` is `{ HSTRING Name; TypeKind Kind; }`, and
go-bindings-winrt v0.4.0 skips it — `struct-field-skipped: has unrepresentable fields`
in its own baseline — while its sibling `TypeKind`, an enum, is emitted. So the moment
this module stopped refusing HSTRING struct fields, 31 members began naming
`wrtuixamlinterop.TypeName` and the whole tree stopped compiling. The assumption had held
only because nothing referenced up to that point had been skipped over there.

`typemap/declared.go` scans the dependency's emitted `.go` files for package-level type
declarations, lazily and memoized per namespace. It is a coupling to that module's output
layout, and the same coupling `ImportPathFor` already has when it builds an import path
from a namespace — so it is consistent rather than new, and it answers the question by
reading the artefact that decides it. Conservative on any read failure: a false degrades
a member, while a wrong true breaks every consumer's build.

go-bindings-winrt v0.4.1 emits `TypeName` — using the same representation, the handle as
the field, because that module has the same absence of a boundary — and the pin here
moved with it, so those 31 members are back. The check stays: it is what turns "the
dependency does not provide this" from a build failure into a diagnostic, and
`Windows.Web.Http.HttpProgress` is still skipped over there, so it keeps a live example.

### The base-class chain

`Microsoft.UI.Xaml.Controls.IButton` carries **only** `get_Flyout`/`put_Flyout`.
`Click` is on `Primitives.IButtonBase` at slot 14; `Content` is on
`IContentControl`. And XAML's `[ExclusiveTo]` interfaces carry **no `Requires` at
all**, so the interface hierarchy offers no route either — walking `Extends` is the
whole mechanism.

`Class.BaseClass` records it (schema version 2), and the class emitter walks the
chain to project each base's interfaces as `As<Interface>()` accessors on the
derived class. `Button` gains fifteen, reaching `ButtonBase`, `ContentControl`,
`Control`, `FrameworkElement`, `UIElement` and `DependencyObject`; each records which
base it came from.

**The import graph has to model what is emitted.** Projecting a base's interfaces
onto a derived class creates import edges that appear nowhere in the derived
namespace's own `TypeRef`s. The first version left the graph ignorant of them, so
`Controls` and `Peers` reached each other through inheritance alone and the output did
not compile. `localReferenceGraph` and the emitter now share `Registry.WalkBaseChain`,
so the graph clustering is computed from is the graph the emitter builds.

Two dead ends, recorded so they are not retried:

**Weighting inherited edges higher makes severing worse.** It was tried, on the
reasoning that losing one costs a whole accessor rather than one signature. Raising
those weights only moves the cut onto other edges in the same cycles, and those carry
more plain references: 870 degradations became 1040, without recovering the edge it was
aimed at.

**Severing was the wrong answer to the question.** Both of the above are about picking a
cut well. [Namespace clusters](#namespace-clusters) removes the need to cut at all —
`Controls` ↔ `Controls.Primitives` is genuinely mutual and dense both ways, and the
conclusion to draw from that is that they belong in one Go package, not that one
direction has to be lost.

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

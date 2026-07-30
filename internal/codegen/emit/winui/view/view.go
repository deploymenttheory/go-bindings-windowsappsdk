// Package view is the pure-data IR the render templates consume. It imports no
// metadata or typemap package — every field is an already-resolved fragment, so a
// template only branches and interpolates and never decides anything.
//
// That separation is the point. Emission has three stages: gather (which resolves
// everything through the typemap), view (this), and render (templates). A template
// that could reach the metadata would be a fourth place type decisions live, and
// the hardest of the four to test.
package view

// EnumModel is one named enum type with its members.
type EnumModel struct {
	TypeName string
	// FullName is the metadata name ("Microsoft.UI.Xaml.Visibility") for the doc
	// comment.
	FullName string
	BaseType string
	IsFlags  bool
	Members  []EnumMemberModel
	// UniqueMembers is Members deduped by value, first name winning — the switch
	// cases of String(). WinRT enums routinely give two names the same value, and
	// duplicate case values would not compile.
	UniqueMembers []EnumMemberModel
}

// EnumMemberModel is one enum constant.
type EnumMemberModel struct {
	Name  string
	Value string
}

// StructModel is one WinRT value struct.
type StructModel struct {
	TypeName string
	FullName string
	Fields   []StructFieldModel
	// NoteLines are doc-comment lines after the summary. A struct carrying an HSTRING
	// field needs them: the field is a handle the caller owns, which the type alone
	// does not say.
	NoteLines []string
}

// StructFieldModel is one struct field, fully resolved.
type StructFieldModel struct {
	Name   string
	GoType string
}

// InterfaceModel is one WinRT interface: a single vtable-pointer struct rooted at
// IInspectable, dispatching through absolute slots.
type InterfaceModel struct {
	TypeName string
	FullName string
	// GUID is the IID string for the doc comment ("" when absent).
	GUID string
	// ExclusiveTo names the owning runtime class ("" for shared interfaces).
	// Nearly every XAML interface is exclusive to its class.
	ExclusiveTo string
	// Requires lists required interfaces as display names, for the doc comment
	// only: WinRT interfaces never embed at the ABI, they are reached by
	// QueryInterface.
	Requires []string
	// IIDVar/IIDLiteral declare the IID variable (skipped when GUID is "").
	IIDVar     string
	IIDLiteral string
	// Methods holds emitted methods and skipped-slot comments interleaved in
	// absolute slot order, so the vtable layout stays auditable in the output.
	Methods []MethodModel
}

// Return-shape discriminants for MethodModel.ReturnKind. The template branches on
// these and nothing else.
const (
	// RetError: no logical return — `return win32.ErrIfFailed(int32(r1))`.
	RetError = 0
	// RetValue: `return <ResultExpr>, win32.ErrIfFailed(int32(r1))`.
	RetValue = 1
	// RetString: an HSTRING retval, where ErrIfFailed has to short-circuit
	// BEFORE TakeHString consumes the handle — on failure there is no handle to
	// consume, and taking one would be a use of uninitialized memory.
	RetString = 2
	// RetArray: a conformant-array retval. Two out-words rather than one (a count
	// pointer and a buffer pointer), and the buffer is callee-allocated, so the
	// body copies into a Go slice and frees it. Short-circuits like RetString,
	// for the same reason: on failure there is no buffer to read or free.
	RetArray = 3
)

// MethodModel is one vtable method, or — when SkipComment is set — a skipped-slot
// marker that keeps the vtable layout auditable.
type MethodModel struct {
	// SkipComment renders a standalone `// slot N: name skipped: reason`
	// comment; every other field is unused when it is non-empty.
	SkipComment string

	CommentLines []string
	GoName       string
	ParamStr     string
	// ReturnSig is the complete return signature ("error", "(string, error)",
	// "(Thickness, error)").
	ReturnSig string
	// Slot is the absolute vtable index (6 + MethodDef index).
	Slot int
	// Preamble holds statements converting idiomatic parameters into raw syscall
	// words (HSTRING inputs, bool → 0/1, float bit patterns) before dispatch.
	Preamble []string
	// ArgExprs are the SyscallN argument words after the self word, including
	// the trailing retval out-pointer when the method has one.
	ArgExprs []string
	// Postamble holds statements converting raw out-parameter slots back into the
	// idiomatic values the caller's pointers expect (HSTRING → string, byte → bool).
	// It runs ONLY after the HRESULT check succeeds: on a failure the callee wrote
	// nothing, and a caller's variable must be left as it was rather than zeroed by a
	// conversion of an unwritten slot.
	Postamble []string
	// ReturnKind selects the body shape (Ret* above).
	ReturnKind int
	// ResultDecl declares the retval local ("result := new(int32)"),
	// heap-allocated because the native side writes through it. See buildMethod
	// for why a stack address will not do.
	ResultDecl string
	// ResultExpr converts the retval local to the Go return value ("*result",
	// "*result != 0", "winrt.TakeHString(*result)").
	ResultExpr string
	// ZeroReturn is the zero value returned alongside a non-nil error in
	// preamble and RetString short-circuits (`""`, "0", "nil", "Thickness{}").
	ZeroReturn string
	// ResultSizeDecl declares the count local of a RetArray return
	// ("resultSize := new(uint32)"). Empty for every other return kind.
	ResultSizeDecl string
	// ResultElemType is a RetArray's element Go type ("byte", "*IFoo"), which the
	// body needs to build the slice view over the callee's buffer.
	ResultElemType string
}

// DelegateModel is one Go-implemented handler type emitted into the consuming
// package: a typed wrapper over the runtime's Delegate, with a constructor whose
// adapter converts the raw Invoke ABI words into typed callback arguments.
//
// Handlers are grounded per consuming package rather than emitted once in their
// home namespace. Two packages using the same delegate each get their own copy:
// distinct Go types, identical ABI, same IID — which is what makes a
// TypedEventHandler`2 closed over a local args class expressible at all.
type DelegateModel struct {
	TypeName string
	// FullName is the display name for the doc comment
	// ("Windows.Foundation.TypedEventHandler`2<Object, Microsoft.UI.Xaml.RoutedEventArgs>").
	FullName string
	// GUID is the delegate IID: declared for a plain delegate, derived per the
	// pinterface rules for a generic instantiation.
	GUID string
	// IIDVar/IIDLiteral declare the IID variable.
	IIDVar     string
	IIDLiteral string
	// CtorName is the typed constructor ("New" + TypeName).
	CtorName string
	// CtorCommentLines document the constructor, including the borrowed-reference
	// contract when pointer or string arguments apply.
	CtorCommentLines []string
	// FnParams is the callback signature's parameter list
	// ("sender *syswinrt.IInspectable, e *RoutedEventArgs").
	FnParams string
	// ParamCount is the number of raw ABI words Invoke receives.
	ParamCount int
	// ArgExprs convert the adapter's raw words into the typed callback
	// arguments, in parameter order.
	ArgExprs []string
}

// ClassModel is one runtime class: a struct embedding its default interface by
// value, plus the package-level statics accessors and constructors surfaced from
// the class's activation factory.
//
// A statics-only class emits with TypeName "" — no class type at all, only the
// accessors, because there is no instance to be had.
type ClassModel struct {
	// TypeName is the emitted class type; "" when the class type itself is not
	// emitted and only Statics render.
	TypeName string
	FullName string
	// DefaultInterface is the embedded default interface's Go type name, possibly
	// package-qualified.
	DefaultInterface string
	// DefaultIIDRef is the address expression of the default interface's IID
	// variable ("&IID_IButton").
	DefaultIIDRef string
	// CtorName is the direct-activation constructor ("NewButton"); "" when the
	// class is not directly activatable.
	CtorName string
	// QueryMethods project the class's other instance interfaces through
	// QueryInterface.
	QueryMethods []QueryMethodModel
	// Statics are the package-level accessors for the class's [Static]
	// interfaces.
	Statics []StaticsAccessorModel
	// Factories are the package-level constructors projected from the class's
	// [Activatable] factory interfaces.
	Factories []FactoryFuncModel
	// ComposableCtors are the package-level null-outer constructors projected
	// from the class's [Composable] factory interfaces. Nearly every XAML class
	// is composable, so for most of the tree this is how an instance is made at
	// all.
	ComposableCtors []ComposableCtorModel
}

// StaticsAccessorModel is one package-level statics accessor: a function
// returning the class's statics interface, fetched through its activation factory.
// GetActivationFactory already queries for the statics IID, so the pointer it
// returns IS the statics interface.
type StaticsAccessorModel struct {
	// FuncName is the accessor — the statics interface name with its I prefix
	// stripped ("ButtonStatics").
	FuncName string
	// InterfaceType is the statics interface's Go type name, possibly
	// package-qualified.
	InterfaceType string
	// InterfaceFullName is its full metadata name, for the doc comment.
	InterfaceFullName string
	// IIDRef is the address expression of its IID variable.
	IIDRef string
	// ClassFullName is the runtime class's activation name.
	ClassFullName string
}

// FactoryFuncModel is one package-level factory constructor: fetch the class's
// activation factory, delegate to the generated factory-interface method, and
// wrap the returned default-interface pointer as the class type.
type FactoryFuncModel struct {
	// FuncName is the constructor — the factory method's projected name.
	FuncName string
	// FactoryType is the factory interface's Go type name.
	FactoryType string
	// FactoryFullName is its full metadata name, for the doc comment.
	FactoryFullName string
	// FactoryIIDRef is the address expression of its IID variable.
	FactoryIIDRef string
	// MethodName is the generated factory-interface method delegated to.
	MethodName string
	// ParamStr is the parameter list, identical to the factory method's — already
	// lowered by the interface emission, so the two cannot disagree.
	ParamStr string
	// ArgNames pass the parameters through, in order.
	ArgNames []string
}

// ComposableCtorModel is one package-level composable constructor: fetch the
// activation factory, call the generated composable factory method with a NULL
// controlling outer, release the returned inner, and wrap the returned
// default-interface pointer as the class type.
//
// The null outer means the class is created as itself rather than derived from.
// Deriving would need COM aggregation support that does not exist yet, and under
// null-outer composition the inner the factory hands back is a second reference to
// the same object — so it is released rather than kept.
type ComposableCtorModel struct {
	// FuncName is the constructor ("NewButton" for CreateInstance).
	FuncName string
	// FactoryType is the composable factory interface's Go type name.
	FactoryType string
	// FactoryFullName is its full metadata name, for the doc comment.
	FactoryFullName string
	// FactoryIIDRef is the address expression of its IID variable.
	FactoryIIDRef string
	// MethodName is the generated factory-interface method delegated to.
	MethodName string
	// ParamStr is the LEADING parameter list: the factory method's parameters
	// minus the trailing (baseInterface, innerInterface) pair the constructor
	// supplies itself.
	ParamStr string
	// InnerName is the local holding the inner out-pointer, freshened against
	// the leading parameter names.
	InnerName string
	// ArgNames pass the leading parameters through, followed by the composition
	// pair ("nil", InnerName).
	ArgNames []string
}

// QueryMethodModel is one As<Interface> query method on a runtime class.
type QueryMethodModel struct {
	// GoName is "As" plus the interface name with its I prefix stripped
	// ("AsButtonBase").
	GoName string
	// InterfaceType is the target interface's Go type name, possibly
	// package-qualified.
	InterfaceType string
	// IIDRef is the address expression of its IID variable.
	IIDRef string
	// Note is an optional extra doc line (used to say where an inherited
	// interface came from).
	Note string
}

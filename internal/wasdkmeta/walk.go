package wasdkmeta

// WalkRefs visits every TypeRef in a namespace: struct fields, interface
// requires/methods/properties/events, class interfaces including the default,
// and delegate Invoke signatures — recursing through generic arguments and
// array element types.
//
// It lives on the model rather than in the codegen pipeline because more than
// one consumer needs the same traversal, and each having its own would mean each
// forgetting a different corner of the IR. Map iteration order is not
// stabilized: every caller so far either counts or checks, and imposing a sort
// on the hot path of a whole-tree walk would cost more than it is worth. A
// caller that needs determinism should sort its own output.
func WalkRefs(meta *NamespaceMeta, visit func(*TypeRef)) {
	var walkRef func(*TypeRef)
	walkRef = func(ref *TypeRef) {
		if ref == nil {
			return
		}
		visit(ref)
		for i := range ref.Args {
			walkRef(&ref.Args[i])
		}
		walkRef(ref.Elem)
	}
	walkMethod := func(method *Method) {
		for i := range method.Params {
			walkRef(&method.Params[i].Type)
		}
		walkRef(method.Return)
	}

	for name := range meta.Structs {
		definition := meta.Structs[name]
		for i := range definition.Fields {
			walkRef(&definition.Fields[i].Type)
		}
	}
	for name := range meta.Interfaces {
		definition := meta.Interfaces[name]
		for i := range definition.Requires {
			walkRef(&definition.Requires[i])
		}
		for i := range definition.Methods {
			walkMethod(&definition.Methods[i])
		}
		for i := range definition.Properties {
			walkRef(&definition.Properties[i].Type)
		}
		for i := range definition.Events {
			walkRef(&definition.Events[i].Type)
		}
	}
	for name := range meta.Classes {
		definition := meta.Classes[name]
		walkRef(definition.DefaultInterface)
		walkRef(definition.BaseClass)
		for i := range definition.Interfaces {
			walkRef(&definition.Interfaces[i])
		}
	}
	for name := range meta.Delegates {
		definition := meta.Delegates[name]
		walkMethod(&definition.Invoke)
	}
}

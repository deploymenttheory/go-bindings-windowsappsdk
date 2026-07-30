package emitwinui

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/naming"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/typemap"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/wasdkmeta"
)

// buildEnumModels converts a namespace's enums into named integral types with
// their constants.
//
// Member names are prefixed with the type name. WinRT enums are scoped and Go's
// constants are not, so Visibility.Visible and other enums' Visible would
// otherwise collide at package level — and across a namespace the size of
// Microsoft.UI.Xaml.Controls they certainly would.
func (g *Generator) buildEnumModels(meta *wasdkmeta.NamespaceMeta) []view.EnumModel {
	models := make([]view.EnumModel, 0, len(meta.Enums))
	for _, name := range sortedKeys(meta.Enums) {
		definition := meta.Enums[name]
		goName := naming.Export(name)
		if !g.claimTypeName(goName) {
			g.diag("name-collision-skipped", "enum %s.%s", meta.Namespace, name)
			continue
		}
		model := view.EnumModel{
			TypeName: goName,
			FullName: meta.Namespace + "." + name,
			BaseType: definition.BaseType,
			IsFlags:  definition.IsFlags,
		}
		seenValues := map[string]bool{}
		for i := range definition.Members {
			member := &definition.Members[i]
			memberName := goName + naming.Export(member.Name)
			if !g.claimName(memberName) {
				g.diag("name-collision-skipped", "enum member %s.%s.%s", meta.Namespace, name, member.Name)
				continue
			}
			model.Members = append(model.Members, view.EnumMemberModel{Name: memberName, Value: member.Value})
			// String() switches on the value, so a repeated value has to appear
			// once: WinRT enums routinely alias two names to one number, and
			// duplicate case values do not compile.
			if !seenValues[member.Value] {
				seenValues[member.Value] = true
				model.UniqueMembers = append(model.UniqueMembers, view.EnumMemberModel{Name: memberName, Value: member.Value})
			}
		}
		models = append(models, model)
	}
	return models
}

// buildStructModels converts a namespace's value structs into Go structs.
//
// A struct is emitted only when every field resolves to a representable value
// shape. Emitting one with a field it could not represent would produce a type
// whose Go layout differs from the ABI's, and every call passing it would corrupt
// its argument — so the struct is skipped and references to it degrade.
func (g *Generator) buildStructModels(meta *wasdkmeta.NamespaceMeta, imports typemap.ImportSet) []view.StructModel {
	context := typemap.Context{Namespace: meta.Namespace}
	models := make([]view.StructModel, 0, len(meta.Structs))
	for _, name := range sortedKeys(meta.Structs) {
		definition := meta.Structs[name]
		fullName := meta.Namespace + "." + name

		if typemap.IsExternalType(meta.Namespace, name) {
			// Already provided by the shared ABI foundation; a second definition
			// would fork an identity every signature depends on.
			g.diag("external-type-not-emitted", "%s", fullName)
			continue
		}
		if !g.mapper.StructEmittable(meta.Namespace, name) {
			g.diag("struct-skipped", "%s has a field that cannot be represented", fullName)
			continue
		}
		goName := naming.Export(name)
		if !g.claimTypeName(goName) {
			g.diag("name-collision-skipped", "struct %s", fullName)
			continue
		}

		model := view.StructModel{TypeName: goName, FullName: fullName}
		for i := range definition.Fields {
			field := &definition.Fields[i]
			resolved := g.mapper.GoType(&field.Type, context, imports)
			if resolved.Kind == typemap.KindUnsupported {
				// StructEmittable already said every field resolves, so reaching
				// here means the two disagree — a generator bug, not a metadata
				// one, and worth saying so rather than emitting a partial struct.
				g.diag("struct-field-unresolved", "%s.%s: %s", fullName, field.Name, resolved.Reason)
				model.Fields = nil
				break
			}
			model.Fields = append(model.Fields, view.StructFieldModel{
				Name:   naming.Export(field.Name),
				GoType: resolved.GoType,
			})
		}
		if len(model.Fields) == 0 && len(definition.Fields) > 0 {
			continue
		}
		models = append(models, model)
	}
	return models
}

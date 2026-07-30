// Package render turns view models into Go source fragments through text/template
// files only. It makes no resolution decisions and imports no metadata or
// type-mapping package — every value it needs is already present on the view.
//
// That is the render firewall. It is load-bearing rather than stylistic: a
// template that could reach the metadata would become a second place type
// decisions live, and templates are the hardest place to test one.
package render

import (
	"embed"
	"strings"
	"text/template"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/internal/codegen/emit/winui/view"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var templates = template.Must(template.New("winui").Funcs(template.FuncMap{
	"join": strings.Join,
}).ParseFS(templateFS, "templates/*.tmpl"))

func execute(name string, data any) (string, error) {
	var builder strings.Builder
	if err := templates.ExecuteTemplate(&builder, name, data); err != nil {
		return "", err
	}
	return builder.String(), nil
}

// Enum renders one enum type block.
func Enum(model view.EnumModel) (string, error) { return execute("enum", model) }

// Struct renders one value-struct block.
func Struct(model view.StructModel) (string, error) { return execute("struct", model) }

// Interface renders one WinRT interface: the type, its IID, and its vtable
// methods.
func Interface(model view.InterfaceModel) (string, error) { return execute("interface", model) }

// Class renders one runtime class: the type, its constructors, its query methods,
// and its statics accessors.
func Class(model view.ClassModel) (string, error) { return execute("class", model) }

// Delegate renders one Go-implemented handler: the type, its IID, the typed
// constructor with its raw-word adapter, Ptr, and Close.
func Delegate(model view.DelegateModel) (string, error) { return execute("delegate", model) }

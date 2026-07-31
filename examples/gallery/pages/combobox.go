//go:build windows && amd64

package pages

// Ported from controls/dev/ComboBox/TestUI/ComboBoxPage.xaml.
//
// The first page that needs a DataTemplate, and so the first that uses app.LoadMarkup.
// A DataTemplate is a factory the framework runs once per item; there is no object graph
// to build in Go because the graph does not exist until an item needs one. Markup is the
// honest representation, not a shortcut.
//
// Items themselves ARE built in Go: ItemsControl.Items is an IObservableVector<Object>,
// reached through the IVector it requires, holding boxed values.

import (
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "ComboBox", Name: "ComboBoxPage", Build: buildComboBoxPage})
}

func buildComboBoxPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	plain, err := newComboBox("Pick one", []string{"Alpha", "Bravo", "Charlie", "Delta"})
	if err != nil {
		return nil, err
	}
	editable, err := newComboBox("Type or pick", []string{"Echo", "Foxtrot", "Golf"})
	if err != nil {
		return nil, err
	}
	if err := editable.SetIsEditable(true); err != nil {
		return nil, err
	}

	// The templated one. The template names no type of ours — it is markup the
	// framework instantiates per item — so it is loaded rather than built.
	templated, err := newComboBox("Templated", []string{"One", "Two", "Three"})
	if err != nil {
		return nil, err
	}
	template, err := app.LoadMarkup[uixaml.IDataTemplate](
		app.Markup(`<DataTemplate><TextBlock Text="{Binding}" FontStyle="Italic"/></DataTemplate>`),
		&uixaml.IID_IDataTemplate)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	if err := app.With(templated.AsItemsControl, func(items *uixaml.IItemsControl) error {
		return items.SetItemTemplate(template)
	}); err != nil {
		return nil, err
	}

	captions := []string{"Default", "Editable", "With an item template"}
	boxes := []*uixaml.ComboBox{plain, editable, templated}
	var children []func() (*uixaml.IUIElement, error)
	for i, box := range boxes {
		caption, err := label(captions[i])
		if err != nil {
			return nil, err
		}
		children = append(children, caption.AsUIElement, box.AsUIElement)
	}

	panel, err := stack(8, children...)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

// newComboBox builds a ComboBox with placeholder text and boxed string items.
func newComboBox(placeholder string, items []string) (*uixaml.ComboBox, error) {
	box, err := uixaml.NewComboBox()
	if err != nil {
		return nil, err
	}
	if err := box.SetPlaceholderText(placeholder); err != nil {
		return nil, err
	}
	if err := app.With(box.AsItemsControl, func(control *uixaml.IItemsControl) error {
		observable, err := control.Items()
		if err != nil {
			return err
		}
		defer observable.Release()
		// Items is an IObservableVector, which carries only its VectorChanged event;
		// adding goes through the IVector it requires.
		collection, err := observable.AsVectorOfObject()
		if err != nil {
			return err
		}
		defer collection.Release()
		for _, item := range items {
			boxed, err := app.Box(item)
			if err != nil {
				return err
			}
			err = collection.Append(boxed)
			boxed.Release()
			if err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return box, nil
}

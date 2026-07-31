//go:build windows && amd64

package pages

// The TreeView pages.
//
// Sources: controls/dev/TreeView/TestUI/*Page.xaml
//
// TreeView can be driven two ways and the six source pages split along that line.
//
// The NODE model is TreeView's own: a tree of TreeViewNode objects, each holding
// Content, built and mutated directly. The ITEMS SOURCE model binds an arbitrary
// hierarchy through ItemsSource and an ItemTemplate, and the control builds nodes behind
// the scenes. They are not interchangeable — RootNodes is empty under the second — which
// is what several of these pages exist to pin down.

import (
	"fmt"

	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

func init() {
	register(Page{Control: "TreeView", Name: "TreeViewPage", Build: buildTreeViewPage})
	register(Page{Control: "TreeView", Name: "TreeViewNodeInMarkupTestPage", Build: buildTreeViewNodeInMarkupTestPage})
	register(Page{Control: "TreeView", Name: "TreeViewItemsSourceTestPage", Build: buildTreeViewItemsSourceTestPage})
	register(Page{Control: "TreeView", Name: "TreeViewLateDataInitTestPage", Build: buildTreeViewLateDataInitTestPage})
	register(Page{Control: "TreeView", Name: "TreeViewUnrealizedChildrenTestPage", Build: buildTreeViewUnrealizedChildrenTestPage})
	register(Page{Control: "TreeView", Name: "TreeViewItemTemplateSelectorTestPage", Build: buildTreeViewItemTemplateSelectorTestPage})
}

// newNode builds a TreeViewNode holding a string.
//
// Content is IInspectable, so the string is boxed — and the box is released here because
// SetContent takes its own reference, which is the ordinary WinRT property rule and
// worth doing rather than leaking one box per node.
func newNode(content string) (*uixaml.TreeViewNode, error) {
	node, err := uixaml.NewTreeViewNode()
	if err != nil {
		return nil, err
	}
	boxed, err := app.Box(content)
	if err != nil {
		node.Release()
		return nil, err
	}
	err = node.SetContent(boxed)
	boxed.Release()
	if err != nil {
		node.Release()
		return nil, err
	}
	return node, nil
}

// appendNode adds child to parent's Children, transferring nothing: the vector takes its
// own reference, so the caller still owns theirs.
func appendNode(parent *uixaml.TreeViewNode, child *uixaml.TreeViewNode) error {
	children, err := parent.Children()
	if err != nil {
		return err
	}
	defer children.Release()
	node, err := child.AsTreeViewNode()
	if err != nil {
		return err
	}
	defer node.Release()
	return children.Append(node)
}

// appendRoot is appendNode against the TreeView's own RootNodes.
func appendRoot(tree *uixaml.TreeView, child *uixaml.TreeViewNode) error {
	roots, err := tree.RootNodes()
	if err != nil {
		return err
	}
	defer roots.Release()
	node, err := child.AsTreeViewNode()
	if err != nil {
		return err
	}
	defer node.Release()
	return roots.Append(node)
}

// sampleTree builds a two-level tree of nodes and returns the TreeView holding it.
func sampleTree(depth, breadth int, expanded bool) (*uixaml.TreeView, error) {
	tree, err := uixaml.NewTreeView()
	if err != nil {
		return nil, err
	}
	for i := 1; i <= breadth; i++ {
		root, err := newNode(fmt.Sprintf("Root %d", i))
		if err != nil {
			return nil, err
		}
		if depth > 1 {
			for j := 1; j <= breadth; j++ {
				child, err := newNode(fmt.Sprintf("Root %d, child %d", i, j))
				if err != nil {
					root.Release()
					return nil, err
				}
				err = appendNode(root, child)
				child.Release()
				if err != nil {
					root.Release()
					return nil, err
				}
			}
		}
		if err := root.SetIsExpanded(expanded); err != nil {
			root.Release()
			return nil, err
		}
		err = appendRoot(tree, root)
		root.Release()
		if err != nil {
			return nil, err
		}
	}
	if err := app.With(tree.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(320), frame.SetWidth(420))
	}); err != nil {
		return nil, err
	}
	return tree, nil
}

// TreeViewPage: the node model, with the events the control raises as it expands.
func buildTreeViewPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tree, err := sampleTree(2, 4, true)
	if err != nil {
		return nil, err
	}
	if err := tree.SetSelectionMode(uixaml.TreeViewSelectionModeMultiple); err != nil {
		return nil, err
	}

	status, err := label("Expand and collapse a node, or select one.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(tree.AddExpanding, uixaml.NewTypedEventHandlerOfTreeViewAndTreeViewExpandingEventArgs,
		func(_ *uixaml.ITreeView, args *uixaml.ITreeViewExpandingEventArgs) {
			_ = status.SetText("Expanding: " + nodeText(args))
		}); err != nil {
		return nil, err
	}
	if _, err := app.On(tree.AddCollapsed, uixaml.NewTypedEventHandlerOfTreeViewAndTreeViewCollapsedEventArgs,
		func(_ *uixaml.ITreeView, _ *uixaml.ITreeViewCollapsedEventArgs) {
			_ = status.SetText("Collapsed a node.")
		}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, tree.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// nodeText reads the expanding node's content, which is the only interesting thing on
// the event args.
func nodeText(args *uixaml.ITreeViewExpandingEventArgs) string {
	node, err := args.Node()
	if err != nil {
		return "(node unavailable: " + err.Error() + ")"
	}
	defer node.Release()
	content, err := node.Content()
	if err != nil || content == nil {
		return "(no content)"
	}
	defer content.Release()
	text, err := app.Unbox[string](content)
	if err != nil {
		return "(content is not a string)"
	}
	return text
}

// TreeViewNodeInMarkupTestPage: the source declares its nodes in XAML.
//
// Declaring a TreeViewNode in markup and building one in Go produce the same object —
// there is no markup-only capability here, so the port is the tree built directly, which
// is what the markup compiles to anyway.
func buildTreeViewNodeInMarkupTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tree, err := sampleTree(2, 3, true)
	if err != nil {
		return nil, err
	}
	note, err := label("The source declares these nodes in XAML. A TreeViewNode built in " +
		"Go is the same object the markup would produce.")
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, tree.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TreeViewItemsSourceTestPage: the other model, and the difference it makes.
//
// Under ItemsSource the control builds its own nodes, so RootNodes stays EMPTY — which
// is the thing worth showing, because a caller who mixes the two models gets a tree that
// renders and a RootNodes that says nothing is there.
func buildTreeViewItemsSourceTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tree, err := uixaml.NewTreeView()
	if err != nil {
		return nil, err
	}
	tree2, err := tree.AsTreeView2()
	if err != nil {
		return nil, err
	}
	defer tree2.Release()

	template, err := dataTemplate(`<DataTemplate><TreeViewItem Content="{Binding}"/></DataTemplate>`)
	if err != nil {
		return nil, err
	}
	defer template.Release()
	if err := tree2.SetItemTemplate(template); err != nil {
		return nil, err
	}

	source, err := itemsSource(numbered("Bound root", 6))
	if err != nil {
		return nil, err
	}
	if err := tree2.SetItemsSource(source.Inspectable()); err != nil {
		source.Close()
		return nil, err
	}
	if err := app.With(tree.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(280), frame.SetWidth(420))
	}); err != nil {
		return nil, err
	}

	roots, err := tree.RootNodes()
	if err != nil {
		return nil, err
	}
	defer roots.Release()
	size, err := roots.Size()
	if err != nil {
		return nil, err
	}
	status, err := label(fmt.Sprintf(
		"Bound through ItemsSource. RootNodes reports %d nodes — the control builds its "+
			"own behind the binding, so the node model sees nothing.", size))
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, tree.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TreeViewLateDataInitTestPage: the source binds an empty source and fills it after the
// control is loaded, checking the tree appears rather than staying blank.
func buildTreeViewLateDataInitTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tree, err := uixaml.NewTreeView()
	if err != nil {
		return nil, err
	}
	if err := app.With(tree.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(280), frame.SetWidth(420))
	}); err != nil {
		return nil, err
	}

	status, err := label("The tree starts empty. Fill it after it has loaded.")
	if err != nil {
		return nil, err
	}
	added := 0
	row, err := buttonRow(func() (*uixaml.Button, error) {
		return button("Add a root now", func() {
			added++
			node, err := newNode(fmt.Sprintf("Added after load %d", added))
			if err != nil {
				_ = status.SetText("Creating the node failed: " + err.Error())
				return
			}
			defer node.Release()
			for j := 1; j <= 2; j++ {
				child, err := newNode(fmt.Sprintf("Child %d", j))
				if err != nil {
					return
				}
				err = appendNode(node, child)
				child.Release()
				if err != nil {
					return
				}
			}
			if err := node.SetIsExpanded(true); err != nil {
				return
			}
			if err := appendRoot(tree, node); err != nil {
				_ = status.SetText("Appending the root failed: " + err.Error())
				return
			}
			_ = status.SetText(fmt.Sprintf("%d roots added after load.", added))
		})
	})
	if err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, row.AsUIElement, tree.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TreeViewUnrealizedChildrenTestPage: the lazy-expansion contract.
//
// HasUnrealizedChildren tells the control a node can be expanded even though its
// Children collection is empty. The Expanding event is then where the children are
// supplied — which is how a tree over something expensive (a filesystem, a network) is
// built without walking it up front.
func buildTreeViewUnrealizedChildrenTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tree, err := uixaml.NewTreeView()
	if err != nil {
		return nil, err
	}
	if err := app.With(tree.AsFrameworkElement, func(frame *uixaml.IFrameworkElement) error {
		return app.All(frame.SetHeight(300), frame.SetWidth(420))
	}); err != nil {
		return nil, err
	}

	for i := 1; i <= 4; i++ {
		root, err := newNode(fmt.Sprintf("Lazy root %d", i))
		if err != nil {
			return nil, err
		}
		// No children, but the control is told there are some.
		err = root.SetHasUnrealizedChildren(true)
		if err == nil {
			err = appendRoot(tree, root)
		}
		root.Release()
		if err != nil {
			return nil, err
		}
	}

	status, err := label("Expand a root: its children are created in the Expanding handler.")
	if err != nil {
		return nil, err
	}
	if _, err := app.On(tree.AddExpanding, uixaml.NewTypedEventHandlerOfTreeViewAndTreeViewExpandingEventArgs,
		func(_ *uixaml.ITreeView, args *uixaml.ITreeViewExpandingEventArgs) {
			node, err := args.Node()
			if err != nil {
				return
			}
			defer node.Release()

			unrealized, err := node.HasUnrealizedChildren()
			if err != nil || !unrealized {
				return // already filled; expanding again must not duplicate them
			}
			children, err := node.Children()
			if err != nil {
				return
			}
			defer children.Release()
			for j := 1; j <= 3; j++ {
				child, err := newNode(fmt.Sprintf("Realized child %d", j))
				if err != nil {
					return
				}
				inner, err := child.AsTreeViewNode()
				if err == nil {
					_ = children.Append(inner)
					inner.Release()
				}
				child.Release()
			}
			_ = node.SetHasUnrealizedChildren(false)
			_ = status.SetText("Realized three children on expand.")
		}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, tree.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// TreeViewItemTemplateSelectorTestPage: a different template per item.
//
// A DataTemplateSelector is a class the application DERIVES, overriding SelectTemplate.
// That needs composition with a Go-side override, which is a different mechanism from
// everything else in this batch — so what ports is the property being reachable and the
// single-template path it falls back to, with the gap stated rather than papered over.
func buildTreeViewItemTemplateSelectorTestPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	tree, err := sampleTree(2, 3, true)
	if err != nil {
		return nil, err
	}
	tree2, err := tree.AsTreeView2()
	if err != nil {
		return nil, err
	}
	defer tree2.Release()

	selector, err := tree2.ItemTemplateSelector()
	if err != nil {
		return nil, err
	}
	state := "none is set"
	if selector != nil {
		selector.Release()
		state = "one is set"
	}

	note, err := label(fmt.Sprintf(
		"ItemTemplateSelector is readable and %s.\n\n"+
			"The source supplies its own by deriving DataTemplateSelector and overriding "+
			"SelectTemplate. Deriving a XAML class in Go needs composition with a Go-side "+
			"override — the machinery exists (app.DerivedApplication uses it) but nothing "+
			"generates the override vtable for an arbitrary class yet, so this page shows "+
			"the property rather than a working selector.", state))
	if err != nil {
		return nil, err
	}
	panel, err := stack(8, note.AsUIElement, tree.AsUIElement)
	if err != nil {
		return nil, err
	}
	return scrolled(panel.AsUIElement)
}

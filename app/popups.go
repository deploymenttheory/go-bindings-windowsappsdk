//go:build windows && amd64

package app

// Closing popups that outlive the element that opened them.
//
// A popup is hosted in the XamlRoot's POPUP ROOT, not in the element tree of whatever
// opened it. That is what lets a flyout draw outside its parent's bounds, and it is also
// why dropping a subtree does not close what that subtree opened: the popup has a
// different parent and does not know its opener is gone.
//
// An ordinary application rarely notices, because it does not tear pages down and rebuild
// them in one window. Anything that DOES — a gallery, a navigation shell, a preview pane
// — accumulates them: one per visit, floating over whatever is shown next.
//
// Found in the gallery, where AnnotatedScrollBar's detail label ("At 0") stayed on screen
// over every page visited afterwards, and a fresh one appeared each time the page was
// opened again.

import (
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
)

// CloseOpenPopups closes every popup open in the window's XamlRoot.
//
// Call it when replacing a window's content, after dropping the outgoing tree:
//
//	content.SetContent(nil)
//	app.CloseOpenPopups(ready)
//
// Errors are not returned. Every failure here means "there was nothing to close, or it
// could not be reached", and neither is a reason for a caller to stop swapping pages —
// a stray popup is a worse thing to propagate an error over than to leave.
func CloseOpenPopups(ready *Ready) {
	popups, release := openPopups(ready)
	if popups == nil {
		return
	}
	defer release()

	size, err := popups.Size()
	if err != nil {
		return
	}
	for index := uint32(0); index < size; index++ {
		popup, err := popups.GetAt(index)
		if err != nil || popup == nil {
			continue
		}
		_ = popup.SetIsOpen(false)
		popup.Release()
	}
}

// OpenPopupCount reports how many popups are open in the window's XamlRoot.
//
// It exists for tests: a leaked popup is invisible to every other kind of check — nothing
// errors, nothing crashes, and the tree measures the same either way.
func OpenPopupCount(ready *Ready) uint32 {
	popups, release := openPopups(ready)
	if popups == nil {
		return 0
	}
	defer release()
	size, err := popups.Size()
	if err != nil {
		return 0
	}
	return size
}

// openPopups is the shared lookup: window -> content -> XamlRoot -> open popups.
func openPopups(ready *Ready) (*uixaml.IVectorViewOfPopup, func()) {
	if ready == nil || ready.Window == nil {
		return nil, nil
	}
	content, err := ready.Window.Content()
	if err != nil || content == nil {
		return nil, nil
	}
	defer content.Release()

	root, err := content.XamlRoot()
	if err != nil || root == nil {
		return nil, nil
	}
	defer root.Release()

	statics, err := uixaml.VisualTreeHelperStatics()
	if err != nil {
		return nil, nil
	}
	defer statics.Release()

	popups, err := statics.GetOpenPopupsForXamlRoot(root)
	if err != nil || popups == nil {
		return nil, nil
	}
	return popups, func() { popups.Release() }
}

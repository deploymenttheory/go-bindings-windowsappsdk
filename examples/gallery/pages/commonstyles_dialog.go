//go:build windows && amd64

package pages

// ContentDialogPage, and a correction to something the plan asserted.
//
// Source: controls/dev/CommonStyles/TestUI/ContentDialogPage.xaml
//
// The plan listed this page as blocked on a generated Await(), on the reasoning that
// ShowAsync returns an IAsyncOperation and go-bindings-winrt generates an Await for each
// one while this module does not. Writing the page shows that was wrong twice over.
//
// It is not blocked: SetCompleted plus GetResults is about ten lines and works today.
//
// More importantly, an Await would be the WRONG tool here even if it existed. Await
// BLOCKS — the sibling's generated one parks on a channel until the completion handler
// fires. ShowAsync is called on the UI thread, and the framework delivers that completion
// through the same thread's dispatcher, so blocking it means the completion can never
// arrive. The callback form is not a workaround for a missing Await; it is the correct
// shape for a thread-affine framework, and Await would deadlock.

import (
	"fmt"

	syswinrt "github.com/deploymenttheory/go-bindings-win32/bindings/win32/system/winrt"
	"github.com/deploymenttheory/go-bindings-windowsappsdk/app"
	uixaml "github.com/deploymenttheory/go-bindings-windowsappsdk/bindings/winui/ui/xaml"
	wrtfoundation "github.com/deploymenttheory/go-bindings-winrt/bindings/winrt/foundation"
)

func init() {
	register(Page{Control: "CommonStyles", Name: "ContentDialogPage", Build: buildContentDialogPage})
}

func buildContentDialogPage(ready *app.Ready) (*uixaml.IUIElement, error) {
	status, err := label("No dialog shown yet.")
	if err != nil {
		return nil, err
	}
	open, err := uixaml.NewButton()
	if err != nil {
		return nil, err
	}
	if err := app.SetContent(open.AsContentControl, "Show a ContentDialog"); err != nil {
		return nil, err
	}

	if err := app.With(open.AsButtonBase, func(base *uixaml.IButtonBase) error {
		_, addErr := app.On(base.AddClick, uixaml.NewRoutedEventHandler,
			func(_ *syswinrt.IInspectable, _ *uixaml.IRoutedEventArgs) {
				if err := showDialog(ready, status); err != nil {
					_ = status.SetText("Could not show the dialog: " + err.Error())
				}
			})
		return addErr
	}); err != nil {
		return nil, err
	}

	panel, err := stack(8, status.AsUIElement, open.AsUIElement)
	if err != nil {
		return nil, err
	}
	return panel.AsUIElement()
}

// showDialog builds a dialog, shows it, and reports which button closed it.
func showDialog(ready *app.Ready, status *uixaml.TextBlock) error {
	dialog, err := uixaml.NewContentDialog()
	if err != nil {
		return err
	}

	title, err := app.Box("A ContentDialog")
	if err != nil {
		return err
	}
	defer title.Release()
	body, err := app.Box("Primary, secondary and close, as the source page declares them.")
	if err != nil {
		return err
	}
	defer body.Release()

	if err := app.All(
		dialog.SetTitle(title),
		dialog.SetPrimaryButtonText("Primary"),
		dialog.SetSecondaryButtonText("Secondary"),
		dialog.SetCloseButtonText("Close"),
		dialog.SetDefaultButton(uixaml.ContentDialogButtonPrimary),
		app.With(dialog.AsContentControl, func(content *uixaml.IContentControl) error {
			return content.SetContent(body)
		}),
	); err != nil {
		return err
	}

	// A dialog needs the XamlRoot of the tree it will appear over. Without it, showing
	// fails rather than appearing somewhere unexpected.
	root, err := ready.Window.Content()
	if err != nil {
		return err
	}
	defer root.Release()
	xamlRoot, err := root.XamlRoot()
	if err != nil {
		return err
	}
	defer xamlRoot.Release()
	if err := app.With(dialog.AsUIElement, func(element *uixaml.IUIElement) error {
		return element.SetXamlRoot(xamlRoot)
	}); err != nil {
		return err
	}

	operation, err := dialog.ShowAsync()
	if err != nil {
		return err
	}
	// NOT awaited. The completion arrives on this same UI thread, so blocking here would
	// stop the message loop that has to deliver it.
	handler, err := uixaml.NewAsyncOperationCompletedHandlerOfContentDialogResult(
		func(completed *uixaml.IAsyncOperationOfContentDialogResult, asyncStatus wrtfoundation.AsyncStatus) {
			if asyncStatus != wrtfoundation.AsyncStatusCompleted {
				_ = status.SetText(fmt.Sprintf("Dialog did not complete: status %v", asyncStatus))
				return
			}
			result, err := completed.GetResults()
			if err != nil {
				_ = status.SetText("Could not read the result: " + err.Error())
				return
			}
			_ = status.SetText("Closed with: " + result.String())
		})
	if err != nil {
		operation.Release()
		return err
	}
	// Neither the handler nor the operation is released here: the framework holds both
	// until the dialog closes and the completion has run.
	if err := operation.SetCompleted(handler); err != nil {
		return err
	}
	// Reported so the open is observable. A ContentDialog is NOT enumerated by
	// VisualTreeHelper.GetOpenPopupsForXamlRoot — twelve dispatcher turns after
	// ShowAsync it still reports none — so a test cannot see the dialog by counting
	// popups, and neither can app.CloseOpenPopups reach one. What is observable is that
	// ShowAsync was accepted, which is what this says.
	return status.SetText("Dialog shown; waiting for a button.")
}

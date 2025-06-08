package gui

import (
	"fmt"
	"path/filepath"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/templates"
)

func BuildTemplateCheckerSection(a fyne.App, parentWindow fyne.Window) fyne.CanvasObject {
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("Enter URL to check templates")

	urlEntryContainer := container.NewMax(urlEntry)
	templateCheckLabel := widget.NewLabel("Template folder: (not selected)")
	templateResultsLabel := widget.NewLabel("")
	createTemplateBtn := widget.NewButton("Create new template", nil)
	createTemplateBtn.Disable()

	var checkTemplatesDir string

	selectTemplateCheckDirBtn := widget.NewButton("Select templates folder for checking", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			checkTemplatesDir = uri.Path()
			templateCheckLabel.SetText("Template folder: " + checkTemplatesDir)
		}, parentWindow)
	})

	checkTemplatesBtn := widget.NewButton("Check templates", func() {
		if checkTemplatesDir == "" {
			dialog.ShowInformation("Error", "Please select a templates folder", parentWindow)
			return
		}
		url := strings.TrimSpace(urlEntry.Text)
		if url == "" {
			dialog.ShowInformation("Error", "Please enter a URL", parentWindow)
			return
		}

		matched, err := templates.FindMatchingTemplates(url, checkTemplatesDir)
		if err != nil {
			dialog.ShowError(err, parentWindow)
			return
		}

		if len(matched) == 0 {
			templateResultsLabel.SetText("No matching templates found.\nYou can create a new template.")
			createTemplateBtn.Enable()
		} else {
			resultStr := "Matching templates:\n"
			for _, path := range matched {
				resultStr += filepath.Base(path) + "\n"
			}
			templateResultsLabel.SetText(resultStr)
			createTemplateBtn.Disable()
		}
	})

	createTemplateBtn.OnTapped = func() {
		url := strings.TrimSpace(urlEntry.Text)
		if url == "" {
			dialog.ShowInformation("Error", "Please enter a URL", parentWindow)
			return
		}
		tpl := templates.GenerateTemplate(url)
		if strings.HasPrefix(tpl, "# Failed") {
			dialog.ShowError(fmt.Errorf("template generation failed:\n%s", tpl), parentWindow)
			return
		}

		saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil || writer == nil {
				return
			}
			_, err = writer.Write([]byte(tpl))
			if err != nil {
				dialog.ShowError(err, parentWindow)
				return
			}
			writer.Close()
			dialog.ShowInformation("Success", "Template saved", parentWindow)
		}, parentWindow)
		saveDialog.SetFileName("template.yaml")
		saveDialog.SetFilter(storage.NewExtensionFileFilter([]string{".yaml", ".yml"}))
		saveDialog.Show()
	}

	section := container.NewVBox(
		widget.NewLabel("Template Checker Section"),
		urlEntryContainer,
		selectTemplateCheckDirBtn, templateCheckLabel,
		checkTemplatesBtn,
		templateResultsLabel,
		createTemplateBtn,
	)

	return section
}

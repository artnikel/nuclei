package gui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"gopkg.in/yaml.v3"

	"github.com/artnikel/nuclei/internal/templates"
)

func BuildTemplateCheckerSection(a fyne.App, parentWindow fyne.Window) fyne.CanvasObject {
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("Enter URL to check templates")

	templateCheckLabel := widget.NewLabel("Template folder: (not selected)")
	templateResultsLabel := widget.NewLabel("")
	createTemplateBtn := widget.NewButton("Create new template", nil)
	createTemplateBtn.Disable()

	var checkTemplatesDir string

	selectTemplateCheckDirBtn := widget.NewButton("Select templates folder for checking", func() {
		selectTemplatesFolder(parentWindow, &checkTemplatesDir, templateCheckLabel)
	})

	checkTemplatesBtn := widget.NewButton("Check templates", func() {
		checkTemplatesAction(parentWindow, urlEntry, checkTemplatesDir, templateResultsLabel, createTemplateBtn)
	})

	createTemplateBtn.OnTapped = func() {
		createTemplateAction(parentWindow, urlEntry)
	}

	section := container.NewVBox(
		widget.NewLabel("Template Checker Section"),
		container.NewStack(urlEntry),
		selectTemplateCheckDirBtn, templateCheckLabel,
		checkTemplatesBtn,
		templateResultsLabel,
		createTemplateBtn,
	)

	return section
}

func selectTemplatesFolder(parentWindow fyne.Window, dir *string, label *widget.Label) {
	dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}
		*dir = uri.Path()
		label.SetText("Template folder: " + *dir)
	}, parentWindow)
}

func checkTemplatesAction(parentWindow fyne.Window, urlEntry *widget.Entry, templatesDir string, resultsLabel *widget.Label, createBtn *widget.Button) {
	if templatesDir == "" {
		dialog.ShowInformation("Error", "Please select a templates folder", parentWindow)
		return
	}
	url := strings.TrimSpace(urlEntry.Text)
	if url == "" {
		dialog.ShowInformation("Error", "Please enter a URL", parentWindow)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	matched, err := templates.FindMatchingTemplates(ctx, url, templatesDir, 5*time.Second)
	if err != nil {
		dialog.ShowError(err, parentWindow)
		return
	}

	if len(matched) == 0 {
		resultsLabel.SetText("No matching templates found.\nYou can create a new template.")
		createBtn.Enable()
	} else {
		var resultStr strings.Builder
		resultStr.WriteString("Matching templates:\n")
		for _, tmpl := range matched {
			resultStr.WriteString(tmpl.ID + "\n")
		}
		resultsLabel.SetText(resultStr.String())
		createBtn.Disable()
	}
}

func createTemplateAction(parentWindow fyne.Window, urlEntry *widget.Entry) {
	url := strings.TrimSpace(urlEntry.Text)
	if url == "" {
		dialog.ShowInformation("Error", "Please enter a URL", parentWindow)
		return
	}

	tmpl := templates.GenerateTemplate(url)
	// if err != nil {
	// 	dialog.ShowError(fmt.Errorf("template generation failed: %w", err), parentWindow)
	// 	return
	// }

	data, err := yaml.Marshal(tmpl)
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to serialize template: %w", err), parentWindow)
		return
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil || writer == nil {
			return
		}
		_, err = writer.Write(data)
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

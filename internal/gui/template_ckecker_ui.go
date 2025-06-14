// package gui implements the user interface of the project - template checker section
package gui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/templates"
)

// BuildTemplateCheckerSection creates a UI section for checking and generating templates from URLs
func BuildTemplateCheckerSection(a fyne.App, parentWindow fyne.Window, logger *logging.Logger) fyne.CanvasObject {
	urlEntry := widget.NewEntry()
	urlEntry.SetPlaceHolder("Enter URL to check templates")

	templateCheckLabel := widget.NewLabel("Template folder: (not selected)")

	resultsOutput := widget.NewMultiLineEntry()
	resultsOutput.SetMinRowsVisible(10)
	resultsOutput.Wrapping = fyne.TextWrapWord

	createTemplateBtn := widget.NewButton("Create new template", nil)
	createTemplateBtn.Disable()

	var checkTemplatesDir string

	selectTemplateCheckDirBtn := widget.NewButton("Select templates folder for checking", func() {
		selectTemplatesFolder(parentWindow, &checkTemplatesDir, templateCheckLabel)
	})

	createTemplateBtn.OnTapped = func() {
		createTemplateAction(parentWindow, urlEntry)
	}

	advancedVisible := false

	semaphoreEntry := widget.NewEntry()
	semaphoreEntry.SetText("10") 

	rateFreqEntry := widget.NewEntry()
	rateFreqEntry.SetText("10") 

	rateBurstEntry := widget.NewEntry()
	rateBurstEntry.SetText("100")
	advanced := &templates.AdvancedSettingsChecker{}

	applyAdvancedBtn := widget.NewButton("Apply settings", func() {
		headlessTabs, err1 := strconv.Atoi(semaphoreEntry.Text)
		rateFreq, err2 := strconv.Atoi(rateFreqEntry.Text)
		burstSize, err3 := strconv.Atoi(rateBurstEntry.Text)

		if err1 != nil || err2 != nil || err3 != nil {
			dialog.ShowError(fmt.Errorf("incorrect values"), parentWindow)
			return
		}

		advanced.HeadlessTabs = headlessTabs
		advanced.RateLimiterFrequency = rateFreq
		advanced.RateLimiterBurstSize = burstSize

		dialog.ShowInformation("Success", "Settings changed", parentWindow)
	})

	advancedSettingsForm := container.NewVBox(
		widget.NewLabel("Advanced Settings"),
		widget.NewForm(
			widget.NewFormItem("Semaphore limit (tabs)", semaphoreEntry),
			widget.NewFormItem("Rate limiter frequency (milisecond)", rateFreqEntry),
			widget.NewFormItem("Rate limiter burst", rateBurstEntry),
		),
		applyAdvancedBtn,
	)
	advancedSettingsForm.Hide()

	checkTemplatesBtn := widget.NewButton("Check templates", func() {
		checkTemplatesAction(parentWindow, urlEntry, checkTemplatesDir, resultsOutput, createTemplateBtn, advanced, logger)
	})

	var toggleAdvancedBtn *widget.Button
	toggleAdvancedBtn = widget.NewButton("Advanced settings", func() {
		advancedVisible = !advancedVisible
		if advancedVisible {
			advancedSettingsForm.Show()
			toggleAdvancedBtn.SetText("Hide advanced settings")
		} else {
			advancedSettingsForm.Hide()
			toggleAdvancedBtn.SetText("Advanced settings")
		}
		parentWindow.Content().Refresh()
	})

	section := container.NewVBox(
		widget.NewLabel("Template Checker Section"),
		urlEntry,
		selectTemplateCheckDirBtn,
		templateCheckLabel,
		checkTemplatesBtn,
		resultsOutput,
		createTemplateBtn,
		toggleAdvancedBtn,
		advancedSettingsForm,
	)

	return section
}

// selectTemplatesFolder opens the dialog box for selecting a folder with templates and updates the path
func selectTemplatesFolder(parentWindow fyne.Window, dir *string, label *widget.Label) {
	fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil || uri == nil {
			return
		}
		*dir = uri.Path()
		label.SetText("Template folder: " + *dir)
	}, parentWindow)
	fd.Resize(fyne.NewSize(800, 600))
	fd.Show()
}

// checkTemplatesAction checks for matching templates for a given URL and updates the interface
func checkTemplatesAction(
	parentWindow fyne.Window,
	urlEntry *widget.Entry,
	templatesDir string,
	resultsOutput *widget.Entry,
	createBtn *widget.Button,
	advanced *templates.AdvancedSettingsChecker,
	logger *logging.Logger,
) {
	if templatesDir == "" {
		dialog.ShowInformation("Error", "Please select a templates folder", parentWindow)
		return
	}
	url := strings.TrimSpace(urlEntry.Text)
	if url == "" {
		dialog.ShowInformation("Error", "Please enter a URL", parentWindow)
		return
	}

	createBtn.Disable()
	resultsOutput.SetText("Starting template check...\n")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), constants.FiveMinTimeout)
		defer cancel()

		startTime := time.Now()
		var totalTemplates int

		progressCallback := func(i, total int) {
			totalTemplates = total
			line := fmt.Sprintf("Checked %d of %d templates...", i, total)
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				resultsOutput.SetText(line)
			}, true)
		}

		matched, err := templates.FindMatchingTemplates(ctx, url, templatesDir, constants.FiveSecTimeout, advanced, logger, progressCallback)
		duration := time.Since(startTime)
		if err != nil {
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {
				dialog.ShowError(err, parentWindow)
			}, true)
			return
		}

		lines := []string{
			fmt.Sprintf("Checked %d templates in %s", totalTemplates, duration.Round(time.Second)),
		}

		fyne.CurrentApp().Driver().DoFromGoroutine(func() {
			if len(matched) == 0 {
				lines = append(lines, "No matching templates found.\nYou can create a new template.")
				resultsOutput.SetText(strings.Join(lines, "\n"))
				createBtn.Enable()
			} else {
				lines = append(lines, "\nTotal matching: "+strconv.Itoa(len(matched)))
				lines = append(lines, "\nMatching templates:")
				for _, tmpl := range matched {
					lines = append(lines, tmpl.ID)
				}
				resultsOutput.SetText(strings.Join(lines, "\n"))
			}
		}, true)
	}()
}

// createTemplateAction generates a template for the specified URL and offers to save it to a file
func createTemplateAction(parentWindow fyne.Window, urlEntry *widget.Entry) {
	url := strings.TrimSpace(urlEntry.Text)
	if url == "" {
		dialog.ShowInformation("Error", "Please enter a URL", parentWindow)
		return
	}

	tmpl := templates.GenerateTemplate(url)
	if strings.HasPrefix(tmpl, "# Failed") {
		dialog.ShowError(fmt.Errorf("template generation failed:\n%s", tmpl), parentWindow)
		return
	}

	saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil || writer == nil {
			return
		}
		_, err = writer.Write([]byte(tmpl))
		if err != nil {
			dialog.ShowError(err, parentWindow)
			return
		}
		writer.Close()
		dialog.ShowInformation("Success", "Template saved", parentWindow)
	}, parentWindow)
	saveDialog.SetFileName("autogenerated-template.yaml")
	saveDialog.SetFilter(storage.NewExtensionFileFilter([]string{constants.YamlFileFormat, constants.YmlFileFormat}))
	saveDialog.Show()
}

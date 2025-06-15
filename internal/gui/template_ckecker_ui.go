// package gui implements the user interface of the project - template checker section
package gui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"os"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/templates"
)

// TemplateCheckerPageWidget holds all the widgets for the template checker section
type TemplateCheckerPageWidget struct {
	URLEntry                *walk.LineEdit
	TemplateCheckLabel      *walk.Label
	ResultsOutput           *walk.TextEdit
	CreateTemplateBtn       *walk.PushButton
	SelectTemplateDirBtn    *walk.PushButton
	CheckTemplatesBtn       *walk.PushButton
	ToggleAdvancedBtn       *walk.PushButton
	SemaphoreEntry          *walk.LineEdit
	RateFreqEntry           *walk.LineEdit
	RateBurstEntry          *walk.LineEdit
	ApplyAdvancedBtn        *walk.PushButton
	AdvancedGroup           *walk.GroupBox
}

var (
	templateCheckerWidget TemplateCheckerPageWidget
	checkTemplatesDir     string
	advancedVisible       bool
	advanced              = &templates.AdvancedSettingsChecker{}
)

// BuildTemplateCheckerSection creates a UI section for checking and generating templates from URLs
func BuildTemplateCheckerSection(logger *logging.Logger) (TabPage, *TemplateCheckerPageWidget) {
	page := TabPage{
		Title: "Template Checker",
		Layout: VBox{},
		Children: []Widget{
			Label{
				Text: "Template Checker Section",
				Font: Font{Bold: true, PointSize: 12},
			},
			VSpacer{Size: 10},
			
			LineEdit{
				AssignTo:    &templateCheckerWidget.URLEntry,
				Text:        "",
				CueBanner:   "Enter URL to check templates",
			},
			VSpacer{Size: 10},
			
			PushButton{
				AssignTo: &templateCheckerWidget.SelectTemplateDirBtn,
				Text:     "Select templates folder for checking",
				MinSize:  Size{250, 30},
			},
			Label{
				AssignTo: &templateCheckerWidget.TemplateCheckLabel,
				Text:     "Template folder: (not selected)",
			},
			VSpacer{Size: 10},
			
			PushButton{
				AssignTo: &templateCheckerWidget.CheckTemplatesBtn,
				Text:     "Check templates",
				MinSize:  Size{150, 30},
			},
			VSpacer{Size: 10},
			
			TextEdit{
				AssignTo: &templateCheckerWidget.ResultsOutput,
				MinSize:  Size{0, 200},
				VScroll:  true,
				ReadOnly: true,
			},
			VSpacer{Size: 10},
			
			PushButton{
				AssignTo: &templateCheckerWidget.CreateTemplateBtn,
				Text:     "Create new template",
				MinSize:  Size{150, 30},
				Enabled:  false,
			},
			VSpacer{Size: 10},
			
			PushButton{
				AssignTo: &templateCheckerWidget.ToggleAdvancedBtn,
				Text:     "Advanced settings",
				MinSize:  Size{150, 30},
			},
			
			GroupBox{
				AssignTo: &templateCheckerWidget.AdvancedGroup,
				Title:    "Advanced Settings",
				Layout:   VBox{},
				Visible:  false,
				Children: []Widget{
					Composite{
						Layout: Grid{Columns: 2},
						Children: []Widget{
							Label{Text: "Semaphore limit (tabs):"},
							LineEdit{
								AssignTo: &templateCheckerWidget.SemaphoreEntry,
								Text:     "10",
							},
							Label{Text: "Rate limiter frequency (millisecond):"},
							LineEdit{
								AssignTo: &templateCheckerWidget.RateFreqEntry,
								Text:     "10",
							},
							Label{Text: "Rate limiter burst:"},
							LineEdit{
								AssignTo: &templateCheckerWidget.RateBurstEntry,
								Text:     "100",
							},
						},
					},
					VSpacer{Size: 10},
					PushButton{
						AssignTo: &templateCheckerWidget.ApplyAdvancedBtn,
						Text:     "Apply settings",
						MinSize:  Size{120, 30},
					},
				},
			},
		},
	}

	return page, &templateCheckerWidget
}

// InitializeTemplateCheckerSection initializes the template checker section widgets with their event handlers
func InitializeTemplateCheckerSection(widget *TemplateCheckerPageWidget, parent walk.Form, logger *logging.Logger) {
	widget.SelectTemplateDirBtn.Clicked().Attach(func() {
		selectTemplatesFolder(parent, widget)
	})

	widget.CheckTemplatesBtn.Clicked().Attach(func() {
		checkTemplatesAction(parent, widget, logger)
	})

	widget.CreateTemplateBtn.Clicked().Attach(func() {
		createTemplateAction(parent, widget)
	})

	widget.ToggleAdvancedBtn.Clicked().Attach(func() {
		toggleAdvancedSettings(widget)
	})

	widget.ApplyAdvancedBtn.Clicked().Attach(func() {
		applyAdvancedSettings(parent, widget)
	})
}

// selectTemplatesFolder opens the dialog box for selecting a folder with templates and updates the path
func selectTemplatesFolder(parent walk.Form, widget *TemplateCheckerPageWidget) {
	dlg := new(walk.FileDialog)
	dlg.Title = "Select templates folder"

	if ok, err := dlg.ShowBrowseFolder(parent); err != nil {
		walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
		return
	} else if !ok {
		return
	}

	checkTemplatesDir = dlg.FilePath
	widget.TemplateCheckLabel.SetText("Template folder: " + checkTemplatesDir)
}

// toggleAdvancedSettings toggles the visibility of advanced settings
func toggleAdvancedSettings(widget *TemplateCheckerPageWidget) {
	advancedVisible = !advancedVisible
	widget.AdvancedGroup.SetVisible(advancedVisible)
	
	if advancedVisible {
		widget.ToggleAdvancedBtn.SetText("Hide advanced settings")
	} else {
		widget.ToggleAdvancedBtn.SetText("Advanced settings")
	}
}

// applyAdvancedSettings applies the advanced settings from the form
func applyAdvancedSettings(parent walk.Form, widget *TemplateCheckerPageWidget) {
	headlessTabs, err1 := strconv.Atoi(widget.SemaphoreEntry.Text())
	rateFreq, err2 := strconv.Atoi(widget.RateFreqEntry.Text())
	burstSize, err3 := strconv.Atoi(widget.RateBurstEntry.Text())

	if err1 != nil || err2 != nil || err3 != nil {
		walk.MsgBox(parent, "Error", "Incorrect values", walk.MsgBoxIconError)
		return
	}

	advanced.HeadlessTabs = headlessTabs
	advanced.RateLimiterFrequency = rateFreq
	advanced.RateLimiterBurstSize = burstSize

	walk.MsgBox(parent, "Success", "Settings changed", walk.MsgBoxIconInformation)
}

// checkTemplatesAction checks for matching templates for a given URL and updates the interface
func checkTemplatesAction(parent walk.Form, widget *TemplateCheckerPageWidget, logger *logging.Logger) {
	if checkTemplatesDir == "" {
		walk.MsgBox(parent, "Error", "Please select a templates folder", walk.MsgBoxIconInformation)
		return
	}
	
	url := strings.TrimSpace(widget.URLEntry.Text())
	if url == "" {
		walk.MsgBox(parent, "Error", "Please enter a URL", walk.MsgBoxIconInformation)
		return
	}

	widget.CreateTemplateBtn.SetEnabled(false)
	widget.ResultsOutput.SetText("Starting template check...\n")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), constants.FiveMinTimeout)
		defer cancel()

		startTime := time.Now()
		var totalTemplates int

		progressCallback := func(i, total int) {
			totalTemplates = total
			line := fmt.Sprintf("Checked %d of %d templates...", i, total)
			widget.ResultsOutput.Synchronize(func() {
				widget.ResultsOutput.SetText(line)
			})
		}

		matched, err := templates.FindMatchingTemplates(ctx, url, checkTemplatesDir, constants.FiveSecTimeout, advanced, logger, progressCallback)
		duration := time.Since(startTime)
		
		if err != nil {
			widget.ResultsOutput.Synchronize(func() {
				walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
			})
			return
		}

		lines := []string{
			fmt.Sprintf("Checked %d templates in %s", totalTemplates, duration.Round(time.Second)),
		}

		widget.ResultsOutput.Synchronize(func() {
			if len(matched) == 0 {
				lines = append(lines, "No matching templates found.\nYou can create a new template.")
				widget.ResultsOutput.SetText(strings.Join(lines, "\n"))
				widget.CreateTemplateBtn.SetEnabled(true)
			} else {
				lines = append(lines, "\nTotal matching: "+strconv.Itoa(len(matched)))
				lines = append(lines, "\nMatching templates:")
				for _, tmpl := range matched {
					lines = append(lines, tmpl.ID)
				}
				widget.ResultsOutput.SetText(strings.Join(lines, "\n"))
			}
		})
	}()
}

// createTemplateAction generates a template for the specified URL and offers to save it to a file
func createTemplateAction(parent walk.Form, widget *TemplateCheckerPageWidget) {
	url := strings.TrimSpace(widget.URLEntry.Text())
	if url == "" {
		walk.MsgBox(parent, "Error", "Please enter a URL", walk.MsgBoxIconInformation)
		return
	}

	tmpl := templates.GenerateTemplate(url)
	if strings.HasPrefix(tmpl, "# Failed") {
		walk.MsgBox(parent, "Error", fmt.Sprintf("Template generation failed:\n%s", tmpl), walk.MsgBoxIconError)
		return
	}

	dlg := new(walk.FileDialog)
	dlg.Filter = "YAML Files (*.yaml;*.yml)|*.yaml;*.yml"
	dlg.FilePath = "autogenerated-template.yaml"
	dlg.Title = "Save template"

	if ok, err := dlg.ShowSave(parent); err != nil {
		walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
		return
	} else if !ok {
		return
	}

	if err := os.WriteFile(dlg.FilePath, []byte(tmpl), 0644); err != nil {
		walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
		return
	}

	walk.MsgBox(parent, "Success", "Template saved", walk.MsgBoxIconInformation)
}
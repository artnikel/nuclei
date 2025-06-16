// package gui implements the user interface of the project - template checker section
package gui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"os"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/templates"
	"github.com/artnikel/nuclei/internal/templates/headless"
)

// TemplateCheckerPageWidget holds all the widgets for the template checker section
type TemplateCheckerPageWidget struct {
	URLEntry             *walk.LineEdit
	TemplateCheckLabel   *walk.Label
	ResultsOutput        *walk.TextEdit
	CreateTemplateBtn    *walk.PushButton
	SelectTemplateDirBtn *walk.PushButton
	CheckTemplatesBtn    *walk.PushButton
	ToggleAdvancedBtn    *walk.PushButton
	SemaphoreEntry       *walk.LineEdit
	RateFreqEntry        *walk.LineEdit
	RateBurstEntry       *walk.LineEdit
	ThreadsEntry         *walk.LineEdit
	TimeoutEntry         *walk.LineEdit
	ApplyAdvancedBtn     *walk.PushButton
	AdvancedGroup        *walk.GroupBox
	StopBtn              *walk.PushButton
}

var (
	templateCheckerWidget TemplateCheckerPageWidget
	checkTemplatesDir     string
	advancedVisible       bool
	isChecking            = &atomic.Bool{}
	cancelCheck           context.CancelFunc
	advanced              = &templates.AdvancedSettingsChecker{
		HeadlessTabs:         10,
		RateLimiterFrequency: 10,
		RateLimiterBurstSize: 100,
		Threads:              300,
		Timeout:              500 * time.Second,
	}
)

// BuildTemplateCheckerSection creates a UI section for checking and generating templates from URLs
func BuildTemplateCheckerSection(logger *logging.Logger) (TabPage, *TemplateCheckerPageWidget) {
	page := TabPage{
		Title:  "Template Checker",
		Layout: VBox{},
		Children: []Widget{
			Label{
				Text: "Template Checker Section",
				Font: Font{Bold: true, PointSize: 12},
			},
			VSpacer{Size: 10},

			LineEdit{
				AssignTo:  &templateCheckerWidget.URLEntry,
				Text:      "",
				CueBanner: "Enter URL to check templates",
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

			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						AssignTo: &templateCheckerWidget.CheckTemplatesBtn,
						Text:     "Check templates",
						MinSize:  Size{150, 30},
					},
					PushButton{
						AssignTo: &templateCheckerWidget.StopBtn,
						Text:     "Stop",
						MinSize:  Size{80, 30},
						Enabled:  false,
					},
				},
			},
			VSpacer{Size: 10},

			TextEdit{
				AssignTo: &templateCheckerWidget.ResultsOutput,
				MinSize:  Size{0, 200},
				VScroll:  true,
				ReadOnly: true,
				HScroll:  false,
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
							Label{Text: "Threads:"},
							LineEdit{
								AssignTo: &templateCheckerWidget.ThreadsEntry,
								Text:     "300",
							},
							Label{Text: "Timeout (seconds):"},
							LineEdit{
								AssignTo: &templateCheckerWidget.TimeoutEntry,
								Text:     "500",
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
	widget.StopBtn.Clicked().Attach(func() {
		if cancelCheck != nil {
			cancelCheck()
			headless.ForceReinitHeadless()
		}
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
	threads, err4 := strconv.Atoi(widget.ThreadsEntry.Text())
	timeout, err5 := strconv.Atoi(widget.TimeoutEntry.Text())

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		walk.MsgBox(parent, "Error", "Incorrect values", walk.MsgBoxIconError)
		return
	}

	advanced.HeadlessTabs = headlessTabs
	advanced.RateLimiterFrequency = rateFreq
	advanced.RateLimiterBurstSize = burstSize
	advanced.Threads = threads
	advanced.Timeout = time.Duration(timeout) * time.Second

	walk.MsgBox(parent, "Success", "Settings changed", walk.MsgBoxIconInformation)
}

// checkTemplatesAction checks for matching templates for a given URL and updates the interface
func checkTemplatesAction(parent walk.Form, widget *TemplateCheckerPageWidget, logger *logging.Logger) {
	if isRunning.Load() {
		walk.MsgBox(parent, "Checker running", "Checker is already running", walk.MsgBoxIconInformation)
		return
	}

	if checkTemplatesDir == "" {
		widget.ResultsOutput.Synchronize(func() {
			walk.MsgBox(parent, "Error", "Please select a templates folder", walk.MsgBoxIconInformation)
		})
		return
	}

	url := strings.TrimSpace(widget.URLEntry.Text())
	if url == "" {
		widget.ResultsOutput.Synchronize(func() {
			walk.MsgBox(parent, "Error", "Please enter a URL", walk.MsgBoxIconInformation)
		})
		return
	}
	isChecking.Store(true)
	widget.StopBtn.SetEnabled(true)
	widget.CheckTemplatesBtn.SetEnabled(false)
	widget.CreateTemplateBtn.SetEnabled(false)
	widget.ResultsOutput.SetText("Starting template check...\n")

	ctx, cancel := context.WithTimeout(context.Background(), advanced.Timeout)
	cancelCheck = cancel
	headless.ForceReinitHeadless()

	go func() {
		defer func() {
			isChecking.Store(false)
			widget.StopBtn.SetEnabled(false)
			widget.CheckTemplatesBtn.SetEnabled(true)
		}()

		startTime := time.Now()
		var totalTemplates int
		var currentChecked atomic.Int32

		// Прогресс обновляется из MatchTemplate
		progressCallback := func(i, total int) {
			currentChecked.Store(int32(i))
			totalTemplates = total
		}

		// Обновление UI каждую секунду
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		done := make(chan struct{})

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-done:
					return
				case <-ticker.C:
					checked := currentChecked.Load()
					elapsed := time.Since(startTime).Round(time.Second)
					line := fmt.Sprintf("Checked %d of %d templates... (%s elapsed)", checked, totalTemplates, elapsed)

					widget.ResultsOutput.Synchronize(func() {
						widget.ResultsOutput.SetText(line)
					})
				}
			}
		}()

		matched, err := templates.FindMatchingTemplates(ctx, url, checkTemplatesDir, constants.FiveSecTimeout, advanced, logger, progressCallback)
		duration := time.Since(startTime)
		close(done)

		if err != nil {
			widget.ResultsOutput.Synchronize(func() {
				if errors.Is(err, context.Canceled) {
					widget.ResultsOutput.SetText("Template checking was canceled.")
				} else {
					walk.MsgBox(parent, "Error", err.Error(), walk.MsgBoxIconError)
				}
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
				lines = append(lines, "\n Total matching: "+strconv.Itoa(len(matched)))
				lines = append(lines, "\n Matching templates:")
				for _, tmpl := range matched {
					lines = append(lines, " / "+tmpl.ID)
				}
				result := strings.Join(lines, "\n")
				widget.ResultsOutput.SetText(result)
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

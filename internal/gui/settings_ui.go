package gui

import (
	"strconv"
	"time"

	"github.com/artnikel/nuclei/internal/templates"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type SettingsPageWidget struct {
	SemaphoreEntry   *walk.LineEdit
	RateFreqEntry    *walk.LineEdit
	RateBurstEntry   *walk.LineEdit
	ThreadsEntry     *walk.LineEdit
	TimeoutEntry     *walk.LineEdit
	ApplyAdvancedBtn *walk.PushButton
	AdvancedGroup    *walk.GroupBox
}

var (
	settingsPageWidget SettingsPageWidget
	advanced           = &templates.AdvancedSettingsChecker{
		Workers:              300,
		Timeout:              500 * time.Second,
		HeadlessTabs:         10,
		RateLimiterFrequency: 10,
		RateLimiterBurstSize: 100,
	}
)

func BuildSettingsSection() (TabPage, *SettingsPageWidget) {
	page := TabPage{
		Title:  "Settings",
		Layout: VBox{},
		Children: []Widget{
			Label{
				Text: "Settings Section",
				Font: Font{Bold: true, PointSize: 12},
			},
			VSpacer{Size: 20},

			GroupBox{
				Layout: VBox{},
				Children: []Widget{
					Label{Text: "Max goroutines:"},
					LineEdit{
						AssignTo: &settingsPageWidget.ThreadsEntry,
						Text:     "300",
					},
					Label{Text: "Timeout (seconds):"},
					LineEdit{
						AssignTo: &settingsPageWidget.TimeoutEntry,
						Text:     "500",
					},
				},
			},

			GroupBox{
				AssignTo: &settingsPageWidget.AdvancedGroup,
				Title:    "Advanced Settings",
				Layout:   VBox{},
				Visible:  true,
				Children: []Widget{
					Composite{
						Layout: Grid{Columns: 2},
						Children: []Widget{
							Label{Text: "Semaphore limit (tabs):"},
							LineEdit{
								AssignTo: &settingsPageWidget.SemaphoreEntry,
								Text:     "10",
							},
							Label{Text: "Rate limiter frequency (millisecond):"},
							LineEdit{
								AssignTo: &settingsPageWidget.RateFreqEntry,
								Text:     "10",
							},
							Label{Text: "Rate limiter burst:"},
							LineEdit{
								AssignTo: &settingsPageWidget.RateBurstEntry,
								Text:     "100",
							},
						},
					},
				},
			},

			VSpacer{Size: 10},
			PushButton{
				AssignTo: &settingsPageWidget.ApplyAdvancedBtn,
				Text:     "Apply settings",
				MinSize:  Size{Width: 120, Height: 30},
			},
		},
	}

	return page, &settingsPageWidget
}


// InitializeLicenseSection initializes the license section widgets with their event handlers
func InitializeSettingsSection(widget *SettingsPageWidget, parent walk.Form) {
	widget.ApplyAdvancedBtn.Clicked().Attach(func() {
		applyAdvancedSettings(parent, widget)
	})
}

// applyAdvancedSettings applies the advanced settings from the form
func applyAdvancedSettings(parent walk.Form, widget *SettingsPageWidget) {
	workers, err1 := strconv.Atoi(widget.ThreadsEntry.Text())
	timeout, err2 := strconv.Atoi(widget.TimeoutEntry.Text())
	headlessTabs, err3 := strconv.Atoi(widget.SemaphoreEntry.Text())
	rateFreq, err4 := strconv.Atoi(widget.RateFreqEntry.Text())
	burstSize, err5 := strconv.Atoi(widget.RateBurstEntry.Text())

	if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil {
		walk.MsgBox(parent, "Error", "Incorrect values", walk.MsgBoxIconError)
		return
	}

	advanced.Workers = workers
	advanced.Timeout = time.Duration(timeout) * time.Second
	advanced.HeadlessTabs = headlessTabs
	advanced.RateLimiterFrequency = rateFreq
	advanced.RateLimiterBurstSize = burstSize

	walk.MsgBox(parent, "Success", "Settings changed", walk.MsgBoxIconInformation)
}

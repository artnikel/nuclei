// package gui implements the user interface of the project - license status section
package gui

import (
	"fmt"
	"time"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"

	"github.com/artnikel/nuclei/internal/config"
	"github.com/artnikel/nuclei/internal/license"
)

// LicensePageWidget holds all the widgets for the license section
type LicensePageWidget struct {
	StatusLabel     *walk.Label
	CreatedAtLabel  *walk.Label
	LastCheckLabel  *walk.Label
	CheckBtn        *walk.PushButton
}

var licenseWidget LicensePageWidget

// BuildLicenseSection creates the UI section for displaying the license status
func BuildLicenseSection() (TabPage, *LicensePageWidget) {
	page := TabPage{
		Title: "License",
		Layout: VBox{},
		Children: []Widget{
			Label{
				Text: "License Status Section",
				Font: Font{Bold: true, PointSize: 12},
			},
			VSpacer{Size: 20},
			
			Label{
				AssignTo: &licenseWidget.StatusLabel,
				Text:     "License Status: Unknown",
				Font:     Font{PointSize: 10},
			},
			VSpacer{Size: 10},
			
			Label{
				AssignTo: &licenseWidget.CreatedAtLabel,
				Text:     "",
			},
			VSpacer{Size: 10},
			
			Label{
				AssignTo: &licenseWidget.LastCheckLabel,
				Text:     "",
			},
			VSpacer{Size: 20},
			
			PushButton{
				AssignTo: &licenseWidget.CheckBtn,
				Text:     "Check License",
				MinSize:  Size{150, 30},
			},
			
			VSpacer{},
		},
	}

	return page, &licenseWidget
}

// InitializeLicenseSection initializes the license section widgets with their event handlers
func InitializeLicenseSection(widget *LicensePageWidget, parent walk.Form) {
	widget.CheckBtn.Clicked().Attach(func() {
		checkLicenseAction(parent, widget)
	})
}

// checkLicenseAction handles the license check button click
func checkLicenseAction(parent walk.Form, widget *LicensePageWidget) {
	widget.StatusLabel.SetText("Checking license...")
	widget.CreatedAtLabel.SetText("")
	widget.LastCheckLabel.SetText("")
	widget.CheckBtn.SetEnabled(false)

	go func() {
		defer func() {
			widget.CheckBtn.Synchronize(func() {
				widget.CheckBtn.SetEnabled(true)
			})
		}()

		cfg, err := config.LoadConfig("config.yaml")
		if err != nil {
			widget.StatusLabel.Synchronize(func() {
				widget.StatusLabel.SetText("License check failed")
				walk.MsgBox(parent, "Error", fmt.Sprintf("Failed to load config: %v", err), walk.MsgBoxIconError)
			})
			return
		}

		if cfg.License.Key == "" || cfg.License.ServerURL == "" {
			widget.StatusLabel.Synchronize(func() {
				widget.StatusLabel.SetText("License configuration incomplete")
				walk.MsgBox(parent, "Error", "Fill the license fields in config.yaml", walk.MsgBoxIconError)
			})
			return
		}

		lc := license.NewLicenseClient(cfg.License.ServerURL, cfg.License.Key)
		err = lc.CheckLicense()

		widget.StatusLabel.Synchronize(func() {
			if err != nil {
				widget.StatusLabel.SetText("License is invalid")
				widget.CreatedAtLabel.SetText("")
				widget.LastCheckLabel.SetText("")
				return
			}

			if lc.IsValid() {
				widget.StatusLabel.SetText("License is valid")
				widget.CreatedAtLabel.SetText("Created At: " + lc.LicenseData.CreatedAt.Format(time.RFC1123))
				widget.LastCheckLabel.SetText("Last Check: " + lc.LicenseData.LastCheck.Format(time.RFC1123))
			} else {
				widget.StatusLabel.SetText("License is invalid")
				widget.CreatedAtLabel.SetText("")
				widget.LastCheckLabel.SetText("")
			}
		})
	}()
}
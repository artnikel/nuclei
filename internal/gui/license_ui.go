// package gui implements the user interface of the project - license status section
package gui

import (
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/config"
	"github.com/artnikel/nuclei/internal/license"
)

// BuildLicenseSection creates the UI section for displaying the license status
func BuildLicenseSection(a fyne.App, w fyne.Window) fyne.CanvasObject {
	statusLabel := widget.NewLabel("License Status Section")
	createdAtLabel := widget.NewLabel("")
	lastCheckLabel := widget.NewLabel("")

	checkBtn := widget.NewButton("Check License", func() {
		statusLabel.SetText("Checking license...")
		createdAtLabel.SetText("")
		lastCheckLabel.SetText("")

		go func() {
			cfg, err := config.LoadConfig("config.yaml")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load config: %v", err), w)
				return
			}
			if cfg.License.Key == "" || cfg.License.ServerURL == "" {
				dialog.ShowError(fmt.Errorf("fill the license fields in config.yaml"), w)
				return
			}
			lc := license.NewLicenseClient(cfg.License.ServerURL, cfg.License.Key)
			err = lc.CheckLicense()
			fyne.CurrentApp().Driver().DoFromGoroutine(func() {

				if err != nil {
					statusLabel.SetText("License is invalid")
					createdAtLabel.SetText("")
					lastCheckLabel.SetText("")
					return
				}

				if lc.IsValid() {
					statusLabel.SetText("License is valid")
					createdAtLabel.SetText("Created At: " + lc.LicenseData.CreatedAt.Format(time.RFC1123))
					lastCheckLabel.SetText("Last Check: " + lc.LicenseData.LastCheck.Format(time.RFC1123))

				} else {
					statusLabel.SetText("License is invalid")
					createdAtLabel.SetText("")
					lastCheckLabel.SetText("")
				}
			}, true)
		}()
	})

	content := container.NewVBox(
		statusLabel,
		createdAtLabel,
		lastCheckLabel,
		checkBtn,
	)

	return container.NewScroll(content)
}

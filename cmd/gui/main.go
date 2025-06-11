//go:generate make release
package main

import (
	"log"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"

	"github.com/artnikel/nuclei/internal/config"
	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/gui"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/security"
	"github.com/artnikel/nuclei/pkg/license"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	logger, err := logging.NewLogger(cfg.Logging.Path)
	if err != nil {
		log.Fatalf("failed to init logger: %v", err)
	}

	go func() {
		for {
			if security.IsBeingDebugged() {
				logger.Error.Fatalf("Debug detected. Exiting.")
				os.Exit(1)
			}
			time.Sleep(constants.FiveSecTimeout)
		}
	}()

	lc := license.NewLicenseClient(cfg.License.ServerURL, cfg.License.Key)
	go func() {
		for {
			time.Sleep(constants.DayTimeout)

			if err := lc.CheckLicense(); err != nil {
				logger.Error.Fatalf("Failed to verify the license: %v", err)
			}
		}
	}()

	a := app.NewWithID(cfg.App.ID)
	w := a.NewWindow("Nuclei 3.0 GUI Scanner")

	scannerSection, _, _ := gui.BuildScannerSection(a, w, logger)
	templateCheckerSection := gui.BuildTemplateCheckerSection(a, w, logger)

	tabs := container.NewAppTabs(
		container.NewTabItem("Scanner", scannerSection),
		container.NewTabItem("Template Checker", templateCheckerSection),
	)
	const (
		width  = 600
		heigth = 500
	)
	w.SetContent(tabs)
	w.Resize(fyne.NewSize(width, heigth))
	w.ShowAndRun()
}

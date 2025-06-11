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
	"github.com/artnikel/nuclei/internal/gui"
	"github.com/artnikel/nuclei/internal/security"
	"github.com/artnikel/nuclei/pkg/license"
)

func main() {
	cfg, err := config.LoadConfig("config.yaml")
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	go func() {
		for {
			if security.IsBeingDebugged() {
				log.Println("Debug detected. Exiting.")
				os.Exit(1)
			}
			time.Sleep(5 * time.Second)
		}
	}()

	lc := license.NewLicenseClient(cfg.License.ServerURL, cfg.License.Key)
	go func() {
		for {
			time.Sleep(24 * time.Hour)

			if err := lc.CheckLicense(); err != nil {
				log.Println("Failed to verify the license:", err)
			}
		}
	}()

	a := app.NewWithID(cfg.App.ID)
	w := a.NewWindow("Nuclei 3.0 GUI Scanner")

	scannerSection, _, _ := gui.BuildScannerSection(a, w)
	templateCheckerSection := gui.BuildTemplateCheckerSection(a, w)

	tabs := container.NewAppTabs(
		container.NewTabItem("Scanner", scannerSection),
		container.NewTabItem("Template Checker", templateCheckerSection),
	)

	w.SetContent(tabs)
	w.Resize(fyne.NewSize(600, 500))
	w.ShowAndRun()
}

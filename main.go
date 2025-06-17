package main

import (
	"fmt"
	"log"
	"os"
	"runtime/debug"
	"time"

	"github.com/artnikel/nuclei/internal/config"
	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/gui"
	"github.com/artnikel/nuclei/internal/license"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/security"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			errMsg := fmt.Sprintf("Panic caught: %v\n%s", r, debug.Stack())

			fmt.Println(errMsg)

			walk.MsgBox(nil, "Fatal error", errMsg, walk.MsgBoxIconError)
		}
	}()
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

	go func() {
		for {
			cfg, err := config.LoadConfig("config.yaml")
			if err != nil {
				logger.Error.Fatalf("Failed to load config: %v", err)
			}
			lc := license.NewLicenseClient(cfg.License.ServerURL, cfg.License.Key)
			time.Sleep(constants.DayTimeout)

			if err := lc.CheckLicense(); err != nil {
				logger.Error.Fatalf("Failed to verify the license: %v", err)
				os.Exit(1)
			}
		}
	}()

	var mw *walk.MainWindow
	var tabWidget *walk.TabWidget

	// Create scanner section
	scannerPage, scannerPageWidget := gui.BuildScannerSection(logger)

	// Create template checker section
	templateCheckerPage, templateCheckerPageWidget := gui.BuildTemplateCheckerSection(logger)

	// Create license section
	licensePage, licensePageWidget := gui.BuildLicenseSection()

	// Create settings section
	settingsPage, settingsPageWidget := gui.BuildSettingsSection()
	
	if err := (MainWindow{
		AssignTo: &mw,
		Title:    "Nuclei 3.0 GUI Scanner",
		MinSize:  Size{Width: 800, Height: 750},
		Size:     Size{Width: 800, Height: 750},
		Layout:   VBox{},
		Children: []Widget{
			TabWidget{
				AssignTo: &tabWidget,
				Pages: []TabPage{
					scannerPage,
					templateCheckerPage,
					licensePage,
					settingsPage,
				},
			},
		},
	}.Create()); err != nil {
		log.Fatal(err)
	}

	// Initialize the widgets after window creation
	gui.InitializeScannerSection(scannerPageWidget, mw, logger)
	gui.InitializeTemplateCheckerSection(templateCheckerPageWidget, mw, logger)
	gui.InitializeLicenseSection(licensePageWidget, mw)
	gui.InitializeSettingsSection(settingsPageWidget, mw)

	mw.Run()
}

package main

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"

	"github.com/artnikel/nuclei/internal/gui"
)

func main() {
	a := app.NewWithID("com.artnikel.nuclei")
	w := a.NewWindow("Nuclei 3.0 GUI Scanner")

	scannerSection, _, _ := gui.BuildScannerSection(a, w)
	templateCheckerSection := gui.BuildTemplateCheckerSection(a, w)

	tabs := container.NewAppTabs(
		container.NewTabItem("Scanner", scannerSection),
		container.NewTabItem("Template Checker", templateCheckerSection),
	)

	w.SetContent(tabs)
	w.Resize(fyne.NewSize(600, 450))
	w.ShowAndRun()
}

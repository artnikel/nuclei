package main

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/scanner"
)

func main() {
	a := app.New()
	w := a.NewWindow("Nuclei 3.0 GUI Scanner")

	var targetsFile string
	var templatesDir string

	targetsLabel := widget.NewLabel("Targets: (not selected)")
	templatesLabel := widget.NewLabel("Templates: (not selected)")

	selectTargetsBtn := widget.NewButton("Select targets.txt", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			targetsFile = reader.URI().Path()
			targetsLabel.SetText("Targets: " + targetsFile)
		}, w)
		fd.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
		fd.Show()
	})

	selectTemplatesBtn := widget.NewButton("Select templates folder", func() {
		fd := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil || uri == nil {
				return
			}
			templatesDir = uri.Path()
			templatesLabel.SetText("Templates: " + templatesDir)
		}, w)
		fd.Show()
	})

	threadsEntry := widget.NewEntry()
	threadsEntry.SetText("10")

	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText("5")

	statsLabel := widget.NewLabel("Statistics:\nTargets loaded: 0\nProcessed: 0\nSuccesses: 0")

	startBtn := widget.NewButton("Start", nil)
	stopBtn := widget.NewButton("Stop", nil)
	stopBtn.Disable()

	var isRunning atomic.Bool

	startBtn.OnTapped = func() {
		if isRunning.Load() {
			dialog.ShowInformation("Scanner running", "Scanner is already running", w)
			return
		}

		threads, err1 := strconv.Atoi(threadsEntry.Text)
		timeoutSec, err2 := strconv.Atoi(timeoutEntry.Text)

		if err1 != nil || threads <= 0 {
			dialog.ShowError(fmt.Errorf("invalid thread count"), w)
			return
		}
		if err2 != nil || timeoutSec <= 0 {
			dialog.ShowError(fmt.Errorf("invalid timeout"), w)
			return
		}
		if targetsFile == "" {
			dialog.ShowError(fmt.Errorf("targets file not selected"), w)
			return
		}
		if templatesDir == "" {
			dialog.ShowError(fmt.Errorf("templates folder not selected"), w)
			return
		}

		isRunning.Store(true)
		startBtn.Disable()
		stopBtn.Enable()

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			defer func() {
				isRunning.Store(false)
				startBtn.Enable()
				stopBtn.Disable()
			}()

			var totalTargets int64 = 0
			var processed int64 = 0
			var success int64 = 0

			targetsChan, errChan := scanner.ReadTargets(ctx, targetsFile)

			go func() {
				for err := range errChan {
					if err != nil {
						log.Println("Error reading targets:", err)
					}
				}
			}()

			processFn := func(ctx context.Context, target string) error {
				time.Sleep(50 * time.Millisecond)
				return nil
			}

			wrappedProcessFn := func(ctx context.Context, target string) error {
				err := processFn(ctx, target)
				atomic.AddInt64(&processed, 1)
				if err == nil {
					atomic.AddInt64(&success, 1)
				}
				return err
			}

			go func() {
				for range targetsChan {
					atomic.AddInt64(&totalTargets, 1)
				}
			}()

			targetsChan, _ = scanner.ReadTargets(ctx, targetsFile)

			resultsCh := scanner.StartWorkers(ctx, targetsChan, threads, wrappedProcessFn)

			ticker := time.NewTicker(300 * time.Millisecond)
			defer ticker.Stop()

		loop:
			for {
				select {
				case <-ctx.Done():
					break loop
				case _, ok := <-resultsCh:
					if !ok {
						break loop
					}
				case <-ticker.C:
					statsLabel.SetText(fmt.Sprintf("Statistics:\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d",
						atomic.LoadInt64(&totalTargets),
						atomic.LoadInt64(&processed),
						atomic.LoadInt64(&success)))
				}
			}

			statsLabel.SetText(fmt.Sprintf("Scan finished.\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d",
				atomic.LoadInt64(&totalTargets),
				atomic.LoadInt64(&processed),
				atomic.LoadInt64(&success)))
		}()

		stopBtn.OnTapped = func() {
			cancel()
		}
	}

	stopBtn.OnTapped = func() {}

	content := container.NewVBox(
		selectTargetsBtn, targetsLabel,
		selectTemplatesBtn, templatesLabel,
		widget.NewForm(
			widget.NewFormItem("Number of threads", threadsEntry),
			widget.NewFormItem("Timeout (seconds)", timeoutEntry),
		),
		statsLabel,
		container.NewHBox(startBtn, stopBtn),
	)

	w.SetContent(content)
	w.Resize(fyne.NewSize(450, 350))
	w.ShowAndRun()
}

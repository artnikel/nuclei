package main

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/scanner"
)

func main() {
	a := app.NewWithID("com.artnikel.nuclei")
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

	maxThreads := runtime.NumCPU()

	options := []string{}
	for i := 1; i <= maxThreads; i++ {
		options = append(options, strconv.Itoa(i))
	}
	threadsSelect := widget.NewSelect(options, nil)
	threadsSelect.SetSelected(strconv.Itoa(maxThreads))

	timeoutEntry := widget.NewEntry()
	timeoutEntry.SetText("5")

	statsBinding := binding.NewString()
	_ = statsBinding.Set("Statistics:\nTargets loaded: 0\nProcessed: 0\nSuccesses: 0")
	statsLabel := widget.NewLabelWithData(statsBinding)

	startBtn := widget.NewButton("Start", nil)
	stopBtn := widget.NewButton("Stop", nil)
	stopBtn.Disable()

	var isRunning atomic.Bool

	var cancelScan context.CancelFunc

	startBtn.OnTapped = func() {
		if isRunning.Load() {
			dialog.ShowInformation("Scanner running", "Scanner is already running", w)
			return
		}

		threads, err1 := strconv.Atoi(threadsSelect.Selected)
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
		cancelScan = cancel

		statsUpdateCh := make(chan string, 10)

		go func() {
			for update := range statsUpdateCh {
				_ = statsBinding.Set(update)
			}
		}()

		go func() {
			defer func() {
				close(statsUpdateCh)
				a.Driver().DoFromGoroutine(func() {
					isRunning.Store(false)
					startBtn.Enable()
					stopBtn.Disable()
				}, true)
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

			var targetList []string
			for t := range targetsChan {
				targetList = append(targetList, t)
			}
			totalTargets = int64(len(targetList))

			newTargetsChan := make(chan string, totalTargets)
			go func() {
				defer close(newTargetsChan)
				for _, t := range targetList {
					select {
					case <-ctx.Done():
						return
					case newTargetsChan <- t:
					}
				}
			}()

			processFn := func(ctx context.Context, target string) error {
				// target handling logic
				time.Sleep(time.Duration(timeoutSec) * time.Second)
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

			resultsCh := scanner.StartWorkers(ctx, newTargetsChan, threads, wrappedProcessFn)

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
					statsUpdateCh <- fmt.Sprintf(
						"Statistics:\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d",
						totalTargets,
						atomic.LoadInt64(&processed),
						atomic.LoadInt64(&success),
					)
				}
			}

			statsUpdateCh <- fmt.Sprintf(
				"Scan finished.\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d",
				totalTargets,
				atomic.LoadInt64(&processed),
				atomic.LoadInt64(&success),
			)
		}()

		stopBtn.OnTapped = func() {
			if cancelScan != nil {
				cancelScan()
			}
		}
	}

	content := container.NewVBox(
		selectTargetsBtn, targetsLabel,
		selectTemplatesBtn, templatesLabel,
		widget.NewForm(
			widget.NewFormItem("Number of threads", threadsSelect),
			widget.NewFormItem("Timeout (seconds)", timeoutEntry),
		),
		statsLabel,
		container.NewHBox(startBtn, stopBtn),
	)

	w.SetContent(content)
	w.Resize(fyne.NewSize(450, 350))
	w.ShowAndRun()
}

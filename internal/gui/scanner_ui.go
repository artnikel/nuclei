// package gui implements the user interface of the project - scanner section
package gui

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/data/binding"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/artnikel/nuclei/internal/constants"
	"github.com/artnikel/nuclei/internal/logging"
	"github.com/artnikel/nuclei/internal/scanner"
	"github.com/artnikel/nuclei/internal/templates"
)

// BuildScannerSection builds the scanner UI section and returns it along with the start flag and cancel function
func BuildScannerSection(a fyne.App, w fyne.Window, logger *logging.Logger) (fyne.CanvasObject, *atomic.Bool, *context.CancelFunc) {
	var targetsFile string
	var templatesDir string

	isRunning := &atomic.Bool{}
	var cancelScan context.CancelFunc

	targetsLabel := widget.NewLabel("Targets: (not selected)")
	templatesLabel := widget.NewLabel("Templates: (not selected)")

	selectTargetsBtn := newSelectTargetsButton(w, &targetsFile, targetsLabel)
	selectTemplatesBtn := newSelectTemplateButton(w, &templatesDir, templatesLabel)

	maxThreads := runtime.NumCPU()
	threadsEntry := newThreadsEntry(maxThreads)
	timeoutEntry := newTimeoutEntry()

	statsBinding := binding.NewString()
	_ = statsBinding.Set(initialStatsText())
	statsLabel := widget.NewLabelWithData(statsBinding)

	startBtn := widget.NewButton("Start", nil)
	stopBtn := widget.NewButton("Stop", nil)
	stopBtn.Disable()

	startBtn.OnTapped = func() {
		handleStartButtonClick(a, w, targetsFile, templatesDir, threadsEntry, timeoutEntry, statsBinding, isRunning, startBtn, stopBtn, &cancelScan, logger)
	}

	stopBtn.OnTapped = func() {
		if cancelScan != nil {
			cancelScan()
		}
	}

	section := container.NewVBox(
		widget.NewLabel("Scan Targets Section"),
		selectTargetsBtn, targetsLabel,
		selectTemplatesBtn, templatesLabel,
		widget.NewForm(
			widget.NewFormItem("Number of threads", threadsEntry),
			widget.NewFormItem("Timeout (seconds)", timeoutEntry),
		),
		container.NewHBox(startBtn, stopBtn),
		statsLabel,
	)

	return section, isRunning, &cancelScan
}

// newSelectTargetsButton creates a button to select a file with scan targets
func newSelectTargetsButton(w fyne.Window, targetsFile *string, label *widget.Label) *widget.Button {
	return widget.NewButton("Select targets (.txt)", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			*targetsFile = reader.URI().Path()
			label.SetText("Targets: " + *targetsFile)
		}, w)
		fd.Resize(fyne.NewSize(800, 600))
		fd.SetFilter(storage.NewExtensionFileFilter([]string{constants.TxtFileFormat}))
		fd.Show()
	})
}

// newSelectTemplateButton creates a button to select a template directory
func newSelectTemplateButton(w fyne.Window, templatesFile *string, label *widget.Label) *widget.Button {
	return widget.NewButton("Select template (.yaml/.yml)", func() {
		fd := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil || reader == nil {
				return
			}
			*templatesFile = reader.URI().Path()
			label.SetText("Template: " + *templatesFile)
		}, w)
		fd.Resize(fyne.NewSize(800, 600))
		fd.SetFilter(storage.NewExtensionFileFilter([]string{constants.YamlFileFormat, constants.YmlFileFormat}))
		fd.Show()
	})
}

// newThreadsSelect creates a field for entering the number of threads
func newThreadsEntry(defaultThreads int) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(strconv.Itoa(defaultThreads))
	entry.Validator = func(s string) error {
		_, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("invalid number")
		}
		return nil
	}
	return entry
}

// newTimeoutEntry creates a field for entering the timeout value in seconds
func newTimeoutEntry() *widget.Entry {
	e := widget.NewEntry()
	e.SetText("1")
	return e
}

// initialStatsText returns a string with initial statistics values
func initialStatsText() string {
	return "Statistics:\nTargets loaded: 0\nProcessed: 0\nSuccesses: 0\nErrors: 0\nAvg time (ms): 0"
}

// handleStartButtonClick handles a click on the scan start button
func handleStartButtonClick(
	a fyne.App,
	w fyne.Window,
	targetsFile, templateFile string,
	threadsEntry *widget.Entry,
	timeoutEntry *widget.Entry,
	statsBinding binding.String,
	isRunning *atomic.Bool,
	startBtn, stopBtn *widget.Button,
	cancelScan *context.CancelFunc,
	logger *logging.Logger,
) {
	if isRunning.Load() {
		dialog.ShowInformation("Scanner running", "Scanner is already running", w)
		return
	}

	threads, err := strconv.Atoi(threadsEntry.Text)
	if err != nil || threads <= 0 {
		dialog.ShowError(fmt.Errorf("invalid thread count"), w)
		return
	}

	timeoutFloat, err := strconv.ParseFloat(timeoutEntry.Text, 64)
	if err != nil || timeoutFloat < 0 {
		dialog.ShowError(fmt.Errorf("invalid timeout"), w)
		return
	}

	if targetsFile == "" {
		dialog.ShowError(fmt.Errorf("targets file not selected"), w)
		return
	}
	if templateFile == "" {
		dialog.ShowError(fmt.Errorf("templates folder not selected"), w)
		return
	}
	template, err := templates.LoadTemplate(templateFile)
	if err != nil {
		logger.Error.Printf("failed to load template: %v", err)
		dialog.ShowError(fmt.Errorf("failed to load template: %w", err), w)
		return
	}

	isRunning.Store(true)
	startBtn.Disable()
	stopBtn.Enable()

	ctx, cancel := context.WithCancel(context.Background())
	*cancelScan = cancel

	statsUpdateCh := make(chan string, 10)
	go updateStatsBinding(statsBinding, statsUpdateCh)

	go runScan(ctx, targetsFile, threads, template, statsUpdateCh, a, isRunning, startBtn, stopBtn, logger)
}

// updateStatsBinding listens to the update channel and updates the statistics string binding
func updateStatsBinding(statsBinding binding.String, statsUpdateCh <-chan string) {
	for update := range statsUpdateCh {
		_ = statsBinding.Set(update)
	}
}

// runScan starts the scan cycle: read targets, apply templates, collect statistics
func runScan(
	ctx context.Context,
	targetsFile string,
	threads int,
	template *templates.Template,
	statsUpdateCh chan<- string,
	a fyne.App,
	isRunning *atomic.Bool,
	startBtn, stopBtn *widget.Button,
	logger *logging.Logger,
) {
	defer func() {
		close(statsUpdateCh)
		a.Driver().DoFromGoroutine(func() {
			isRunning.Store(false)
			startBtn.Enable()
			stopBtn.Disable()
		}, true)
	}()

	var totalTargets, processed, success, errors, totalDuration int64
	targetsChan := make(chan string, 1000)

	go feedTargets(ctx, targetsFile, targetsChan, &totalTargets)

	processFn := func(ctx context.Context, target string) error {
		startTime := time.Now()
		matched, err := templates.MatchTemplate(ctx, target,"", template, &templates.AdvancedSettingsChecker{}, logger)
		durationMs := time.Since(startTime).Milliseconds()

		atomic.AddInt64(&processed, 1)
		atomic.AddInt64(&totalDuration, durationMs)

		if err != nil {
			logger.Info.Printf("Error processing target %s: %v\n", target, err)
			atomic.AddInt64(&errors, 1)
			return err
		}

		if matched {
			atomic.AddInt64(&success, 1)
			return nil
		}

		atomic.AddInt64(&errors, 1)
		return fmt.Errorf("no match found")
	}

	resultsDone := scanner.StartWorkers(ctx, targetsChan, threads, processFn, logger)

	select {
	case <-ctx.Done():
		return
	case <-resultsDone:
	}

	statsUpdateCh <- "Scan finished.\n" + formatStats(totalTargets, processed, success, errors, totalDuration)
}

// feedTargets reads targets from the file and sends them to the channel for scanning
func feedTargets(ctx context.Context, targetsFile string, targetsChan chan<- string, totalTargets *int64) {
	defer close(targetsChan)

	file, err := os.Open(targetsFile)
	if err != nil {
		log.Printf("Error opening targets file %s: %v\n", targetsFile, err)
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
			target := strings.TrimSpace(scanner.Text())
			if target == "" {
				continue
			}
			targetsChan <- target
			atomic.AddInt64(totalTargets, 1)
		}
	}
}

// formatStats formats the collected statistics at the end of scanning
func formatStats(totalTargets, processed, success, errors, totalDuration int64) string {
	var avgMs int64
	if processed > 0 {
		avgMs = totalDuration / processed
	}
	return fmt.Sprintf(
		"Statistics:\nTargets loaded: %d\nProcessed: %d\nSuccesses: %d\nErrors: %d\nAvg time (ms): %d",
		totalTargets, processed, success, errors, avgMs,
	)
}


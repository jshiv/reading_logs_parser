package main

import (
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/fatih/color"
	"github.com/invopop/jsonschema"
)

// Build-time variables (set via -ldflags)
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

const progressFile = ".progress.json"

// ReadingLog represents the structured data extracted from a reading log image.
type ReadingLog struct {
	FullName        string         `json:"full_name" jsonschema:"description=The student full name as written on the form"`
	Grade           string         `json:"grade" jsonschema:"description=The student grade level (e.g. Kinder or 1st or 2nd)"`
	HomeroomTeacher string         `json:"homeroom_teacher" jsonschema:"description=The homeroom teacher name"`
	ReadingEntries  []ReadingEntry `json:"reading_entries" jsonschema:"description=Reading time entries for each day on the log"`
}

// ReadingEntry represents a single day's reading time.
type ReadingEntry struct {
	Day     string `json:"day" jsonschema:"description=Day of the week (e.g. Friday)"`
	Date    string `json:"date" jsonschema:"description=The date in M/D format (e.g. 1/30)"`
	Minutes int    `json:"minutes" jsonschema:"description=Number of minutes read as an integer. Use 0 if not filled in or blank."`
}

// Progress tracks which files have been processed and their results.
type Progress struct {
	Completed map[string]ReadingLog `json:"completed"`
	Errors    map[string]string     `json:"errors"`
}

// --- pretty printers ---------------------------------------------------

var (
	bold    = color.New(color.Bold)
	cyan    = color.New(color.FgCyan)
	green   = color.New(color.FgGreen)
	yellow  = color.New(color.FgYellow)
	red     = color.New(color.FgRed)
	dim     = color.New(color.Faint)
	boldGrn = color.New(color.Bold, color.FgGreen)
	boldCyn = color.New(color.Bold, color.FgCyan)
)

func printBanner() {
	boldCyn.Println("┌─────────────────────────────────────┐")
	boldCyn.Println("│     Reading Logs Parser              │")
	boldCyn.Println("└─────────────────────────────────────┘")
}

func printProgress(current, total, skipped int, filename string) {
	pct := float64(current) / float64(total) * 100
	bar := renderBar(current, total, 20)
	fmt.Printf("  %s %s %s %s\n",
		bold.Sprintf("[%d/%d]", current, total),
		cyan.Sprint(bar),
		dim.Sprintf("%.0f%%", pct),
		yellow.Sprint(filepath.Base(filename)),
	)
	if skipped > 0 {
		dim.Printf("  (%d already completed, skipped)\n", skipped)
	}
}

func renderBar(current, total, width int) string {
	filled := width * current / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return bar
}

func printResult(log *ReadingLog) {
	total := 0
	for _, e := range log.ReadingEntries {
		total += e.Minutes
	}
	green.Printf("  ✓ %s", log.FullName)
	dim.Printf(" | %s | %s\n", log.Grade, log.HomeroomTeacher)
	for _, entry := range log.ReadingEntries {
		if entry.Minutes > 0 {
			fmt.Printf("    %s %-10s %s\n",
				dim.Sprint("│"),
				dim.Sprintf("%s %s", entry.Day, entry.Date),
				green.Sprintf("%d min", entry.Minutes),
			)
		} else {
			fmt.Printf("    %s %-10s %s\n",
				dim.Sprint("│"),
				dim.Sprintf("%s %s", entry.Day, entry.Date),
				dim.Sprint("—"),
			)
		}
	}
	fmt.Printf("    %s %s\n",
		dim.Sprint("└"),
		boldGrn.Sprintf("Total: %d min", total),
	)
}

func printError(filename string, err error) {
	red.Printf("  ✗ %s: %v\n", filepath.Base(filename), err)
}

func printSummary(total, succeeded, failed, skipped int) {
	fmt.Println()
	bold.Println("─── Summary ──────────────────────────")
	fmt.Printf("  Images found:     %s\n", bold.Sprintf("%d", total))
	if skipped > 0 {
		fmt.Printf("  Already done:     %s\n", cyan.Sprintf("%d", skipped))
	}
	fmt.Printf("  Newly processed:  %s\n", green.Sprintf("%d", succeeded))
	if failed > 0 {
		fmt.Printf("  Failed:           %s\n", red.Sprintf("%d", failed))
	}
	bold.Println("──────────────────────────────────────")
}

// --- progress persistence -----------------------------------------------

func loadProgress() *Progress {
	p := &Progress{
		Completed: make(map[string]ReadingLog),
		Errors:    make(map[string]string),
	}
	data, err := os.ReadFile(progressFile)
	if err != nil {
		return p // no progress file yet, start fresh
	}
	if err := json.Unmarshal(data, p); err != nil {
		yellow.Printf("  Warning: could not parse %s, starting fresh\n", progressFile)
		return &Progress{
			Completed: make(map[string]ReadingLog),
			Errors:    make(map[string]string),
		}
	}
	return p
}

func saveProgress(p *Progress) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(progressFile, data, 0644)
}

// --- main ---------------------------------------------------------------

func main() {
	printBanner()

	// Find all image files in current directory
	images, err := findImages(".")
	if err != nil {
		red.Fprintf(os.Stderr, "Error finding images: %v\n", err)
		os.Exit(1)
	}

	if len(images) == 0 {
		yellow.Println("No image files found in current directory")
		os.Exit(1)
	}

	// Load existing progress
	progress := loadProgress()
	skipped := 0
	for _, img := range images {
		if _, done := progress.Completed[filepath.Base(img)]; done {
			skipped++
		}
	}

	if skipped > 0 {
		cyan.Printf("  Resuming: %d of %d already completed\n", skipped, len(images))
	}
	fmt.Printf("  %s images to process\n\n", bold.Sprintf("%d", len(images)-skipped))

	succeeded := 0
	failed := 0

	for i, imgPath := range images {
		baseName := filepath.Base(imgPath)

		// Skip already-completed files
		if _, done := progress.Completed[baseName]; done {
			continue
		}

		printProgress(i+1, len(images), skipped, imgPath)

		// Convert HEIC to JPEG if needed
		processPath := imgPath
		if isHEIC(imgPath) {
			jpgPath, err := convertHEICtoJPEG(imgPath)
			if err != nil {
				printError(imgPath, err)
				progress.Errors[baseName] = err.Error()
				saveProgress(progress)
				failed++
				continue
			}
			processPath = jpgPath
			defer os.Remove(jpgPath)
		}

		// Read and base64-encode the image
		mediaType, encoded, err := encodeImage(processPath)
		if err != nil {
			printError(imgPath, err)
			progress.Errors[baseName] = err.Error()
			saveProgress(progress)
			failed++
			continue
		}

		// Send to Claude and parse the structured output
		log, err := parseReadingLog(mediaType, encoded)
		if err != nil {
			printError(imgPath, err)
			progress.Errors[baseName] = err.Error()
			saveProgress(progress)
			failed++
			continue
		}

		// Save progress immediately after each success
		progress.Completed[baseName] = *log
		delete(progress.Errors, baseName) // clear any previous error for this file
		if err := saveProgress(progress); err != nil {
			red.Fprintf(os.Stderr, "  Warning: could not save progress: %v\n", err)
		}

		printResult(log)
		succeeded++
	}

	// Write all completed results (including previous runs) to CSV
	allLogs := make([]ReadingLog, 0, len(progress.Completed))
	for _, log := range progress.Completed {
		allLogs = append(allLogs, log)
	}

	if len(allLogs) == 0 {
		red.Println("No reading logs were successfully parsed")
		os.Exit(1)
	}

	if err := writeCSV("reading_logs.csv", allLogs); err != nil {
		red.Fprintf(os.Stderr, "Error writing CSV: %v\n", err)
		os.Exit(1)
	}

	printSummary(len(images), succeeded, failed, skipped)
	boldGrn.Printf("  Wrote %d reading log(s) to reading_logs.csv\n\n", len(allLogs))
}

// --- image handling -----------------------------------------------------

// findImages scans a directory for supported image files.
func findImages(dir string) ([]string, error) {
	var images []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	imageExts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true,
		".gif": true, ".webp": true, ".heic": true,
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if imageExts[ext] {
			images = append(images, filepath.Join(dir, e.Name()))
		}
	}
	return images, nil
}

// isHEIC returns true if the file has a .heic extension.
func isHEIC(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".heic"
}

// convertHEICtoJPEG uses macOS sips to convert a HEIC file to JPEG in a temp directory.
func convertHEICtoJPEG(heicPath string) (string, error) {
	tmpFile, err := os.CreateTemp("", "reading-log-*.jpg")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFile.Close()
	jpgPath := tmpFile.Name()

	cmd := exec.Command("sips", "-s", "format", "jpeg", heicPath, "--out", jpgPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(jpgPath)
		return "", fmt.Errorf("sips conversion failed: %w\n%s", err, string(output))
	}
	return jpgPath, nil
}

// encodeImage reads an image file and returns its media type and base64 encoding.
func encodeImage(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", "", err
	}

	ext := strings.ToLower(filepath.Ext(path))
	mediaTypes := map[string]string{
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".png":  "image/png",
		".gif":  "image/gif",
		".webp": "image/webp",
	}

	mediaType, ok := mediaTypes[ext]
	if !ok {
		return "", "", fmt.Errorf("unsupported image type: %s", ext)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	return mediaType, encoded, nil
}

// --- claude API ---------------------------------------------------------

// parseReadingLog sends an image to Claude and returns the structured reading log data.
func parseReadingLog(mediaType, encodedImage string) (*ReadingLog, error) {
	client := anthropic.NewClient()

	schemaMap := generateJSONSchema(&ReadingLog{})

	prompt := `Analyze this reading log image carefully. Extract the following information exactly as written:

1. The student's full name
2. The grade level
3. The homeroom teacher's name

Then for each day listed on the reading log (Friday 1/30, Saturday 1/31, Sunday 2/1, Monday 2/2, Tuesday 2/3, Wednesday 2/4, Thursday 2/5), extract the reading time as a number of minutes (integer only, e.g. if it says "10 min" or "10mi" return 10). If a day has no reading time filled in, use 0.

Return all information in the structured JSON format requested.`

	imageSource := anthropic.BetaBase64ImageSourceParam{
		Data:      encodedImage,
		MediaType: anthropic.BetaBase64ImageSourceMediaType(mediaType),
	}

	msg, err := client.Beta.Messages.New(context.TODO(), anthropic.BetaMessageNewParams{
		Model:     anthropic.ModelClaudeSonnet4_5_20250929,
		MaxTokens: 1024,
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(
				anthropic.NewBetaImageBlock(imageSource),
				anthropic.NewBetaTextBlock(prompt),
			),
		},
		OutputFormat: anthropic.BetaJSONSchemaOutputFormat(schemaMap),
		Betas:        []anthropic.AnthropicBeta{"structured-outputs-2025-11-13"},
	})
	if err != nil {
		return nil, fmt.Errorf("API call failed: %w", err)
	}

	// Parse the structured JSON from the response
	for _, block := range msg.Content {
		if textBlock, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			var log ReadingLog
			if err := json.Unmarshal([]byte(textBlock.Text), &log); err != nil {
				return nil, fmt.Errorf("failed to parse response JSON: %w\nraw: %s", err, textBlock.Text)
			}
			return &log, nil
		}
	}

	return nil, fmt.Errorf("no text content in API response")
}

// --- csv output ---------------------------------------------------------

// writeCSV writes the parsed reading logs to a CSV file.
func writeCSV(filename string, logs []ReadingLog) error {
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Header row
	header := []string{
		"Full Name", "Grade", "Homeroom Teacher",
		"Friday 1/30", "Saturday 1/31", "Sunday 2/1",
		"Monday 2/2", "Tuesday 2/3", "Wednesday 2/4", "Thursday 2/5",
		"Total Minutes",
	}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Data rows
	for _, log := range logs {
		total := 0
		for _, e := range log.ReadingEntries {
			total += e.Minutes
		}
		row := []string{
			log.FullName,
			log.Grade,
			log.HomeroomTeacher,
			formatMinutes(log.ReadingEntries, "1/30"),
			formatMinutes(log.ReadingEntries, "1/31"),
			formatMinutes(log.ReadingEntries, "2/1"),
			formatMinutes(log.ReadingEntries, "2/2"),
			formatMinutes(log.ReadingEntries, "2/3"),
			formatMinutes(log.ReadingEntries, "2/4"),
			formatMinutes(log.ReadingEntries, "2/5"),
			fmt.Sprintf("%d", total),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

// formatMinutes looks up the reading minutes for a given date and returns it as a string.
func formatMinutes(entries []ReadingEntry, date string) string {
	for _, e := range entries {
		if e.Date == date {
			if e.Minutes > 0 {
				return fmt.Sprintf("%d", e.Minutes)
			}
			return ""
		}
	}
	return ""
}

// --- json schema --------------------------------------------------------

// generateJSONSchema produces a JSON Schema map from a Go struct using invopop/jsonschema.
func generateJSONSchema(t any) map[string]any {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	schema := reflector.Reflect(t)
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		panic(err)
	}
	var schemaMap map[string]any
	if err := json.Unmarshal(schemaBytes, &schemaMap); err != nil {
		panic(err)
	}
	return schemaMap
}

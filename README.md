# Reading Logs Parser

A CLI tool that uses Claude's vision and structured outputs to extract data from photos of student reading logs and write the results to a CSV.

Built for the **TECA 2026 Read-a-Thon** weekly reading logs.

## What it does

1. Scans the current directory for image files (`.heic`, `.jpg`, `.jpeg`, `.png`, `.gif`, `.webp`)
2. Converts HEIC images to JPEG automatically (macOS `sips`)
3. Sends each image to **Claude Sonnet 4.5** via the Anthropic API
4. Uses structured outputs to extract:
   - Student full name
   - Grade
   - Homeroom teacher
   - Reading minutes for each day (Friday 1/30 – Thursday 2/5)
5. Writes all results to `reading_logs.csv` with a total minutes column

## Requirements

- **Go 1.23+**
- **macOS** (for HEIC → JPEG conversion via `sips`; not needed if images are already JPEG/PNG)
- An **Anthropic API key** set as an environment variable:
  ```bash
  export ANTHROPIC_API_KEY="sk-ant-..."
  ```

## Setup

```bash
git clone <repo-url>
cd reading-logs-parser
go build -o reading-logs-parser .
```

## Usage

Drop your reading log images into the directory and run:

```bash
./reading-logs-parser
```

Output:

```
┌─────────────────────────────────────┐
│     Reading Logs Parser              │
└─────────────────────────────────────┘
  1 images to process

  [1/1] ████████████████████ 100% IMG_0900.heic
  ✓ Flora Willoughby | Kinder | Alm
    │ Friday 1/30    10 min
    │ Saturday 1/31  10 min
    │ Sunday 2/1     10 min
    │ Monday 2/2     10 min
    │ Tuesday 2/3    —
    │ Wednesday 2/4  —
    │ Thursday 2/5   —
    └ Total: 40 min

─── Summary ──────────────────────────
  Images found:     1
  Newly processed:  1
──────────────────────────────────────
  Wrote 1 reading log(s) to reading_logs.csv
```

## Crash resilience

Progress is saved to `.progress.json` after each successfully parsed image. If the program crashes or is interrupted mid-batch:

- **Just re-run it** — already-completed images are skipped automatically
- To start completely fresh, delete the progress file:
  ```bash
  rm .progress.json
  ```

## Output format

`reading_logs.csv`:

| Full Name | Grade | Homeroom Teacher | Friday 1/30 | Saturday 1/31 | Sunday 2/1 | Monday 2/2 | Tuesday 2/3 | Wednesday 2/4 | Thursday 2/5 | Total Minutes |
|---|---|---|---|---|---|---|---|---|---|---|
| Flora Willoughby | Kinder | Alm | 10 | 10 | 10 | 10 | | | | 40 |

## Dependencies

- [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) — Anthropic API client
- [invopop/jsonschema](https://github.com/invopop/jsonschema) — JSON Schema generation for structured outputs
- [fatih/color](https://github.com/fatih/color) — Colored terminal output

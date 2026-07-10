package executor

import (
	"fmt"
	"strings"
	"testing"
)

func makeTestEntries(count int) []FileEntry {
	var entries []FileEntry
	for i := 0; i < count; i++ {
		entries = append(entries, FileEntry{
			Action:  "CREATE",
			Path:    fmt.Sprintf("src/service/handler_%d.go", i),
			Purpose: fmt.Sprintf("HTTP handler for endpoint /api/v%d/items", i),
			Spec:    fmt.Sprintf("Implement a REST handler that:\n- Accepts GET and POST requests\n- Validates input with the shared validator\n- Returns JSON responses with proper status codes\n- Uses the repository layer for data access\n- Logs all requests with correlation IDs\n- Handles errors with the standard error middleware"),
			Follows: "src/service/handler_example.go",
			Needs:   []string{"internal/validator", "internal/repository", "internal/middleware"},
		})
	}
	return entries
}

func makeKbBlock() string {
	return strings.Repeat("# Section 1: Architecture\nLayered architecture with handlers → services → repositories.\n\n", 5)
}

// BenchmarkBuildBatchPrompt measures prompt size with full vs compact mode.
func BenchmarkBuildBatchPrompt(b *testing.B) {
	allEntries := makeTestEntries(20)
	kbBlock := makeKbBlock()
	batchSize := 5

	for _, compact := range []bool{false, true} {
		label := "full"
		if compact {
			label = "compact"
		}
		b.Run(label, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				batch := allEntries[:batchSize]
				_ = buildBatchPrompt(allEntries, batch, kbBlock, compact)
			}
		})
	}
}

// TestBuildBatchPromptSizeDelta reports the actual byte savings from compact mode.
func TestBuildBatchPromptSizeDelta(t *testing.T) {
	kbBlock := makeKbBlock()

	tests := []struct {
		totalFiles int
		batchSize  int
	}{
		{10, 5},
		{20, 5},
		{30, 5},
		{50, 5},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d_files_%d_batch", tt.totalFiles, tt.batchSize), func(t *testing.T) {
			allEntries := makeTestEntries(tt.totalFiles)

			// Simulate 4 batches; batch 1 is full, batches 2-4 are compact.
			var fullSize, compactSize int
			for start := 0; start < len(allEntries); start += tt.batchSize {
				end := start + tt.batchSize
				if end > len(allEntries) {
					end = len(allEntries)
				}
				batch := allEntries[start:end]
				firstBatch := start == 0

				if firstBatch {
					p := buildBatchPrompt(allEntries, batch, kbBlock, false)
					fullSize = len(p)
				} else {
					p := buildBatchPrompt(allEntries, batch, kbBlock, true)
					compactSize += len(p)
				}
			}

			batches := (len(allEntries) + tt.batchSize - 1) / tt.batchSize
			if batches > 1 {
				avgFull := fullSize
				avgCompact := compactSize / (batches - 1)
				savings := float64(avgFull-avgCompact) / float64(avgFull) * 100
				t.Logf("full batch: %d bytes, avg compact: %d bytes, savings: %.1f%%",
					avgFull, avgCompact, savings)
			}
		})
	}
}

package channels

import (
	"fmt"
	"strings"
	"testing"
)

// makeTable builds a markdown table with the given number of rows (excluding header and separator).
// Each row is roughly 20 chars: "| col1 | col2 |"
func makeTable(label string, rows int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "| %s_h1 | %s_h2 |\n", label, label)
	b.WriteString("| --- | --- |\n")
	for i := 0; i < rows; i++ {
		fmt.Fprintf(&b, "| %s_r%d | val%d |\n", label, i, i)
	}
	return strings.TrimRight(b.String(), "\n")
}

func TestIsTableSeparatorLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want bool
	}{
		{"simple separator", "| --- | --- |", true},
		{"with alignment colons", "| :--- | :---: | ---: |", true},
		{"minimal separator", "| - |", true},
		{"long dashes", "| ---------- | ---------- |", true},
		{"with leading spaces", "  | --- | --- |", true},
		{"text content", "| hello | world |", false},
		{"mixed content and dashes", "| --- | hello |", false},
		{"no pipes", "--- --- ---", false},
		{"empty string", "", false},
		{"only pipes", "| | |", false},
		{"single pipe", "|", false},
		{"dashes without pipe separators", "|---|", true},
		{"colon only cell", "| : | --- |", false},
		{"header row with text", "| Name | Age |", false},
		{"empty cells between valid", "| --- | | --- |", false},
		{"no trailing pipe", "| --- | ---", true},
		{"no trailing pipe with alignment", "| :--- | :---:", true},
		{"no trailing pipe single cell", "| ---", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isTableSeparatorLine(tc.line)
			if got != tc.want {
				t.Errorf("isTableSeparatorLine(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestCountMarkdownTables(t *testing.T) {
	table1 := makeTable("t1", 2)
	table2 := makeTable("t2", 1)
	table3 := makeTable("t3", 3)

	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"empty content", "", 0},
		{"no tables", "Hello world\nsome text", 0},
		{"single table", table1, 1},
		{"two tables separated by text", table1 + "\n\nSome text\n\n" + table2, 2},
		{"three consecutive tables", table1 + "\n\n" + table2 + "\n\n" + table3, 3},
		{"separator line in code block is not a table", "```\n| --- | --- |\n```", 0},
		{"text with pipes but no separator", "| not | a | table |\n| also | not | one |", 0},
		{"table at very start", table1 + "\nSome trailing text", 1},
		{"table at very end", "Leading text\n" + table1, 1},
		{"table without trailing pipes", "| h1 | h2\n| --- | ---\n| v1 | v2", 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CountMarkdownTables(tc.content)
			if got != tc.want {
				t.Errorf("CountMarkdownTables() = %d, want %d\ncontent:\n%s", got, tc.want, tc.content)
			}
		})
	}
}

func TestSegmentContent(t *testing.T) {
	table := makeTable("t", 1)

	tests := []struct {
		name         string
		content      string
		wantSegments int
		checkContent func(t *testing.T, segments []segment)
	}{
		{
			name:         "plain text single segment",
			content:      "Hello world\nSecond line",
			wantSegments: 1,
		},
		{
			name:         "single table",
			content:      table,
			wantSegments: 1,
			checkContent: func(t *testing.T, segments []segment) {
				if !segments[0].hasTable {
					t.Error("expected segment to have hasTable=true")
				}
			},
		},
		{
			name:         "text then table then text",
			content:      "Before\n\n" + table + "\n\nAfter",
			wantSegments: 3,
			checkContent: func(t *testing.T, segments []segment) {
				if segments[0].hasTable {
					t.Error("first segment should not be table")
				}
				if !segments[1].hasTable {
					t.Error("middle segment should be table")
				}
				if segments[2].hasTable {
					t.Error("last segment should not be table")
				}
			},
		},
		{
			name:         "two tables with text between",
			content:      makeTable("a", 1) + "\n\nMiddle\n\n" + makeTable("b", 1),
			wantSegments: 3,
		},
		{
			name:         "empty content",
			content:      "",
			wantSegments: 0,
		},
		{
			name:         "consecutive tables separated by blank line",
			content:      makeTable("a", 1) + "\n\n" + makeTable("b", 1),
			wantSegments: 3, // table, blank line, table
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			segments := segmentContent(tc.content)
			if len(segments) != tc.wantSegments {
				t.Errorf("segmentContent() returned %d segments, want %d", len(segments), tc.wantSegments)
				for i, s := range segments {
					t.Logf("  segment[%d] hasTable=%v content=%q", i, s.hasTable, s.content)
				}
				return
			}
			if tc.checkContent != nil {
				tc.checkContent(t, segments)
			}

			// Verify no content loss: joined segments == original content
			var parts []string
			for _, s := range segments {
				parts = append(parts, s.content)
			}
			joined := strings.Join(parts, "")
			if joined != tc.content {
				t.Errorf("content loss after segmenting:\ngot:  %q\nwant: %q", joined, tc.content)
			}
		})
	}
}

func TestSplitGreedy(t *testing.T) {
	table := func(label string) string { return makeTable(label, 2) }

	tests := []struct {
		name         string
		content      string
		fits         func(string) bool
		wantChunks   int
		checkContent func(t *testing.T, chunks []string)
	}{
		{
			name:       "empty content",
			content:    "",
			fits:       func(s string) bool { return len(s) <= 100 },
			wantChunks: 0,
		},
		{
			name:       "content fits in one chunk",
			content:    "Hello world",
			fits:       func(s string) bool { return true },
			wantChunks: 1,
		},
		{
			name:    "split by table count limit of 2",
			content: table("a") + "\n\n" + table("b") + "\n\n" + table("c"),
			fits: func(s string) bool {
				return CountMarkdownTables(s) <= 2
			},
			wantChunks: 2,
			checkContent: func(t *testing.T, chunks []string) {
				for i, chunk := range chunks {
					if count := CountMarkdownTables(chunk); count > 2 {
						t.Errorf("chunk %d has %d tables, want <= 2", i, count)
					}
				}
			},
		},
		{
			name:    "split by table count limit of 1",
			content: table("a") + "\n\n" + table("b") + "\n\n" + table("c"),
			fits: func(s string) bool {
				return CountMarkdownTables(s) <= 1
			},
			wantChunks: 3,
		},
		{
			name:    "split by rune length",
			content: strings.Repeat("x", 300),
			fits: func(s string) bool {
				return len([]rune(s)) <= 100
			},
			wantChunks: 3,
			checkContent: func(t *testing.T, chunks []string) {
				for i, chunk := range chunks {
					if len([]rune(chunk)) > 100 {
						t.Errorf("chunk %d has %d runes, want <= 100", i, len([]rune(chunk)))
					}
				}
			},
		},
		{
			name:    "dual constraints: length and table count",
			content: strings.Repeat("x", 50) + "\n\n" + table("a") + "\n\n" + table("b") + "\n\n" + strings.Repeat("y", 50) + "\n\n" + table("c"),
			fits: func(s string) bool {
				return len([]rune(s)) <= 200 && CountMarkdownTables(s) <= 2
			},
			wantChunks: 2,
			checkContent: func(t *testing.T, chunks []string) {
				for i, chunk := range chunks {
					if len([]rune(chunk)) > 200 {
						t.Errorf("chunk %d exceeds length limit: %d runes", i, len([]rune(chunk)))
					}
					if count := CountMarkdownTables(chunk); count > 2 {
						t.Errorf("chunk %d has %d tables, want <= 2", i, count)
					}
				}
			},
		},
		{
			name:    "text before and after tables preserved",
			content: "Intro text\n\n" + table("a") + "\n\n" + table("b") + "\n\n" + table("c") + "\n\nOutro text",
			fits: func(s string) bool {
				return CountMarkdownTables(s) <= 2
			},
			wantChunks: 2,
			checkContent: func(t *testing.T, chunks []string) {
				// All original content should be present across chunks
				joined := strings.Join(chunks, "\n\n")
				if !strings.Contains(joined, "Intro text") {
					t.Error("missing 'Intro text'")
				}
				if !strings.Contains(joined, "Outro text") {
					t.Error("missing 'Outro text'")
				}
			},
		},
		{
			name:    "greedy: maximizes chunk length",
			content: table("a") + "\n\nText between\n\n" + table("b") + "\n\n" + table("c") + "\n\n" + table("d") + "\n\n" + table("e"),
			fits: func(s string) bool {
				return CountMarkdownTables(s) <= 3
			},
			wantChunks: 2,
			checkContent: func(t *testing.T, chunks []string) {
				// First chunk should greedily take 3 tables
				if count := CountMarkdownTables(chunks[0]); count != 3 {
					t.Errorf("first chunk should have 3 tables (greedy), got %d", count)
				}
				if count := CountMarkdownTables(chunks[1]); count != 2 {
					t.Errorf("second chunk should have 2 tables, got %d", count)
				}
			},
		},
		{
			name:       "exactly at limit: no split needed",
			content:    table("a") + "\n\n" + table("b"),
			fits:       func(s string) bool { return CountMarkdownTables(s) <= 2 },
			wantChunks: 1,
		},
		{
			name:    "five table feishu scenario",
			content: table("1") + "\n\n" + table("2") + "\n\n" + table("3") + "\n\n" + table("4") + "\n\n" + table("5") + "\n\n" + table("6") + "\n\n" + table("7"),
			fits: func(s string) bool {
				return CountMarkdownTables(s) <= 5
			},
			wantChunks: 2,
			checkContent: func(t *testing.T, chunks []string) {
				c0 := CountMarkdownTables(chunks[0])
				c1 := CountMarkdownTables(chunks[1])
				if c0 != 5 {
					t.Errorf("first chunk should have 5 tables, got %d", c0)
				}
				if c1 != 2 {
					t.Errorf("second chunk should have 2 tables, got %d", c1)
				}
			},
		},
		{
			name:    "line-level fallback for oversized text segment",
			content: "line1\nline2\nline3\nline4\nline5\nline6",
			fits: func(s string) bool {
				return len([]rune(s)) <= 18
			},
			wantChunks: 2,
			checkContent: func(t *testing.T, chunks []string) {
				for i, chunk := range chunks {
					if len([]rune(chunk)) > 18 {
						t.Errorf("chunk %d has %d runes, exceeds limit 18", i, len([]rune(chunk)))
					}
				}
			},
		},
		{
			name:       "fits always true returns single chunk",
			content:    "anything goes here\n" + table("big") + "\n" + strings.Repeat("z", 500),
			fits:       func(s string) bool { return true },
			wantChunks: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitGreedy(tc.content, tc.fits)

			if tc.wantChunks == 0 {
				if len(got) != 0 {
					t.Errorf("expected 0 chunks, got %d: %q", len(got), got)
				}
				return
			}

			if len(got) != tc.wantChunks {
				t.Errorf("expected %d chunks, got %d", tc.wantChunks, len(got))
				for i, c := range got {
					t.Logf("  chunk[%d] (%d runes, %d tables): %q", i, len([]rune(c)), CountMarkdownTables(c), c)
				}
				return
			}

			// Verify no empty chunks
			for i, chunk := range got {
				if strings.TrimSpace(chunk) == "" {
					t.Errorf("chunk %d is empty or whitespace-only", i)
				}
			}

			if tc.checkContent != nil {
				tc.checkContent(t, got)
			}
		})
	}
}

// TestSplitGreedy_ContentPreservation verifies that no content is lost during splitting.
func TestSplitGreedy_ContentPreservation(t *testing.T) {
	content := "Header\n\n" + makeTable("a", 2) + "\n\nMiddle text\n\n" + makeTable("b", 1) + "\n\nFooter"

	chunks := SplitGreedy(content, func(s string) bool {
		return CountMarkdownTables(s) <= 1
	})

	// Verify key content pieces are present across all chunks
	joined := strings.Join(chunks, "\n")
	for _, needle := range []string{"Header", "Middle text", "Footer", "a_h1", "b_h1"} {
		if !strings.Contains(joined, needle) {
			t.Errorf("content %q lost after splitting", needle)
		}
	}
}

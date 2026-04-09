package channels

import (
	"strings"
)

// segment represents a structural piece of content: either a table block or a text block.
type segment struct {
	content  string
	hasTable bool
}

// isTableSeparatorLine returns true if the line is a markdown table separator row,
// e.g. "| --- | :---: | ---: |".
func isTableSeparatorLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "|") {
		return false
	}
	cells := strings.Split(trimmed, "|")
	// Leading "|" produces an empty first element; skip it.
	// Trailing "|" is optional in GFM; if present it produces an empty last element.
	cells = cells[1:]
	if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" {
		cells = cells[:len(cells)-1]
	}
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if cell == "" || !strings.ContainsRune(cell, '-') {
			return false
		}
		for _, ch := range cell {
			if ch != '-' && ch != ':' {
				return false
			}
		}
	}
	return true
}

// CountMarkdownTables counts the number of markdown tables in content.
// A table is identified by a separator line (e.g. "| --- | --- |") that is
// NOT inside a fenced code block.
func CountMarkdownTables(content string) int {
	count := 0
	inCodeBlock := false
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}
		if !inCodeBlock && isTableSeparatorLine(line) {
			count++
		}
	}
	return count
}

// segmentContent splits content into structural segments (table blocks and text blocks).
// A table block is a contiguous run of lines starting with "|" that contains at least
// one separator line. Lines between table blocks form text segments.
// The concatenation of all segment contents equals the original content.
func segmentContent(content string) []segment {
	if content == "" {
		return nil
	}

	lines := strings.Split(content, "\n")
	var segments []segment
	chunkStart := 0
	inTableBlock := false
	hasSeparator := false

	flush := func(end int, asTable bool) {
		if end <= chunkStart {
			return
		}
		seg := strings.Join(lines[chunkStart:end], "\n")
		if end < len(lines) {
			seg += "\n"
		}
		segments = append(segments, segment{
			content:  seg,
			hasTable: asTable,
		})
		chunkStart = end
	}

	for i, line := range lines {
		isTableLine := strings.HasPrefix(strings.TrimSpace(line), "|")

		if isTableLine {
			if !inTableBlock {
				flush(i, false)
				inTableBlock = true
				hasSeparator = false
			}
			if isTableSeparatorLine(line) {
				hasSeparator = true
			}
		} else if inTableBlock {
			flush(i, hasSeparator)
			inTableBlock = false
			hasSeparator = false
		}
	}

	flush(len(lines), inTableBlock && hasSeparator)
	return segments
}

// SplitGreedy splits content into chunks where each chunk satisfies the fits predicate.
// It greedily maximizes chunk length by accumulating structural segments (tables and text
// blocks) until adding the next segment would violate the predicate. When a single text
// segment is too large to fit, it falls back to line-level splitting.
// Returns nil for empty content.
func SplitGreedy(content string, fits func(string) bool) []string {
	if content == "" {
		return nil
	}
	if fits(content) {
		return []string{content}
	}

	segments := segmentContent(content)
	var result []string
	var current strings.Builder

	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			result = append(result, s)
		}
		current.Reset()
	}

	for _, seg := range segments {
		candidate := current.String() + seg.content
		if current.Len() > 0 && !fits(strings.TrimSpace(candidate)) {
			flush()
			candidate = seg.content
		}

		if !fits(strings.TrimSpace(candidate)) {
			// Single segment too large — split by lines
			for _, line := range strings.SplitAfter(seg.content, "\n") {
				if line == "" {
					continue
				}
				if current.Len() > 0 && !fits(strings.TrimSpace(current.String()+line)) {
					flush()
				}
				// Single line still too large — split by runes
				if current.Len() == 0 && !fits(strings.TrimSpace(line)) {
					lineRunes := []rune(line)
					for len(lineRunes) > 0 {
						lo, hi := 1, len(lineRunes)
						for lo < hi {
							mid := (lo + hi + 1) / 2
							if fits(strings.TrimSpace(string(lineRunes[:mid]))) {
								lo = mid
							} else {
								hi = mid - 1
							}
						}
						result = append(result, strings.TrimSpace(string(lineRunes[:lo])))
						lineRunes = lineRunes[lo:]
					}
					continue
				}
				current.WriteString(line)
			}
		} else {
			current.WriteString(seg.content)
		}
	}

	flush()
	return result
}

// SplitMessage splits long messages into chunks, preserving code block integrity.
// The maxLen parameter is measured in runes (Unicode characters), not bytes.
// The function reserves a buffer (10% of maxLen, min 50) to leave room for closing code blocks,
// but may extend to maxLen when needed.
// Call SplitMessage with the full text content and the maximum allowed length of a single message;
// it returns a slice of message chunks that each respect maxLen and avoid splitting fenced code blocks.
func SplitMessage(content string, maxLen int) []string {
	if maxLen <= 0 {
		if content == "" {
			return nil
		}
		return []string{content}
	}

	runes := []rune(content)
	totalLen := len(runes)
	var messages []string

	// Dynamic buffer: 10% of maxLen, but at least 50 chars if possible
	codeBlockBuffer := max(maxLen/10, 50)
	if codeBlockBuffer > maxLen/2 {
		codeBlockBuffer = maxLen / 2
	}

	start := 0
	for start < totalLen {
		remaining := totalLen - start
		if remaining <= maxLen {
			messages = append(messages, string(runes[start:totalLen]))
			break
		}

		// Effective split point: maxLen minus buffer, to leave room for code blocks
		effectiveLimit := max(maxLen-codeBlockBuffer, maxLen/2)

		end := start + effectiveLimit

		// Find natural split point within the effective limit
		msgEnd := findLastNewlineInRange(runes, start, end, 200)
		if msgEnd <= start {
			msgEnd = findLastSpaceInRange(runes, start, end, 100)
		}
		if msgEnd <= start {
			msgEnd = end
		}

		// Check if this would end with an incomplete code block
		unclosedIdx := findLastUnclosedCodeBlockInRange(runes, start, msgEnd)

		if unclosedIdx >= 0 {
			// Message would end with incomplete code block
			// Try to extend up to maxLen to include the closing ```
			if totalLen > msgEnd {
				closingIdx := findNextClosingCodeBlockInRange(runes, msgEnd, totalLen)
				if closingIdx > 0 && closingIdx-start <= maxLen {
					// Extend to include the closing ```
					msgEnd = closingIdx
				} else {
					// Code block is too long to fit in one chunk or missing closing fence.
					// Try to split inside by injecting closing and reopening fences.
					headerEnd := findNewlineFrom(runes, unclosedIdx)
					var header string
					if headerEnd == -1 {
						header = strings.TrimSpace(string(runes[unclosedIdx : unclosedIdx+3]))
					} else {
						header = strings.TrimSpace(string(runes[unclosedIdx:headerEnd]))
					}
					headerEndIdx := unclosedIdx + len([]rune(header))
					if headerEnd != -1 {
						headerEndIdx = headerEnd
					}

					// If we have a reasonable amount of content after the header, split inside
					if msgEnd > headerEndIdx+20 {
						// Find a better split point closer to maxLen
						innerLimit := min(
							// Leave room for "\n```"
							start+maxLen-5, totalLen)
						betterEnd := findLastNewlineInRange(runes, start, innerLimit, 200)
						if betterEnd > headerEndIdx {
							msgEnd = betterEnd
						} else {
							msgEnd = innerLimit
						}
						chunk := strings.TrimRight(string(runes[start:msgEnd]), " \t\n\r") + "\n```"
						messages = append(messages, chunk)
						remaining := strings.TrimSpace(header + "\n" + string(runes[msgEnd:totalLen]))
						// Replace the tail of runes with the reconstructed remaining
						runes = []rune(remaining)
						totalLen = len(runes)
						start = 0
						continue
					}

					// Otherwise, try to split before the code block starts
					newEnd := findLastNewlineInRange(runes, start, unclosedIdx, 200)
					if newEnd <= start {
						newEnd = findLastSpaceInRange(runes, start, unclosedIdx, 100)
					}
					if newEnd > start {
						msgEnd = newEnd
					} else {
						// If we can't split before, we MUST split inside (last resort)
						if unclosedIdx-start > 20 {
							msgEnd = unclosedIdx
						} else {
							splitAt := min(start+maxLen-5, totalLen)
							chunk := strings.TrimRight(string(runes[start:splitAt]), " \t\n\r") + "\n```"
							messages = append(messages, chunk)
							remaining := strings.TrimSpace(header + "\n" + string(runes[splitAt:totalLen]))
							runes = []rune(remaining)
							totalLen = len(runes)
							start = 0
							continue
						}
					}
				}
			}
		}

		if msgEnd <= start {
			msgEnd = start + effectiveLimit
		}

		messages = append(messages, string(runes[start:msgEnd]))
		// Advance start, skipping leading whitespace of next chunk
		start = msgEnd
		for start < totalLen && (runes[start] == ' ' || runes[start] == '\t' || runes[start] == '\n' || runes[start] == '\r') {
			start++
		}
	}

	return messages
}

// findLastUnclosedCodeBlockInRange finds the last opening ``` that doesn't have a closing ```
// within runes[start:end]. Returns the absolute rune index or -1.
func findLastUnclosedCodeBlockInRange(runes []rune, start, end int) int {
	inCodeBlock := false
	lastOpenIdx := -1

	for i := start; i < end; i++ {
		if i+2 < end && runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			if !inCodeBlock {
				lastOpenIdx = i
			}
			inCodeBlock = !inCodeBlock
			i += 2
		}
	}

	if inCodeBlock {
		return lastOpenIdx
	}
	return -1
}

// findNextClosingCodeBlockInRange finds the next closing ``` starting from startIdx
// within runes[startIdx:end]. Returns the absolute index after the closing ``` or -1.
func findNextClosingCodeBlockInRange(runes []rune, startIdx, end int) int {
	for i := startIdx; i < end; i++ {
		if i+2 < end && runes[i] == '`' && runes[i+1] == '`' && runes[i+2] == '`' {
			return i + 3
		}
	}
	return -1
}

// findNewlineFrom finds the first newline character starting from the given index.
// Returns the absolute index or -1 if not found.
func findNewlineFrom(runes []rune, from int) int {
	for i := from; i < len(runes); i++ {
		if runes[i] == '\n' {
			return i
		}
	}
	return -1
}

// findLastNewlineInRange finds the last newline within the last searchWindow runes
// of the range runes[start:end]. Returns the absolute index or start-1 (indicating not found).
func findLastNewlineInRange(runes []rune, start, end, searchWindow int) int {
	searchStart := max(end-searchWindow, start)
	for i := end - 1; i >= searchStart; i-- {
		if runes[i] == '\n' {
			return i
		}
	}
	return start - 1
}

// findLastSpaceInRange finds the last space/tab within the last searchWindow runes
// of the range runes[start:end]. Returns the absolute index or start-1 (indicating not found).
func findLastSpaceInRange(runes []rune, start, end, searchWindow int) int {
	searchStart := max(end-searchWindow, start)
	for i := end - 1; i >= searchStart; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			return i
		}
	}
	return start - 1
}

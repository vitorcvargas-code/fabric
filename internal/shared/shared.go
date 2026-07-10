package shared

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PromptSep separates sections in LLM prompts.
const PromptSep = "\n\n---\n\n"

// ReadFile reads a file relative to root, returning an error string if missing.
func ReadFile(root, path string) string {
	content, err := os.ReadFile(filepath.Join(root, path))
	if err != nil {
		return fmt.Sprintf("(unavailable: %v)", err)
	}
	return string(content)
}

// TrimLines truncates s to at most n lines, appending a truncation note.
func TrimLines(s string, n int) string {
	if strings.Count(s, "\n")+1 <= n {
		return s
	}
	lines := strings.Split(s, "\n")
	return strings.Join(lines[:n], "\n") + "\n// ... (truncated)"
}

// TrimLinesHeadTail returns the first head lines and last tail lines,
// separated by a truncation marker when the file exceeds head+tail lines.
func TrimLinesHeadTail(s string, head, tail int) string {
	total := strings.Count(s, "\n") + 1
	if total <= head+tail {
		return s
	}
	lines := strings.Split(s, "\n")
	headPart := strings.Join(lines[:head], "\n")
	tailPart := strings.Join(lines[total-tail:], "\n")
	return headPart + fmt.Sprintf("\n\n// ... %d lines omitted ...\n\n", total-head-tail) + tailPart
}

// ReadLinesAround extracts a line number from a compiler token like "file.kt:183:141"
// and returns context lines around it from the file. radius is the number of lines
// above and below the error line. Returns empty string if no line number is found.
func ReadLinesAround(root, token string, radius int) string {
	// Extract path: strip line:col suffix and URI scheme
	clean := LineColRe.ReplaceAllString(strings.Trim(token, `"'(:;)`), "")
	if idx := strings.Index(clean, "://"); idx >= 0 && idx <= 6 {
		clean = clean[idx+3:]
	}
	content := ReadFile(root, clean)
	if strings.HasPrefix(content, "(unavailable:") {
		return ""
	}

	// Extract line number from the original token
	lineMatch := LineColRe.FindString(strings.Trim(token, `"'(:;)`))
	if lineMatch == "" {
		return ""
	}
	lineMatch = strings.TrimPrefix(lineMatch, ":")
	lineStr := strings.Split(lineMatch, ":")[0]
	lineNum, err := strconv.Atoi(lineStr)
	if err != nil || lineNum < 1 {
		return ""
	}

	lines := strings.Split(content, "\n")
	if lineNum > len(lines) {
		return ""
	}
	start := lineNum - 1 - radius
	if start < 0 {
		start = 0
	}
	end := lineNum + radius
	if end > len(lines) {
		end = len(lines)
	}
	return fmt.Sprintf("(line %d):\n%s", lineNum, strings.Join(lines[start:end], "\n"))
}

// FenceFor returns a markdown fence string appropriate for the file extension.
func FenceFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sql":
		return "```sql"
	case ".yaml", ".yml":
		return "```yaml"
	case ".json":
		return "```json"
	case ".properties":
		return "```"
	default:
		return "```"
	}
}

// closeFenceRe matches a closing ``` fence on its own line.  In multiline
// mode, ^ and $ anchor to line boundaries, so content after the fence (e.g.
// analysis summaries) does not prevent the match.
var closeFenceRe = regexp.MustCompile("(?m)^```[ \t]*$")

// ExtractMarkdownJSON strips markdown fences and extracts the JSON object from a raw LLM response.
func ExtractMarkdownJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "```json"); idx >= 0 {
		raw = raw[idx+7:]
	} else if idx := strings.Index(raw, "```"); idx >= 0 {
		raw = raw[idx+3:]
	}
	if loc := closeFenceRe.FindStringIndex(raw); loc != nil {
		raw = raw[:loc[0]]
	}
	if idx := strings.Index(raw, "{"); idx >= 0 {
		raw = raw[idx:]
	}
	if last := strings.LastIndex(raw, "}"); last >= 0 {
		raw = raw[:last+1]
	}
	// Fix invalid \$ JSON escapes without corrupting valid \\$ sequences.
	// \\$ is valid JSON (escaped backslash + literal $); \$ is invalid.
	// Use a sentinel to protect \\$ before replacing \$.
	const sentinel = "\x00FS"
	raw = strings.ReplaceAll(raw, `\\$`, sentinel)
	raw = strings.ReplaceAll(raw, `\$`, `$`)
	raw = strings.ReplaceAll(raw, sentinel, `\\$`)
	return strings.TrimSpace(raw)
}

var (
	fileHeaderRe  = regexp.MustCompile(`(?m)^===\s*(?:FILE:\s*)?(.+?)\s*(?:===\s*)?$`)
	fenceSplitRe  = regexp.MustCompile("(?m)^```")
	kbSectionRe   = regexp.MustCompile(`(?m)^## (\d+)\. `)
	LineColRe     = regexp.MustCompile(`:\d+[:\d]*$`)
	langTagRe     = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+#-]*$`)
)

// IsNonSourcePath returns true for paths that contain build artifact or
// dependency directories that should never be written by fabric.
func IsNonSourcePath(path string) bool {
	for _, seg := range strings.Split(path, "/") {
		switch seg {
		case "build", ".gradle", "node_modules", ".git":
			return true
		}
	}
	return false
}

// ResolveFilePaths scans build output for tokens that look like file paths
// and returns those that exist on disk. Handles both relative paths (joined with
// root) and absolute paths (from file:/// or / prefix). Strips line:column
// suffixes (universal across all compilers).
func ResolveFilePaths(root, output string) []string {
	seen := map[string]bool{}
	var paths []string
	for _, token := range strings.Fields(output) {
		// Strip common wrapping characters: quotes, parens, semicolons
		clean := strings.Trim(token, `"'(:;)`)
		// Strip line:column or line:col:anything suffix
		clean = LineColRe.ReplaceAllString(clean, "")
		// Strip URI scheme prefix if present: file:///path, s3://bucket/key, etc.
		if idx := strings.Index(clean, "://"); idx >= 0 && idx <= 6 {
			clean = clean[idx+3:]
		}
		if clean == "" || clean == "." || clean == ".." {
			continue
		}
		// Try as-is first (absolute path), then relative to repo root.
		if _, err := os.Stat(clean); err == nil && !seen[clean] {
			seen[clean] = true
			paths = append(paths, clean)
		} else if _, err := os.Stat(filepath.Join(root, clean)); err == nil && !seen[clean] {
			seen[clean] = true
			paths = append(paths, clean)
		}
	}
	return paths
}


// FilterKBSections extracts only matching section numbers from the KB.
// Section numbers are matched by prefix (e.g. "2" matches "2", "2.1", "2.2").
// Quick Reference (section 1) is always included.
func FilterKBSections(kb []byte, sections ...string) []byte {
	raw := string(kb)
	idxs := kbSectionRe.FindAllStringSubmatchIndex(raw, -1)
	if len(idxs) == 0 {
		return kb
	}

	want := map[string]bool{"1": true} // quick reference always included
	for _, s := range sections {
		want[s] = true
	}

	var sb strings.Builder
	for i, m := range idxs {
		secNum := raw[m[2]:m[3]]
		if !wantSection(want, secNum) {
			continue
		}

		start := m[0] // start of "## N."
		var end int
		if i+1 < len(idxs) {
			end = idxs[i+1][0] // start of next section
		} else {
			end = len(raw)
		}
		sb.WriteString(raw[start:end])
		sb.WriteString("\n")
	}

	if sb.Len() == 0 {
		return kb // nothing matched; use full KB
	}
	return []byte(sb.String())
}

func wantSection(want map[string]bool, num string) bool {
	if want[num] {
		return true
	}
	// Also match sub-sections: "2.1" matches if "2" is wanted.
	if dot := strings.Index(num, "."); dot >= 0 {
		return want[num[:dot]]
	}
	return false
}

// ParseMultiFile splits a raw LLM response delimited by === FILE: path === markers.
func ParseMultiFile(raw string) map[string]string {
	files := map[string]string{}
	idxs := fileHeaderRe.FindAllStringIndex(raw, -1)
	matches := fileHeaderRe.FindAllStringSubmatch(raw, -1)
	for i, idx := range idxs {
		path := strings.TrimSpace(matches[i][1])
		start := idx[1]
		var end int
		if i+1 < len(idxs) {
			end = idxs[i+1][0]
		} else {
			end = len(raw)
		}
		content := stripFences(strings.TrimSpace(raw[start:end]))
		if content != "" {
			files[path] = content
		}
	}
	return files
}

func stripFences(s string) string {
	// Split on ``` at start of line — content between first
	// and last fence is the actual file; everything after the
	// closing fence (commentary, summaries) is discarded.
	parts := fenceSplitRe.Split(s, -1)
	// Need at least opening fence, content, closing fence.
	if len(parts) >= 3 {
		// Content is everything between first and last fence.
		inner := strings.Join(parts[1:len(parts)-1], "\n```")
		return strings.TrimSpace(stripLangTag(inner))
	}
	// Single fence pair: content is between opening and closing.
	if len(parts) == 2 {
		return strings.TrimSpace(stripLangTag(parts[1]))
	}
	return strings.TrimSpace(s)
}

// stripLangTag removes a language identifier (e.g. "kotlin", "java", "sql")
// that follows an opening ``` fence. The regex split leaves it as the first
// line of the content block.
func stripLangTag(s string) string {
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s
	}
	firstLine := strings.TrimSpace(s[:nl])
	if firstLine == "" {
		return s
	}
	if langTagRe.MatchString(firstLine) {
		return s[nl+1:]
	}
	return s
}

// DeduplicateTestFailures collapses identical exception chains in filtered build
// output. Consecutive indented lines (stack traces) following a non-indented
// header are treated as one block. Blocks with identical trace content are
// collapsed into a single instance with an occurrence count.
func DeduplicateTestFailures(output string) string {
	lines := strings.Split(output, "\n")

	// traceIndentRe matches stack trace lines: indented, or starting with
	// "Caused by:", "at ", or suppressed/circular reference markers.
	traceIndentRe := regexp.MustCompile(`^[\t ]`)

	type block struct {
		header string
		trace  []string
		idx    int // line index of header
	}

	var blocks []block
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		// Skip separators, blanks, and already-trace lines.
		if trimmed == "" || trimmed == "---" || traceIndentRe.MatchString(line) {
			i++
			continue
		}
		// This non-indented line is a potential header.
		header := line
		headerIdx := i
		var trace []string
		i++
		for i < len(lines) {
			next := lines[i]
			nextTrimmed := strings.TrimSpace(next)
			if nextTrimmed == "" || nextTrimmed == "---" {
				break
			}
			if traceIndentRe.MatchString(next) {
				trace = append(trace, next)
				i++
			} else {
				break // next non-indented line — new header
			}
		}
		if len(trace) > 0 {
			blocks = append(blocks, block{header: header, trace: trace, idx: headerIdx})
		}
		// If no trace was consumed, i is already at headerIdx+1.
	}

	if len(blocks) <= 1 {
		return output
	}

	// Group by trace signature.
	type group struct {
		blocks []block
	}
	groups := map[string]*group{}
	var order []string
	for _, b := range blocks {
		key := strings.Join(b.trace, "\n")
		if _, ok := groups[key]; !ok {
			order = append(order, key)
			groups[key] = &group{}
		}
		groups[key].blocks = append(groups[key].blocks, b)
	}

	// Collapse groups with >1 member.
	skip := map[int]bool{}
	replace := map[int]string{}
	for _, key := range order {
		g := groups[key]
		if len(g.blocks) <= 1 {
			continue
		}
		var names []string
		for _, b := range g.blocks {
			names = append(names, strings.TrimSpace(b.header))
		}
		listed := names
		if len(listed) > 5 {
			listed = append(listed[:5], fmt.Sprintf("... +%d more", len(names)-5))
		}
		first := g.blocks[0]
		replace[first.idx] = first.header + fmt.Sprintf("  (%d occurrences: %s)", len(g.blocks), strings.Join(listed, ", "))
		for _, b := range g.blocks[1:] {
			skip[b.idx] = true
			for j := b.idx + 1; j <= b.idx+len(b.trace); j++ {
				skip[j] = true
			}
		}
	}

	var result []string
	for i, line := range lines {
		if skip[i] {
			continue
		}
		if rep, ok := replace[i]; ok {
			result = append(result, rep)
			continue
		}
		result = append(result, line)
	}

	cleaned := cleanConsecutiveSeparators(result)
	out := strings.Join(cleaned, "\n")
	if len(out) > 10000 {
		out = "...(truncated)\n" + out[len(out)-10000:]
	}
	return out
}

func cleanConsecutiveSeparators(lines []string) []string {
	var out []string
	for i, line := range lines {
		if strings.TrimSpace(line) == "---" {
			if i > 0 && strings.TrimSpace(lines[i-1]) == "---" {
				continue
			}
			if i == 0 || i == len(lines)-1 {
				continue
			}
		}
		out = append(out, line)
	}
	return out
}

// IsTestPath returns true for file paths under a test directory or with test
// file naming conventions (language-agnostic).
func IsTestPath(path string) bool {
	if strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") {
		return true
	}
	name := filepath.Base(path)
	return strings.Contains(name, "_test.") || strings.HasPrefix(name, "test_")
}

// ExtractErrorLines filters build output using grep-style patterns with ±5 context lines.
// Each filter is compiled as a regex. Lines matching any filter are kept with surrounding
// context. When no filters match, the last 5000 chars of raw output are returned.
func ExtractErrorLines(output string, filters []string) string {
	rawLines := strings.Split(output, "\n")
	match := make([]bool, len(rawLines))

	hasMatch := false
	for _, f := range filters {
		if strings.TrimSpace(f) == "" {
			continue
		}
		re, err := regexp.Compile(f)
		if err != nil {
			re = regexp.MustCompile(regexp.QuoteMeta(f))
		}
		for i, line := range rawLines {
			if re.MatchString(line) {
				match[i] = true
				hasMatch = true
			}
		}
	}
	if !hasMatch {
		if len(output) > 5000 {
			return "...(truncated)\n" + output[len(output)-5000:]
		}
		return output
	}

	expanded := make([]bool, len(rawLines))
	const ctx = 5
	for i := range match {
		if !match[i] {
			continue
		}
		start := i - ctx
		if start < 0 {
			start = 0
		}
		end := i + ctx
		if end >= len(rawLines) {
			end = len(rawLines) - 1
		}
		for j := start; j <= end; j++ {
			expanded[j] = true
		}
	}

	var result []string
	last := -2
	for i, line := range rawLines {
		if !expanded[i] {
			continue
		}
		if i > last+1 && len(result) > 0 {
			result = append(result, "---")
		}
		result = append(result, line)
		last = i
	}

	out := strings.Join(result, "\n")
	if len(out) > 10000 {
		out = "...(truncated)\n" + out[len(out)-10000:]
	}
	return out
}

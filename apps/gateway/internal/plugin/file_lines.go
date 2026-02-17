package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const fileLinesToolMaxRange = 400

var (
	ErrFileLinesToolPathMissing    = errors.New("file_lines_tool_path_missing")
	ErrFileLinesToolPathInvalid    = errors.New("file_lines_tool_path_invalid")
	ErrFileLinesToolItemsInvalid   = errors.New("file_lines_tool_items_invalid")
	ErrFileLinesToolStartInvalid   = errors.New("file_lines_tool_start_invalid")
	ErrFileLinesToolEndInvalid     = errors.New("file_lines_tool_end_invalid")
	ErrFileLinesToolRangeInvalid   = errors.New("file_lines_tool_range_invalid")
	ErrFileLinesToolRangeTooLarge  = errors.New("file_lines_tool_range_too_large")
	ErrFileLinesToolContentMissing = errors.New("file_lines_tool_content_missing")
	ErrFileLinesToolOutOfRange     = errors.New("file_lines_tool_out_of_range")
	ErrFileLinesToolFileNotFound   = errors.New("file_lines_tool_file_not_found")
	ErrFileLinesToolFileRead       = errors.New("file_lines_tool_file_read_failed")
	ErrFileLinesToolFileWrite      = errors.New("file_lines_tool_file_write_failed")
)

type ViewFileLinesTool struct {
}

func NewViewFileLinesTool(_ string) *ViewFileLinesTool {
	return &ViewFileLinesTool{}
}

func (t *ViewFileLinesTool) Name() string {
	return "view"
}

func (t *ViewFileLinesTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	items, err := parseInvocationItems(input, true)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		viewResult, viewErr := t.viewOne(item)
		if viewErr != nil {
			return nil, viewErr
		}
		results = append(results, viewResult)
	}
	if len(results) == 1 {
		return results[0], nil
	}

	textBlocks := make([]string, 0, len(results))
	for _, item := range results {
		if text, ok := item["text"].(string); ok {
			textBlocks = append(textBlocks, text)
		}
	}
	return map[string]interface{}{
		"ok":      true,
		"count":   len(results),
		"results": results,
		"text":    strings.Join(textBlocks, "\n\n"),
	}, nil
}

type EditFileLinesTool struct {
}

func NewEditFileLinesTool(_ string) *EditFileLinesTool {
	return &EditFileLinesTool{}
}

func (t *EditFileLinesTool) Name() string {
	return "edit"
}

func (t *EditFileLinesTool) Invoke(input map[string]interface{}) (map[string]interface{}, error) {
	items, err := parseInvocationItems(input, true)
	if err != nil {
		return nil, err
	}
	results := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		editResult, editErr := t.editOne(item)
		if editErr != nil {
			return nil, editErr
		}
		results = append(results, editResult)
	}
	if len(results) == 1 {
		return results[0], nil
	}

	textBlocks := make([]string, 0, len(results))
	for _, item := range results {
		if text, ok := item["text"].(string); ok {
			textBlocks = append(textBlocks, text)
		}
	}
	return map[string]interface{}{
		"ok":      true,
		"count":   len(results),
		"results": results,
		"text":    strings.Join(textBlocks, "\n"),
	}, nil
}

func (t *ViewFileLinesTool) viewOne(input map[string]interface{}) (map[string]interface{}, error) {
	relPath, absPath, err := resolveFileLinesPath(input)
	if err != nil {
		return nil, err
	}
	start, end, err := parseLineRange(input)
	if err != nil {
		return nil, err
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileLinesToolFileNotFound, relPath)
		}
		return nil, fmt.Errorf("%w: %v", ErrFileLinesToolFileRead, err)
	}
	lines, _ := splitFileLines(string(raw))
	total := len(lines)
	if total == 0 {
		return map[string]interface{}{
			"ok":          true,
			"path":        relPath,
			"start":       0,
			"end":         0,
			"total_lines": 0,
			"content":     "",
			"text": fmt.Sprintf(
				"view %s [empty] (fallback from requested [%d-%d], total=0)",
				relPath,
				start,
				end,
			),
		}, nil
	}
	actualStart := start
	actualEnd := end
	fallbackToFull := false
	if start > total || end > total {
		actualStart = 1
		actualEnd = total
		fallbackToFull = true
	}

	selected := lines[actualStart-1 : actualEnd]
	content := strings.Join(selected, "\n")
	numbered := make([]string, 0, len(selected))
	for idx, line := range selected {
		lineNo := actualStart + idx
		numbered = append(numbered, fmt.Sprintf("%d: %s", lineNo, line))
	}
	text := fmt.Sprintf("view %s [%d-%d]\n%s", relPath, actualStart, actualEnd, strings.Join(numbered, "\n"))
	if fallbackToFull {
		text = fmt.Sprintf(
			"view %s [%d-%d] (fallback from requested [%d-%d], total=%d)\n%s",
			relPath,
			actualStart,
			actualEnd,
			start,
			end,
			total,
			strings.Join(numbered, "\n"),
		)
	}
	return map[string]interface{}{
		"ok":          true,
		"path":        relPath,
		"start":       actualStart,
		"end":         actualEnd,
		"total_lines": total,
		"content":     content,
		"text":        text,
	}, nil
}

func (t *EditFileLinesTool) editOne(input map[string]interface{}) (map[string]interface{}, error) {
	relPath, absPath, err := resolveFileLinesPath(input)
	if err != nil {
		return nil, err
	}
	start, end, err := parseLineRange(input)
	if err != nil {
		return nil, err
	}
	contentRaw, ok := input["content"]
	if !ok {
		return nil, ErrFileLinesToolContentMissing
	}
	content, ok := contentRaw.(string)
	if !ok {
		return nil, ErrFileLinesToolContentMissing
	}

	raw, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrFileLinesToolFileNotFound, relPath)
		}
		return nil, fmt.Errorf("%w: %v", ErrFileLinesToolFileRead, err)
	}
	lines, hadTrailingNewline := splitFileLines(string(raw))
	total := len(lines)
	if total == 0 || start > total || end > total {
		return nil, fmt.Errorf("%w: path=%s total=%d range=%d-%d", ErrFileLinesToolOutOfRange, relPath, total, start, end)
	}

	replLines, _ := splitFileLines(content)
	updatedLines := make([]string, 0, len(lines)-((end-start)+1)+len(replLines))
	updatedLines = append(updatedLines, lines[:start-1]...)
	updatedLines = append(updatedLines, replLines...)
	updatedLines = append(updatedLines, lines[end:]...)

	output := strings.Join(updatedLines, "\n")
	if hadTrailingNewline && len(updatedLines) > 0 {
		output += "\n"
	}

	perm := os.FileMode(0o644)
	if info, statErr := os.Stat(absPath); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := os.WriteFile(absPath, []byte(output), perm); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFileLinesToolFileWrite, err)
	}

	changed := end - start + 1
	text := fmt.Sprintf("edit %s [%d-%d] replaced %d line(s).", relPath, start, end, changed)
	return map[string]interface{}{
		"ok":                true,
		"path":              relPath,
		"start":             start,
		"end":               end,
		"replaced_lines":    changed,
		"inserted_lines":    len(replLines),
		"total_lines_after": len(updatedLines),
		"text":              text,
	}, nil
}

func parseLineRange(input map[string]interface{}) (int, int, error) {
	startRaw := input["start"]
	if startRaw == nil {
		startRaw = input["start_line"]
	}
	start, err := parseLineNumber(startRaw, ErrFileLinesToolStartInvalid)
	if err != nil {
		return 0, 0, err
	}
	endRaw := input["end"]
	if endRaw == nil {
		endRaw = input["end_line"]
	}
	end, err := parseLineNumber(endRaw, ErrFileLinesToolEndInvalid)
	if err != nil {
		return 0, 0, err
	}
	if start > end {
		return 0, 0, ErrFileLinesToolRangeInvalid
	}
	if end-start+1 > fileLinesToolMaxRange {
		return 0, 0, ErrFileLinesToolRangeTooLarge
	}
	return start, end, nil
}

func parseLineNumber(raw interface{}, fallback error) (int, error) {
	switch value := raw.(type) {
	case float64:
		line := int(value)
		if float64(line) == value && line >= 1 {
			return line, nil
		}
	case int:
		if value >= 1 {
			return value, nil
		}
	case int64:
		if value >= 1 {
			return int(value), nil
		}
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil && parsed >= 1 {
			return parsed, nil
		}
	}
	return 0, fallback
}

func resolveFileLinesPath(input map[string]interface{}) (string, string, error) {
	path := strings.TrimSpace(stringValue(input["path"]))
	if path == "" {
		return "", "", ErrFileLinesToolPathMissing
	}
	absPath, err := normalizeAbsolutePath(path)
	if err != nil {
		return "", "", err
	}
	return absPath, absPath, nil
}

func normalizeAbsolutePath(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", ErrFileLinesToolPathMissing
	}
	if !filepath.IsAbs(candidate) {
		return "", ErrFileLinesToolPathInvalid
	}
	return filepath.Clean(candidate), nil
}

func splitFileLines(content string) ([]string, bool) {
	if content == "" {
		return []string{}, false
	}
	lines := strings.Split(content, "\n")
	hadTrailingNewline := len(lines) > 0 && lines[len(lines)-1] == ""
	if hadTrailingNewline {
		lines = lines[:len(lines)-1]
	}
	return lines, hadTrailingNewline
}

func parseInvocationItems(input map[string]interface{}, requireArray bool) ([]map[string]interface{}, error) {
	rawItems, ok := input["items"]
	if !ok || rawItems == nil {
		if requireArray {
			return nil, ErrFileLinesToolItemsInvalid
		}
		return []map[string]interface{}{cloneInputMap(input)}, nil
	}
	entries, ok := rawItems.([]interface{})
	if !ok || len(entries) == 0 {
		return nil, ErrFileLinesToolItemsInvalid
	}
	out := make([]map[string]interface{}, 0, len(entries))
	for _, item := range entries {
		entry, ok := item.(map[string]interface{})
		if !ok {
			return nil, ErrFileLinesToolItemsInvalid
		}
		out = append(out, cloneInputMap(entry))
	}
	return out, nil
}

func cloneInputMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return map[string]interface{}{}
	}
	out := make(map[string]interface{}, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

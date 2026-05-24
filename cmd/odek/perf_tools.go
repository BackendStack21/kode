package main

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/BackendStack21/kode"
	"github.com/BackendStack21/kode/internal/danger"
)

// ═════════════════════════════════════════════════════════════════════════
// 1. batch_patch — Apply multiple edits atomically
// ═════════════════════════════════════════════════════════════════════════

const maxBatchPatches = 10

type batchPatchTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *batchPatchTool) Name() string        { return "batch_patch" }
func (t *batchPatchTool) Description() string  { return `Apply up to 10 find-replace edits across files in a single call. Edits are applied sequentially; if any fails the rest are skipped (early-stop). Each edit uses O_NOFOLLOW read + atomic temp+rename write, same as the patch tool.` }

type batchPatchArg struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type batchPatchEntry struct {
	Path    string `json:"path"`
	Success bool   `json:"success"`
	Diff    string `json:"diff,omitempty"`
	Error   string `json:"error,omitempty"`
}

type batchPatchArgs struct {
	Patches []batchPatchArg `json:"patches"`
}

type batchPatchResult struct {
	Results []batchPatchEntry `json:"results"`
}

func (t *batchPatchTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patches": map[string]any{
				"type":        "array",
				"description": "Edits to apply (max 10). Each: {path, old_string, new_string, replace_all?}.",
				"minItems":    1,
				"maxItems":    maxBatchPatches,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":       map[string]any{"type": "string", "description": "File path."},
						"old_string": map[string]any{"type": "string", "description": "Text to find."},
						"new_string": map[string]any{"type": "string", "description": "Replacement (empty = delete)."},
						"replace_all": map[string]any{"type": "boolean", "description": "Replace all occurrences (default: false)."},
					},
					"required": []string{"path", "old_string"},
				},
			},
		},
		"required": []string{"patches"},
	}
}

func (t *batchPatchTool) Call(argsJSON string) (string, error) {
	var args batchPatchArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if len(args.Patches) == 0 {
		return jsonError("at least one patch is required")
	}
	if len(args.Patches) > maxBatchPatches {
		return jsonError(fmt.Sprintf("max %d patches per call", maxBatchPatches))
	}

	results := make([]batchPatchEntry, len(args.Patches))
	allOK := true
	stopIdx := 0

	for idx, p := range args.Patches {
		stopIdx = idx
		entry := batchPatchEntry{Path: p.Path}
		if p.OldString == "" {
			entry.Error = "old_string is required"
			results[idx] = entry
			allOK = false
			break
		}

		if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
			Name: "batch_patch", Resource: p.Path, Risk: danger.ClassifyPath(p.Path),
		}, nil); err != nil {
			entry.Error = err.Error()
			results[idx] = entry
			allOK = false
			break
		}

		f, err := os.OpenFile(p.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
		if err != nil {
			entry.Error = fmt.Sprintf("cannot open %q: %v", p.Path, err)
			results[idx] = entry
			allOK = false
			break
		}

		var sb strings.Builder
		_, err = io.Copy(&sb, f)
		f.Close()
		if err != nil {
			entry.Error = fmt.Sprintf("cannot read %q: %v", p.Path, err)
			results[idx] = entry
			allOK = false
			break
		}
		original := sb.String()

		if !strings.Contains(original, p.OldString) {
			entry.Error = fmt.Sprintf("old_string not found in %q", p.Path)
			results[idx] = entry
			allOK = false
			break
		}

		var modified string
		if p.ReplaceAll {
			modified = strings.ReplaceAll(original, p.OldString, p.NewString)
		} else {
			modified = strings.Replace(original, p.OldString, p.NewString, 1)
		}

		diff := fmt.Sprintf("--- a/%s\n+++ b/%s\n@@ -1 +1 @@\n-%s\n+%s\n",
			p.Path, p.Path, truncateDiff(original, 100), truncateDiff(modified, 100))

		// Atomic write
		dir := filepath.Dir(p.Path)
		tmpFile, err := os.CreateTemp(dir, ".tmp_batchpatch_*")
		if err != nil {
			entry.Error = fmt.Sprintf("cannot create temp file: %v", err)
			results[idx] = entry
			allOK = false
			break
		}
		tmpPath := tmpFile.Name()
		tmpFile.Write([]byte(modified))
		tmpFile.Chmod(0644)
		tmpFile.Close()

		if err := os.Rename(tmpPath, p.Path); err != nil {
			os.Remove(tmpPath)
			entry.Error = fmt.Sprintf("cannot write %q: %v", p.Path, err)
			results[idx] = entry
			allOK = false
			break
		}

		entry.Success = true
		entry.Diff = diff
		results[idx] = entry
	}

	if !allOK {
		for j := stopIdx + 1; j < len(args.Patches); j++ {
			results[j] = batchPatchEntry{
				Path:  args.Patches[j].Path,
				Error: "skipped due to prior failure",
			}
		}
	}

	return jsonResult(batchPatchResult{Results: results})
}

// ═════════════════════════════════════════════════════════════════════════
// 2. parallel_shell — Run N shell commands concurrently
// ═════════════════════════════════════════════════════════════════════════

const maxParallelShellCmds = 8

type parallelShellTool struct {
	dangerousConfig danger.DangerousConfig
	approver        danger.Approver
}

func (t *parallelShellTool) Name() string        { return "parallel_shell" }
func (t *parallelShellTool) Description() string  { return `Run multiple independent shell commands in parallel. Each command gets its own process. Returns structured results with stdout, stderr, exit_code, and duration for each command. Commands requiring approval are checked before execution.` }

type parallelShellCmd struct {
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
	Timeout     int    `json:"timeout,omitempty"`
}

type parallelShellEntry struct {
	Index      int    `json:"index"`
	Command    string `json:"command"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type parallelShellArgs struct {
	Commands []parallelShellCmd `json:"commands"`
}

type parallelShellResult struct {
	Results []parallelShellEntry `json:"results"`
}

func (t *parallelShellTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"commands": map[string]any{
				"type":        "array",
				"description": "Commands to run in parallel (max 8). Each: {command, description?, timeout?}.",
				"minItems":    1,
				"maxItems":    maxParallelShellCmds,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"command":     map[string]any{"type": "string", "description": "Shell command to execute."},
						"description": map[string]any{"type": "string", "description": "Explain what this command does (shown in approval)."},
						"timeout":     map[string]any{"type": "integer", "description": "Per-command timeout in seconds (default: 30)."},
					},
					"required": []string{"command"},
				},
			},
		},
		"required": []string{"commands"},
	}
}

func (t *parallelShellTool) Call(argsJSON string) (string, error) {
	var args parallelShellArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if len(args.Commands) == 0 {
		return jsonError("at least one command is required")
	}
	if len(args.Commands) > maxParallelShellCmds {
		return jsonError(fmt.Sprintf("max %d commands per call", maxParallelShellCmds))
	}

	// Pre-check all commands for approval
	for _, c := range args.Commands {
		action := t.dangerousConfig.ActionForCommand(c.Command)
		switch action {
		case danger.Deny:
			return jsonError(fmt.Sprintf("command denied: %s", c.Command))
		case danger.Prompt:
			if t.approver != nil {
				cls := danger.Classify(c.Command)
				if err := t.approver.PromptCommand(cls, c.Command, c.Description); err != nil {
					return jsonError(fmt.Sprintf("command rejected: %s", c.Command))
				}
			}
		}
	}

	results := make([]parallelShellEntry, len(args.Commands))
	var mu sync.Mutex
	sem := make(chan struct{}, 4)

	for i, c := range args.Commands {
		sem <- struct{}{}
		go func(idx int, cmd parallelShellCmd) {
			defer func() { <-sem }()

			timeout := cmd.Timeout
			if timeout <= 0 {
				timeout = 30
			}

			start := time.Now()
			entry := parallelShellEntry{Index: idx, Command: cmd.Command}

			shCmd := exec.Command("sh", "-c", cmd.Command)
			var stdout, stderr strings.Builder
			shCmd.Stdout = &stdout
			shCmd.Stderr = &stderr

			// Kill on timeout via goroutine
			done := make(chan error, 1)
			go func() {
				done <- shCmd.Run()
			}()

			select {
			case err := <-done:
				entry.Stdout = strings.TrimSpace(stdout.String())
				entry.Stderr = strings.TrimSpace(stderr.String())
				entry.DurationMs = time.Since(start).Milliseconds()

				if err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						entry.ExitCode = exitErr.ExitCode()
					} else {
						entry.Error = err.Error()
					}
				}
			case <-time.After(time.Duration(timeout) * time.Second):
				if shCmd.Process != nil {
					shCmd.Process.Kill()
				}
				entry.Error = fmt.Sprintf("timeout after %ds", timeout)
				entry.DurationMs = time.Since(start).Milliseconds()
			}

			mu.Lock()
			results[idx] = entry
			mu.Unlock()
		}(i, c)
	}

	// Drain
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	return jsonResult(parallelShellResult{Results: results})
}

// ═════════════════════════════════════════════════════════════════════════
// 3. http_batch — Fetch N URLs in parallel
// ═════════════════════════════════════════════════════════════════════════

const maxHTTPBatchURLs = 10

type httpBatchTool struct {
	dangerousConfig danger.DangerousConfig
	client          *http.Client
}

func newHTTPBatchTool(dc danger.DangerousConfig) *httpBatchTool {
	return &httpBatchTool{
		dangerousConfig: dc,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (t *httpBatchTool) Name() string        { return "http_batch" }
func (t *httpBatchTool) Description() string  { return `Fetch multiple URLs in parallel. Returns status code, content length, and error for each URL. Does NOT parse HTML — it's a lightweight parallel fetch for APIs, docs, and data files. Max 10 URLs per call.` }

type httpBatchReq struct {
	URL     string            `json:"url"`
	Method  string            `json:"method,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

type httpBatchEntry struct {
	URL           string `json:"url"`
	Status        int    `json:"status"`
	ContentLength int64  `json:"content_length,omitempty"`
	Error         string `json:"error,omitempty"`
}

type httpBatchArgs struct {
	Requests []httpBatchReq `json:"requests"`
}

type httpBatchResult struct {
	Results []httpBatchEntry `json:"results"`
}

func (t *httpBatchTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"requests": map[string]any{
				"type":        "array",
				"description": "URLs to fetch in parallel (max 10). Each: {url, method?, headers?}.",
				"minItems":    1,
				"maxItems":    maxHTTPBatchURLs,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"url":     map[string]any{"type": "string", "description": "URL to fetch (http/https)."},
						"method":  map[string]any{"type": "string", "description": "HTTP method (default: GET)."},
						"headers": map[string]any{"type": "object", "description": "Optional HTTP headers."},
					},
					"required": []string{"url"},
				},
			},
		},
		"required": []string{"requests"},
	}
}

func (t *httpBatchTool) Call(argsJSON string) (string, error) {
	var args httpBatchArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if len(args.Requests) == 0 {
		return jsonError("at least one URL is required")
	}
	if len(args.Requests) > maxHTTPBatchURLs {
		return jsonError(fmt.Sprintf("max %d URLs per call", maxHTTPBatchURLs))
	}

	results := make([]httpBatchEntry, len(args.Requests))
	var mu sync.Mutex
	sem := make(chan struct{}, 4)

	for i, req := range args.Requests {
		// Security check
		risk := danger.ClassifyURL(req.URL)
		if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
			Name: "http_batch", Resource: req.URL, Risk: risk,
		}, nil); err != nil {
			results[i] = httpBatchEntry{URL: req.URL, Error: err.Error()}
			continue
		}

		sem <- struct{}{}
		go func(idx int, r httpBatchReq) {
			defer func() { <-sem }()

			method := r.Method
			if method == "" {
				method = "GET"
			}

			entry := httpBatchEntry{URL: r.URL}
			httpReq, err := http.NewRequest(method, r.URL, nil)
			if err != nil {
				entry.Error = err.Error()
				mu.Lock()
				results[idx] = entry
				mu.Unlock()
				return
			}

			for k, v := range r.Headers {
				httpReq.Header.Set(k, v)
			}
			httpReq.Header.Set("User-Agent", "odek-http-batch/0.1")

			resp, err := t.client.Do(httpReq)
			if err != nil {
				entry.Error = err.Error()
				mu.Lock()
				results[idx] = entry
				mu.Unlock()
				return
			}
			defer resp.Body.Close()

			entry.Status = resp.StatusCode
			entry.ContentLength = resp.ContentLength
			if entry.ContentLength <= 0 {
				n, _ := io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
				entry.ContentLength = n
			}

			mu.Lock()
			results[idx] = entry
			mu.Unlock()
		}(i, req)
	}

	// Drain
	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	return jsonResult(httpBatchResult{Results: results})
}

// ═════════════════════════════════════════════════════════════════════════
// 4. math_eval — Evaluate arithmetic expressions
// ═════════════════════════════════════════════════════════════════════════

type mathEvalTool struct{}

func (t *mathEvalTool) Name() string        { return "math_eval" }
func (t *mathEvalTool) Description() string  { return `Evaluate a math expression and return the result. Supports: +, -, *, /, parentheses, decimal numbers. Replaces shell: bc, expr, python -c forks. Example: "42 * 17 + 256 / 10"` }

type mathEvalArgs struct {
	Expression string `json:"expression"`
}

type mathEvalResult struct {
	Expression string  `json:"expression"`
	Result     float64 `json:"result"`
	Error      string  `json:"error,omitempty"`
}

func (t *mathEvalTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expression": map[string]any{
				"type":        "string",
				"description": "Arithmetic expression (e.g. '42 * 17 + 256 / 10').",
			},
		},
		"required": []string{"expression"},
	}
}

func (t *mathEvalTool) Call(argsJSON string) (string, error) {
	var args mathEvalArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if args.Expression == "" {
		return jsonError("expression is required")
	}

	result, err := evalMath(args.Expression)
	if err != nil {
		return jsonResult(mathEvalResult{Expression: args.Expression, Error: err.Error()})
	}
	return jsonResult(mathEvalResult{Expression: args.Expression, Result: result})
}

func evalMath(expr string) (float64, error) {
	node, err := parser.ParseExpr(expr)
	if err != nil {
		return 0, fmt.Errorf("parse error: %v", err)
	}
	return evalNode(node)
}

func evalNode(node ast.Expr) (float64, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		if n.Kind == token.INT || n.Kind == token.FLOAT {
			return strconv.ParseFloat(n.Value, 64)
		}
		return 0, fmt.Errorf("unsupported literal: %s", n.Value)
	case *ast.BinaryExpr:
		x, err := evalNode(n.X)
		if err != nil {
			return 0, err
		}
		y, err := evalNode(n.Y)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.ADD:
			return x + y, nil
		case token.SUB:
			return x - y, nil
		case token.MUL:
			return x * y, nil
		case token.QUO:
			if y == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return x / y, nil
		default:
			return 0, fmt.Errorf("unsupported operator: %s", n.Op)
		}
	case *ast.ParenExpr:
		return evalNode(n.X)
	case *ast.UnaryExpr:
		if n.Op == token.SUB {
			v, err := evalNode(n.X)
			if err != nil {
				return 0, err
			}
			return -v, nil
		}
		return 0, fmt.Errorf("unsupported unary operator")
	default:
		return 0, fmt.Errorf("unsupported expression: %T", node)
	}
}

// ═════════════════════════════════════════════════════════════════════════
// 5. diff — Structured file comparison
// ═════════════════════════════════════════════════════════════════════════

type diffTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *diffTool) Name() string        { return "diff" }
func (t *diffTool) Description() string  { return `Compare two files and return structured hunks. Each hunk has a type (equal/added/removed) and line-by-line content. Use path_a+path_b for file-vs-file, or path+content for file-vs-string. Replaces shell: diff fork.` }

type diffArgs struct {
	PathA   string `json:"path_a,omitempty"`
	PathB   string `json:"path_b,omitempty"`
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
}

type diffLine struct {
	OldLine int    `json:"old_line,omitempty"`
	NewLine int    `json:"new_line,omitempty"`
	Content string `json:"content"`
}

type diffHunk struct {
	Type  string     `json:"type"`
	Lines []diffLine `json:"lines"`
}

type diffResult struct {
	Hunks []diffHunk `json:"hunks"`
	Error string     `json:"error,omitempty"`
	PathA string     `json:"path_a"`
	PathB string     `json:"path_b"`
}

func (t *diffTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path_a":  map[string]any{"type": "string", "description": "First file path (for file-vs-file)."},
			"path_b":  map[string]any{"type": "string", "description": "Second file path."},
			"path":    map[string]any{"type": "string", "description": "File path (for file-vs-string)."},
			"content": map[string]any{"type": "string", "description": "String content (for file-vs-string)."},
		},
	}
}

func (t *diffTool) Call(argsJSON string) (string, error) {
	var args diffArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}

	var linesA, linesB []string
	var pathA, pathB string

	if args.PathA != "" && args.PathB != "" {
		pathA, pathB = args.PathA, args.PathB
		for _, p := range []string{args.PathA, args.PathB} {
			if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
				Name: "diff", Resource: p, Risk: danger.ClassifyPath(p),
			}, nil); err != nil {
				return jsonError(err.Error())
			}
		}
		data, err := os.ReadFile(args.PathA)
		if err != nil {
			return jsonResult(diffResult{Error: err.Error(), PathA: pathA, PathB: pathB})
		}
		linesA = strings.Split(string(data), "\n")
		data, err = os.ReadFile(args.PathB)
		if err != nil {
			return jsonResult(diffResult{Error: err.Error(), PathA: pathA, PathB: pathB})
		}
		linesB = strings.Split(string(data), "\n")
	} else if args.Path != "" {
		pathA, pathB = args.Path, "<inline>"
		if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
			Name: "diff", Resource: args.Path, Risk: danger.ClassifyPath(args.Path),
		}, nil); err != nil {
			return jsonError(err.Error())
		}
		data, err := os.ReadFile(args.Path)
		if err != nil {
			return jsonResult(diffResult{Error: err.Error(), PathA: pathA, PathB: pathB})
		}
		linesA = strings.Split(string(data), "\n")
		linesB = strings.Split(args.Content, "\n")
	} else {
		return jsonError("provide either path_a+path_b or path+content")
	}

	// Trim trailing empty from final newline
	if len(linesA) > 0 && linesA[len(linesA)-1] == "" {
		linesA = linesA[:len(linesA)-1]
	}
	if len(linesB) > 0 && linesB[len(linesB)-1] == "" {
		linesB = linesB[:len(linesB)-1]
	}

	hunks := computeDiff(linesA, linesB)
	return jsonResult(diffResult{Hunks: hunks, PathA: pathA, PathB: pathB})
}

func computeDiff(a, b []string) []diffHunk {
	m, n := len(a), len(b)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				lcs[i][j] = lcs[i-1][j-1] + 1
			} else if lcs[i-1][j] >= lcs[i][j-1] {
				lcs[i][j] = lcs[i-1][j]
			} else {
				lcs[i][j] = lcs[i][j-1]
			}
		}
	}

	// Backtrack
	var hunks []diffHunk
	i, j := m, n

	var equalLines, addedLines, removedLines []diffLine

	flushHunk := func() {
		if len(equalLines) > 0 {
			hunks = append(hunks, diffHunk{Type: "equal", Lines: equalLines})
			equalLines = nil
		}
		if len(removedLines) > 0 {
			hunks = append(hunks, diffHunk{Type: "removed", Lines: removedLines})
			removedLines = nil
		}
		if len(addedLines) > 0 {
			hunks = append(hunks, diffHunk{Type: "added", Lines: addedLines})
			addedLines = nil
		}
	}

	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			flushHunk()
			equalLines = append([]diffLine{{OldLine: i, NewLine: j, Content: a[i-1]}}, equalLines...)
			i--
			j--
		} else if j > 0 && (i == 0 || lcs[i][j-1] >= lcs[i-1][j]) {
			addedLines = append([]diffLine{{NewLine: j, Content: b[j-1]}}, addedLines...)
			j--
		} else if i > 0 {
			removedLines = append([]diffLine{{OldLine: i, Content: a[i-1]}}, removedLines...)
			i--
		}
	}
	flushHunk()

	// Reverse hunks
	for i, k := 0, len(hunks)-1; i < k; i, k = i+1, k-1 {
		hunks[i], hunks[k] = hunks[k], hunks[i]
	}

	return hunks
}

// ═════════════════════════════════════════════════════════════════════════
// 6. count_lines — Quick line/byte/char counts
// ═════════════════════════════════════════════════════════════════════════

const maxCountFiles = 20

type countLinesTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *countLinesTool) Name() string        { return "count_lines" }
func (t *countLinesTool) Description() string  { return `Count lines, bytes, and characters in one or more files. Streaming scanner — zero-alloc on content. Replaces shell: wc -l, wc -c, wc -m forks.` }

type countFileArg struct {
	Path string `json:"path"`
}

type countFileEntry struct {
	Path  string `json:"path"`
	Lines int    `json:"lines"`
	Bytes int64  `json:"bytes"`
	Chars int    `json:"chars"`
	Error string `json:"error,omitempty"`
}

type countLinesArgs struct {
	Files []countFileArg `json:"files"`
}

type countLinesResult struct {
	Results []countFileEntry `json:"results"`
	Total   countFileEntry   `json:"total"`
}

func (t *countLinesTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"files": map[string]any{
				"type":        "array",
				"description": "Files to count (max 20). Each: {path}.",
				"minItems":    1,
				"maxItems":    maxCountFiles,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path": map[string]any{"type": "string", "description": "File path."},
					},
					"required": []string{"path"},
				},
			},
		},
		"required": []string{"files"},
	}
}

func (t *countLinesTool) Call(argsJSON string) (string, error) {
	var args countLinesArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if len(args.Files) == 0 {
		return jsonError("at least one file is required")
	}
	if len(args.Files) > maxCountFiles {
		return jsonError(fmt.Sprintf("max %d files per call", maxCountFiles))
	}

	results := make([]countFileEntry, len(args.Files))
	sem := make(chan struct{}, 4)

	for i, f := range args.Files {
		sem <- struct{}{}
		go func(idx int, path string) {
			defer func() { <-sem }()
			results[idx] = t.countFile(path)
		}(i, f.Path)
	}

	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	var total countFileEntry
	total.Path = "(total)"
	for _, r := range results {
		if r.Error == "" {
			total.Lines += r.Lines
			total.Bytes += r.Bytes
			total.Chars += r.Chars
		}
	}

	return jsonResult(countLinesResult{Results: results, Total: total})
}

func (t *countLinesTool) countFile(path string) countFileEntry {
	if path == "" {
		return countFileEntry{Error: "path is required"}
	}

	if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
		Name: "count_lines", Resource: path, Risk: danger.ClassifyPath(path),
	}, nil); err != nil {
		return countFileEntry{Path: path, Error: err.Error()}
	}

	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return countFileEntry{Path: path, Error: fmt.Sprintf("cannot open %q: %v", path, err)}
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return countFileEntry{Path: path, Error: fmt.Sprintf("cannot stat %q: %v", path, err)}
	}
	if info.IsDir() {
		return countFileEntry{Path: path, Error: fmt.Sprintf("%q is a directory", path)}
	}

	lines := 0
	chars := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines++
		chars += len([]rune(scanner.Text())) + 1
	}

	return countFileEntry{
		Path:  path,
		Lines: lines,
		Bytes: info.Size(),
		Chars: chars,
	}
}

// ═════════════════════════════════════════════════════════════════════════
// 7. multi_grep — Search multiple patterns in parallel
// ═════════════════════════════════════════════════════════════════════════

const maxGrepPatterns = 10

type multiGrepTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *multiGrepTool) Name() string        { return "multi_grep" }
func (t *multiGrepTool) Description() string  { return `Search for multiple regex patterns in parallel across files. Each pattern runs its own directory walk with bounded concurrency. Returns structured {pattern, path, line, content} results. Replaces N serial search_files calls. Directly targets the multi_search benchmark.` }

type grepMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

type grepPatternResult struct {
	Pattern string      `json:"pattern"`
	Matches []grepMatch `json:"matches"`
	Count   int         `json:"count"`
	Error   string      `json:"error,omitempty"`
}

type multiGrepArgs struct {
	Patterns []string `json:"patterns"`
	Path     string   `json:"path,omitempty"`
	FileGlob string   `json:"file_glob,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

type multiGrepResult struct {
	Results []grepPatternResult `json:"results"`
}

func (t *multiGrepTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"patterns": map[string]any{
				"type":        "array",
				"description": "Regex patterns to search for (max 10).",
				"minItems":    1,
				"maxItems":    maxGrepPatterns,
				"items":       map[string]any{"type": "string"},
			},
			"path":      map[string]any{"type": "string", "description": "Root directory (default: '.')."},
			"file_glob": map[string]any{"type": "string", "description": "Filter files by glob (e.g. '*.go')."},
			"limit":     map[string]any{"type": "integer", "description": "Max matches per pattern (default: 50)."},
		},
		"required": []string{"patterns"},
	}
}

func (t *multiGrepTool) Call(argsJSON string) (string, error) {
	var args multiGrepArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if len(args.Patterns) == 0 {
		return jsonError("at least one pattern is required")
	}
	if len(args.Patterns) > maxGrepPatterns {
		return jsonError(fmt.Sprintf("max %d patterns per call", maxGrepPatterns))
	}
	if args.Path == "" {
		args.Path = "."
	}
	if args.Limit <= 0 {
		args.Limit = 50
	}

	if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
		Name: "multi_grep", Resource: args.Path, Risk: danger.ClassifyPath(args.Path),
	}, nil); err != nil {
		return jsonError(err.Error())
	}

	results := make([]grepPatternResult, len(args.Patterns))
	sem := make(chan struct{}, 4)

	for i, pattern := range args.Patterns {
		sem <- struct{}{}
		go func(idx int, pat string) {
			defer func() { <-sem }()
			results[idx] = t.searchPattern(pat, args.Path, args.FileGlob, args.Limit)
		}(i, pattern)
	}

	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	return jsonResult(multiGrepResult{Results: results})
}

func (t *multiGrepTool) searchPattern(pattern, root, fileGlob string, limit int) grepPatternResult {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return grepPatternResult{Pattern: pattern, Error: fmt.Sprintf("invalid regex: %v", err)}
	}

	var matches []grepMatch

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if fileGlob != "" {
			match, _ := filepath.Match(fileGlob, info.Name())
			if !match {
				return nil
			}
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		sample := make([]byte, 512)
		n, _ := f.Read(sample)
		if isBinary(sample[:n]) {
			return nil
		}
		f.Seek(0, 0)

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				matches = append(matches, grepMatch{
					Path: path, Line: lineNum, Content: strings.TrimSpace(line),
				})
				if len(matches) >= limit {
					return filepath.SkipAll
				}
			}
		}
		if len(matches) >= limit {
			return filepath.SkipAll
		}
		return nil
	})

	return grepPatternResult{
		Pattern: pattern,
		Matches: matches,
		Count:   len(matches),
	}
}

// ═════════════════════════════════════════════════════════════════════════
// 8. json_query — Query/extract from JSON files
// ═════════════════════════════════════════════════════════════════════════

type jsonQueryTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *jsonQueryTool) Name() string        { return "json_query" }
func (t *jsonQueryTool) Description() string  { return `Parse a JSON file and extract a value using a dot-path query. Supports array indexing with [N]. Empty query returns the entire parsed JSON. Replaces shell: jq, python -c "import json" forks.` }

type jsonQueryArgs struct {
	Path  string `json:"path"`
	Query string `json:"query"`
}

type jsonQueryResult struct {
	Path      string      `json:"path"`
	Query     string      `json:"query"`
	Value     interface{} `json:"value,omitempty"`
	ValueType string      `json:"value_type,omitempty"`
	Error     string      `json:"error,omitempty"`
}

func (t *jsonQueryTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":  map[string]any{"type": "string", "description": "Path to JSON file."},
			"query": map[string]any{"type": "string", "description": "Dot-path query (empty = return entire JSON)."},
		},
		"required": []string{"path"},
	}
}

func (t *jsonQueryTool) Call(argsJSON string) (string, error) {
	var args jsonQueryArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if args.Path == "" {
		return jsonError("path is required")
	}

	if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
		Name: "json_query", Resource: args.Path, Risk: danger.ClassifyPath(args.Path),
	}, nil); err != nil {
		return jsonError(err.Error())
	}

	f, err := os.OpenFile(args.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return jsonResult(jsonQueryResult{Path: args.Path, Error: fmt.Sprintf("cannot open %q: %v", args.Path, err)})
	}
	defer f.Close()

	var data interface{}
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return jsonResult(jsonQueryResult{Path: args.Path, Error: fmt.Sprintf("invalid JSON: %v", err)})
	}

	if args.Query == "" {
		vt := fmt.Sprintf("%T", data)
		return jsonResult(jsonQueryResult{Path: args.Path, Query: "", Value: data, ValueType: vt})
	}

	value, err := jsonPathQuery(data, args.Query)
	if err != nil {
		return jsonResult(jsonQueryResult{Path: args.Path, Query: args.Query, Error: err.Error()})
	}

	vt := fmt.Sprintf("%T", value)
	return jsonResult(jsonQueryResult{Path: args.Path, Query: args.Query, Value: value, ValueType: vt})
}

func jsonPathQuery(data interface{}, query string) (interface{}, error) {
	parts := strings.Split(query, ".")
	current := data

	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty path segment")
		}

		bracketIdx := strings.Index(part, "[")
		if bracketIdx >= 0 {
			if !strings.HasSuffix(part, "]") {
				return nil, fmt.Errorf("invalid array index in %q", part)
			}

			var key string
			if bracketIdx > 0 {
				key = part[:bracketIdx]
			}

			idxStr := part[bracketIdx+1 : len(part)-1]
			idx, err := strconv.Atoi(idxStr)
			if err != nil {
				return nil, fmt.Errorf("invalid index %q", idxStr)
			}

			if key != "" {
				m, ok := current.(map[string]interface{})
				if !ok {
					return nil, fmt.Errorf("%q is not an object", key)
				}
				current, ok = m[key]
				if !ok {
					return nil, fmt.Errorf("key %q not found", key)
				}
			}

			arr, ok := current.([]interface{})
			if !ok {
				return nil, fmt.Errorf("value is not an array")
			}
			if idx < 0 || idx >= len(arr) {
				return nil, fmt.Errorf("index %d out of range (len %d)", idx, len(arr))
			}
			current = arr[idx]
		} else {
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("%q is not an object", part)
			}
			current, ok = m[part]
			if !ok {
				return nil, fmt.Errorf("key %q not found", part)
			}
		}
	}

	return current, nil
}

// ═════════════════════════════════════════════════════════════════════════
// 9. tree — Structured directory tree listing
// ═════════════════════════════════════════════════════════════════════════

type treeTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *treeTool) Name() string        { return "tree" }
func (t *treeTool) Description() string  { return `List the directory tree with file counts, sizes, and nesting. Returns a structured tree: each entry shows path, is_dir, file_count, total_size, children, depth. Replaces shell: find, tree, ls -R forks.` }

type treeArgs struct {
	Path          string `json:"path,omitempty"`
	MaxDepth      int    `json:"max_depth,omitempty"`
	IncludeHidden bool   `json:"include_hidden,omitempty"`
}

type treeEntry struct {
	Path      string      `json:"path"`
	IsDir     bool        `json:"is_dir"`
	FileCount int         `json:"file_count,omitempty"`
	TotalSize int64       `json:"total_size,omitempty"`
	Depth     int         `json:"depth"`
	Children  []treeEntry `json:"children,omitempty"`
	ErrMsg    string      `json:"error,omitempty"`
}

type treeResult struct {
	Tree  treeEntry `json:"tree"`
	Error string    `json:"error,omitempty"`
}

func (t *treeTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path":           map[string]any{"type": "string", "description": "Root directory (default: '.')."},
			"max_depth":      map[string]any{"type": "integer", "description": "Max depth (default: 3, max: 10)."},
			"include_hidden": map[string]any{"type": "boolean", "description": "Include hidden files (default: false)."},
		},
	}
}

func (t *treeTool) Call(argsJSON string) (string, error) {
	var args treeArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if args.Path == "" {
		args.Path = "."
	}
	if args.MaxDepth <= 0 {
		args.MaxDepth = 3
	}
	if args.MaxDepth > 10 {
		args.MaxDepth = 10
	}

	if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
		Name: "tree", Resource: args.Path, Risk: danger.ClassifyPath(args.Path),
	}, nil); err != nil {
		return jsonError(err.Error())
	}

	entry, err := buildTree(args.Path, args.Path, 0, args.MaxDepth, args.IncludeHidden)
	if err != nil {
		return jsonResult(treeResult{Error: err.Error()})
	}

	return jsonResult(treeResult{Tree: entry})
}

func buildTree(root, path string, depth, maxDepth int, includeHidden bool) (treeEntry, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return treeEntry{Path: path, ErrMsg: err.Error()}, nil
	}

	entry := treeEntry{
		Path:  filepath.Base(path),
		IsDir: info.IsDir(),
		Depth: depth,
	}

	if depth == 0 {
		entry.Path = path
	}

	if !info.IsDir() || depth >= maxDepth {
		if !info.IsDir() {
			entry.FileCount = 1
			entry.TotalSize = info.Size()
		}
		return entry, nil
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return entry, nil
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	entry.Children = make([]treeEntry, 0, len(entries))
	for _, e := range entries {
		if !includeHidden && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		childPath := filepath.Join(path, e.Name())
		child, err := buildTree(root, childPath, depth+1, maxDepth, includeHidden)
		if err != nil {
			continue
		}
		entry.Children = append(entry.Children, child)
		entry.FileCount += child.FileCount
		entry.TotalSize += child.TotalSize
	}

	return entry, nil
}

// ═════════════════════════════════════════════════════════════════════════
// 10. checksum — Compute file hashes natively
// ═════════════════════════════════════════════════════════════════════════

const maxChecksumFiles = 10

type checksumTool struct {
	dangerousConfig danger.DangerousConfig
}

func (t *checksumTool) Name() string        { return "checksum" }
func (t *checksumTool) Description() string  { return `Compute cryptographic hashes of files using SHA-256 (default), SHA-1, or MD5. Uses Go crypto stdlib — zero subprocess fork. Replaces shell: sha256sum, sha1sum, md5sum forks.` }

type checksumFileArg struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm,omitempty"`
}

type checksumEntry struct {
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
	Hash      string `json:"hash"`
	Error     string `json:"error,omitempty"`
}

type checksumArgs struct {
	Files []checksumFileArg `json:"files"`
}

type checksumResult struct {
	Results []checksumEntry `json:"results"`
}

func (t *checksumTool) Schema() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"files": map[string]any{
				"type":        "array",
				"description": "Files to hash (max 10). Each: {path, algorithm?} — 'sha256' (default), 'sha1', 'md5'.",
				"minItems":    1,
				"maxItems":    maxChecksumFiles,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"path":      map[string]any{"type": "string", "description": "File path."},
						"algorithm": map[string]any{"type": "string", "description": "Hash algorithm: sha256 (default), sha1, md5."},
					},
					"required": []string{"path"},
				},
			},
		},
		"required": []string{"files"},
	}
}

func (t *checksumTool) Call(argsJSON string) (string, error) {
	var args checksumArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return jsonError("invalid arguments: " + err.Error())
	}
	if len(args.Files) == 0 {
		return jsonError("at least one file is required")
	}
	if len(args.Files) > maxChecksumFiles {
		return jsonError(fmt.Sprintf("max %d files per call", maxChecksumFiles))
	}

	results := make([]checksumEntry, len(args.Files))
	sem := make(chan struct{}, 4)

	for i, f := range args.Files {
		sem <- struct{}{}
		go func(idx int, cf checksumFileArg) {
			defer func() { <-sem }()
			results[idx] = t.hashFile(cf)
		}(i, f)
	}

	for i := 0; i < cap(sem); i++ {
		sem <- struct{}{}
	}

	return jsonResult(checksumResult{Results: results})
}

func (t *checksumTool) hashFile(arg checksumFileArg) checksumEntry {
	if arg.Path == "" {
		return checksumEntry{Error: "path is required"}
	}
	algo := strings.ToLower(arg.Algorithm)
	if algo == "" {
		algo = "sha256"
	}

	if err := t.dangerousConfig.CheckOperation(danger.ToolOperation{
		Name: "checksum", Resource: arg.Path, Risk: danger.ClassifyPath(arg.Path),
	}, nil); err != nil {
		return checksumEntry{Path: arg.Path, Algorithm: algo, Error: err.Error()}
	}

	f, err := os.OpenFile(arg.Path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return checksumEntry{Path: arg.Path, Algorithm: algo, Error: fmt.Sprintf("cannot open %q: %v", arg.Path, err)}
	}
	defer f.Close()

	var hash string
	switch algo {
	case "sha256":
		h := sha256.New()
		io.Copy(h, f)
		hash = hex.EncodeToString(h.Sum(nil))
	case "sha1":
		h := sha1.New()
		io.Copy(h, f)
		hash = hex.EncodeToString(h.Sum(nil))
	case "md5":
		h := md5.New()
		io.Copy(h, f)
		hash = hex.EncodeToString(h.Sum(nil))
	default:
		return checksumEntry{Path: arg.Path, Algorithm: algo, Error: fmt.Sprintf("unsupported algorithm: %s", algo)}
	}

	return checksumEntry{Path: arg.Path, Algorithm: algo, Hash: hash}
}

// ── Compile-time interface checks ────────────────────────────────────
var (
	_ odek.Tool = (*batchPatchTool)(nil)
	_ odek.Tool = (*parallelShellTool)(nil)
	_ odek.Tool = (*httpBatchTool)(nil)
	_ odek.Tool = (*mathEvalTool)(nil)
	_ odek.Tool = (*diffTool)(nil)
	_ odek.Tool = (*countLinesTool)(nil)
	_ odek.Tool = (*multiGrepTool)(nil)
	_ odek.Tool = (*jsonQueryTool)(nil)
	_ odek.Tool = (*treeTool)(nil)
	_ odek.Tool = (*checksumTool)(nil)
)

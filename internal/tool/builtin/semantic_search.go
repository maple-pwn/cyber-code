package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

const (
	maxSemanticFiles       = 1000
	maxSemanticFileBytes   = 512 << 10
	maxSemanticTotalBytes  = 8 << 20
	maxSemanticOutputBytes = 32 << 10
)

// semanticSearchTool provides bounded local relevance ranking using TF-IDF.
// It does not use an embedding model and does not claim semantic equivalence.
type semanticSearchTool struct{ workspace string }

func NewSemanticSearch(workspace string) tool.Tool { return &semanticSearchTool{workspace: workspace} }

func (s *semanticSearchTool) Spec() tool.Spec {
	return tool.Spec{
		Name:            "semantic_search",
		Description:     "Rank workspace text files by local TF-IDF relevance to a query (no embeddings)",
		ReadOnly:        true,
		ConcurrencySafe: false,
		Schema: json.RawMessage(`{"type":"object","required":["query"],"properties":{
			"query":{"type":"string","description":"Natural language or code snippet to search for"},
			"path":{"type":"string","description":"Subdirectory to search within (default: workspace root)"},
			"limit":{"type":"integer","description":"Maximum results to return (default: 20, max: 100)"}
		},"additionalProperties":false}`),
	}
}

func (s *semanticSearchTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return permissions.Request{}, err
	}
	if input.Path == "" {
		input.Path = "."
	}
	return permissions.Request{Tool: "semantic_search", Action: permissions.ActionRead, Workspace: s.workspace, Paths: []string{input.Path}}, nil
}

func (s *semanticSearchTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var input struct {
		Query string `json:"query"`
		Path  string `json:"path"`
		Limit int    `json:"limit"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return core.ToolResult{}, err
	}
	if strings.TrimSpace(input.Query) == "" {
		return core.ToolResult{}, fmt.Errorf("query is required")
	}
	if input.Path == "" {
		input.Path = "."
	}
	if input.Limit <= 0 {
		input.Limit = 20
	}
	if input.Limit > 100 {
		input.Limit = 100
	}
	if err := ctx.Err(); err != nil {
		return core.ToolResult{}, err
	}
	workspaceRoot, err := permissions.ResolvePath(s.workspace, ".")
	if err != nil {
		return core.ToolResult{}, err
	}
	root, err := permissions.ResolvePath(workspaceRoot, input.Path)
	if err != nil {
		return core.ToolResult{}, err
	}

	// Phase 1: collect files and their content.
	type fileEntry struct {
		path    string
		content string
	}
	var files []fileEntry
	totalBytes := 0
	visitedFiles := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		visitedFiles++
		if visitedFiles > maxSemanticFiles {
			return fs.SkipAll
		}
		info, statErr := entry.Info()
		if statErr != nil {
			return statErr
		}
		if info.Size() > maxSemanticFileBytes || isBinaryExtension(filepath.Ext(path)) {
			return nil
		}
		if info.Size() > int64(maxSemanticTotalBytes-totalBytes) {
			return fs.SkipAll
		}
		resolvedPath, resolveErr := permissions.ResolvePath(workspaceRoot, path)
		if resolveErr != nil {
			return resolveErr
		}
		file, openErr := os.Open(resolvedPath)
		if openErr != nil {
			return openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxSemanticFileBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		if len(data) == 0 || len(data) > maxSemanticFileBytes || bytes.IndexByte(data, 0) >= 0 {
			return nil
		}
		if len(data) > maxSemanticTotalBytes-totalBytes {
			return fs.SkipAll
		}
		totalBytes += len(data)
		relPath, relErr := filepath.Rel(workspaceRoot, resolvedPath)
		if relErr != nil {
			return relErr
		}
		relPath = filepath.ToSlash(relPath)
		files = append(files, fileEntry{path: relPath, content: string(data)})
		return nil
	})
	if err != nil {
		return core.ToolResult{}, fmt.Errorf("semantic search walk: %w", err)
	}
	if len(files) == 0 {
		return textResult("no files found in the search path"), nil
	}

	// Phase 2: tokenize and build TF-IDF vectors.
	queryTokens := tokenize(input.Query)
	if len(queryTokens) == 0 {
		return textResult("query produced no searchable tokens"), nil
	}

	// Document frequency: how many files contain each term.
	docFreq := make(map[string]int)
	fileTokens := make([]map[string]int, len(files))
	for i, f := range files {
		tokens := tokenize(f.content)
		seen := make(map[string]bool)
		tf := make(map[string]int)
		for _, t := range tokens {
			tf[t]++
			seen[t] = true
		}
		fileTokens[i] = tf
		for t := range seen {
			docFreq[t]++
		}
	}

	// Phase 3: compute TF-IDF vector for query and each file, then cosine similarity.
	queryVec := make(map[string]float64)
	totalDocs := float64(len(files))
	for _, t := range queryTokens {
		df := float64(docFreq[t])
		if df > 0 {
			idf := math.Log(totalDocs/df) + 1
			queryVec[t] += idf
		}
	}
	queryNorm := vectorNorm(queryVec)
	if queryNorm == 0 {
		return textResult("query terms not found in any file"), nil
	}

	type scored struct {
		path  string
		score float64
	}
	var results []scored
	for i, tf := range fileTokens {
		if ctx.Err() != nil {
			return core.ToolResult{}, ctx.Err()
		}
		// Build TF-IDF vector for this file.
		fileNorm := 0.0
		dot := 0.0
		for t, count := range tf {
			df := float64(docFreq[t])
			if df == 0 {
				continue
			}
			idf := math.Log(totalDocs/df) + 1
			tfidf := float64(count) * idf
			fileNorm += tfidf * tfidf
			if qv, ok := queryVec[t]; ok {
				dot += qv * tfidf
			}
		}
		fileNorm = math.Sqrt(fileNorm)
		if fileNorm == 0 {
			continue
		}
		similarity := dot / (queryNorm * fileNorm)
		if similarity > 0.01 {
			results = append(results, scored{path: files[i].path, score: similarity})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].score == results[j].score {
			return results[i].path < results[j].path
		}
		return results[i].score > results[j].score
	})
	if len(results) > input.Limit {
		results = results[:input.Limit]
	}
	if len(results) == 0 {
		return textResult("no semantically similar files found"), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Local relevance search for %q (TF-IDF, no embeddings), top %d results:\n\n", input.Query, len(results)))
	for _, r := range results {
		line := fmt.Sprintf("%.3f  %s\n", r.score, r.path)
		if sb.Len()+len(line) > maxSemanticOutputBytes {
			break
		}
		sb.WriteString(line)
	}
	return textResult(sb.String()), nil
}

// tokenize splits text into lowercase alphanumeric tokens, filtering out
// very short tokens and common stop words.
func tokenize(text string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		word := strings.ToLower(current.String())
		current.Reset()
		if len(word) < 2 || isStopWord(word) {
			return
		}
		tokens = append(tokens, word)
	}
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return tokens
}

func vectorNorm(vec map[string]float64) float64 {
	sum := 0.0
	for _, v := range vec {
		sum += v * v
	}
	return math.Sqrt(sum)
}

func isStopWord(word string) bool {
	_, ok := stopWords[word]
	return ok
}

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "are": true, "but": true,
	"not": true, "you": true, "all": true, "can": true, "had": true,
	"her": true, "was": true, "one": true, "our": true, "out": true,
	"has": true, "have": true, "this": true, "that": true, "with": true,
	"from": true, "they": true, "been": true, "said": true, "each": true,
	"which": true, "their": true, "will": true, "other": true, "about": true,
	"many": true, "then": true, "them": true, "some": true, "would": true,
	"into": true, "than": true, "its": true, "only": true, "also": true,
	"func": true, "return": true, "package": true, "import": true, "type": true,
	"var": true, "const": true, "struct": true, "interface": true, "map": true,
	"string": true, "bool": true, "int": true, "error": true, "nil": true,
	"true": true, "false": true, "case": true, "switch": true, "default": true,
	"break": true, "continue": true, "range": true, "select": true, "chan": true,
	"goto": true, "defer": true, "fallthrough": true, "else": true, "if": true,
}

func isBinaryExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".ico", ".webp",
		".pdf", ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z",
		".exe", ".dll", ".so", ".dylib", ".o", ".a",
		".woff", ".woff2", ".ttf", ".eot",
		".mp3", ".mp4", ".avi", ".mov", ".wav",
		".pyc", ".pyo", ".class", ".jar":
		return true
	}
	return false
}

package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/permissions"
	"cyber-code/internal/tool"
)

const maxNotebookCells = 256
const maxNotebookSourceBytes = 1 << 20

type notebookEditTool struct {
	workspace string
	spec      tool.Spec
}

func NewNotebookEdit(workspace string) tool.Tool {
	return &notebookEditTool{workspace: workspace, spec: tool.Spec{
		Name: "notebook_edit", Description: "Edit a Jupyter notebook cell as structured JSON",
		Schema: json.RawMessage(`{"type":"object","required":["path","operation"],"properties":{"path":{"type":"string"},"operation":{"enum":["replace","insert","delete"]},"cell_id":{"type":"string"},"index":{"type":"integer"},"cell_type":{"enum":["code","markdown","raw"]},"source":{"type":"string"}},"additionalProperties":false}`),
	}}
}

func (notebook *notebookEditTool) Spec() tool.Spec { return notebook.spec }

func (notebook *notebookEditTool) Authorize(_ context.Context, arguments json.RawMessage) (permissions.Request, error) {
	var input struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return permissions.Request{}, err
	}
	return permissions.Request{Tool: notebook.spec.Name, Action: permissions.ActionWrite, Workspace: notebook.workspace, Paths: []string{input.Path}}, nil
}

func (notebook *notebookEditTool) Run(ctx context.Context, arguments json.RawMessage) (core.ToolResult, error) {
	var input struct {
		Path      string `json:"path"`
		Operation string `json:"operation"`
		CellID    string `json:"cell_id"`
		Index     int    `json:"index"`
		CellType  string `json:"cell_type"`
		Source    string `json:"source"`
	}
	if err := json.Unmarshal(arguments, &input); err != nil {
		return core.ToolResult{}, err
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(input.Path)), ".ipynb") {
		return core.ToolResult{}, fmt.Errorf("notebook path must end with .ipynb")
	}
	path, err := permissions.ResolvePath(notebook.workspace, input.Path)
	if err != nil {
		return core.ToolResult{}, err
	}
	diffPath, err := relativeDiffPath(notebook.workspace, path)
	if err != nil {
		return core.ToolResult{}, err
	}
	content, err := readLimitedFile(path)
	if err != nil {
		return core.ToolResult{}, err
	}
	var document struct {
		NBFormat      int               `json:"nbformat"`
		NBFormatMinor int               `json:"nbformat_minor"`
		Metadata      json.RawMessage   `json:"metadata"`
		Cells         []json.RawMessage `json:"cells"`
	}
	if err := json.Unmarshal(content, &document); err != nil || document.NBFormat != 4 || document.Cells == nil || len(document.Cells) > maxNotebookCells {
		return core.ToolResult{}, fmt.Errorf("invalid Jupyter notebook: requires nbformat 4 and a bounded cells array")
	}
	for index, raw := range document.Cells {
		if err := validateNotebookCell(raw); err != nil {
			return core.ToolResult{}, fmt.Errorf("cell %d: %w", index, err)
		}
	}
	cellIndex := -1
	switch input.Operation {
	case "replace", "delete":
		cellIndex, err = notebookCellIndex(document.Cells, input.CellID, input.Index)
		if err != nil {
			return core.ToolResult{}, err
		}
		if input.Operation == "delete" {
			document.Cells = append(document.Cells[:cellIndex], document.Cells[cellIndex+1:]...)
		} else {
			cell, err := replaceNotebookCell(document.Cells[cellIndex], input.Source)
			if err != nil {
				return core.ToolResult{}, err
			}
			document.Cells[cellIndex] = cell
		}
	case "insert":
		if len(document.Cells) >= maxNotebookCells {
			return core.ToolResult{}, fmt.Errorf("notebook exceeds %d cells", maxNotebookCells)
		}
		if input.Index < 0 || input.Index > len(document.Cells) {
			return core.ToolResult{}, fmt.Errorf("insert index %d is out of range", input.Index)
		}
		cell, err := newNotebookCell(input.CellID, input.CellType, input.Source, path)
		if err != nil {
			return core.ToolResult{}, err
		}
		document.Cells = append(document.Cells, nil)
		copy(document.Cells[input.Index+1:], document.Cells[input.Index:])
		document.Cells[input.Index] = cell
	default:
		return core.ToolResult{}, fmt.Errorf("operation must be replace, insert, or delete")
	}
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return core.ToolResult{}, err
	}
	updated := append(encoded, '\n')
	if err := atomicWrite(ctx, path, updated); err != nil {
		return core.ToolResult{}, err
	}
	return fileResult("notebook updated", diffPath, string(content), string(updated)), nil
}

func validateNotebookCell(raw json.RawMessage) error {
	var cell struct {
		CellType string          `json:"cell_type"`
		Source   json.RawMessage `json:"source"`
	}
	if err := json.Unmarshal(raw, &cell); err != nil || (cell.CellType != "code" && cell.CellType != "markdown" && cell.CellType != "raw") {
		return fmt.Errorf("invalid cell_type")
	}
	if len(cell.Source) == 0 || string(cell.Source) == "null" {
		return fmt.Errorf("source is required")
	}
	return nil
}

func notebookCellIndex(cells []json.RawMessage, id string, index int) (int, error) {
	if strings.TrimSpace(id) != "" {
		found := -1
		for i, raw := range cells {
			var cell struct {
				ID string `json:"id"`
			}
			_ = json.Unmarshal(raw, &cell)
			if cell.ID == id {
				if found >= 0 {
					return -1, fmt.Errorf("cell_id %q is duplicated", id)
				}
				found = i
			}
		}
		if found < 0 {
			return -1, fmt.Errorf("cell_id %q was not found", id)
		}
		return found, nil
	}
	if index < 0 || index >= len(cells) {
		return -1, fmt.Errorf("cell index %d is out of range", index)
	}
	return index, nil
}

func replaceNotebookCell(raw json.RawMessage, source string) (json.RawMessage, error) {
	if len([]byte(source)) > maxNotebookSourceBytes {
		return nil, fmt.Errorf("cell source exceeds %d bytes", maxNotebookSourceBytes)
	}
	var cell map[string]any
	if err := json.Unmarshal(raw, &cell); err != nil {
		return nil, err
	}
	cell["source"] = source
	return json.Marshal(cell)
}

func newNotebookCell(id, cellType, source, path string) (json.RawMessage, error) {
	if cellType == "" {
		cellType = "code"
	}
	if cellType != "code" && cellType != "markdown" && cellType != "raw" {
		return nil, fmt.Errorf("invalid cell_type %q", cellType)
	}
	if len([]byte(source)) > maxNotebookSourceBytes {
		return nil, fmt.Errorf("cell source exceeds %d bytes", maxNotebookSourceBytes)
	}
	if id == "" {
		digest := sha256.Sum256([]byte(path + "\x00" + source))
		id = "cyber-" + hex.EncodeToString(digest[:8])
	}
	return json.Marshal(map[string]any{"cell_type": cellType, "id": id, "metadata": map[string]any{}, "source": source})
}

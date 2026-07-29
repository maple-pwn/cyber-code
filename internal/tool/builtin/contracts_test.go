package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInteractiveAndStateToolContracts(t *testing.T) {
	ask := NewAskUser(func(context.Context, Question) (string, error) { return "", errors.New("question failed") })
	if ask.Spec().Name != "ask_user" {
		t.Fatal("ask spec")
	}
	if _, err := ask.Authorize(context.Background(), json.RawMessage(`{"question":"choose"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := ask.Run(context.Background(), json.RawMessage(`{"question":"choose"}`)); err == nil || err.Error() != "question failed" {
		t.Fatalf("question error=%v", err)
	}
	for _, input := range []string{`{`, `{"question":""}`, `{"question":"q","options":["a","a"]}`, `{"question":"q","options":[""]}`} {
		if _, err := ask.Authorize(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	todo := NewTodo(filepath.Join(t.TempDir(), "todo.json"))
	if todo.Spec().Name != "todo_write" {
		t.Fatal("todo spec")
	}
	if request, err := todo.Authorize(context.Background(), nil); err != nil || len(request.Paths) != 1 {
		t.Fatalf("todo auth=%#v err=%v", request, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := todo.Run(ctx, json.RawMessage(`{"todos":[]}`)); !errors.Is(err, context.Canceled) {
		t.Fatalf("todo cancel=%v", err)
	}
	if _, err := todo.Run(context.Background(), json.RawMessage(`{"todos":[{"id":"","content":"x","status":"pending"}]}`)); err == nil {
		t.Fatal("empty todo ID accepted")
	}
}

func TestAdditionalBuiltinBoundaryContracts(t *testing.T) {
	emptyAnswer := NewAskUser(func(context.Context, Question) (string, error) { return "  ", nil })
	if _, err := emptyAnswer.Run(context.Background(), json.RawMessage(`{"question":"q"}`)); err == nil {
		t.Fatal("empty answer accepted")
	}
	tooManyOptions := make([]string, 21)
	for index := range tooManyOptions {
		tooManyOptions[index] = string(rune('a' + index))
	}
	encodedOptions, _ := json.Marshal(map[string]any{"question": "q", "options": tooManyOptions})
	if _, err := emptyAnswer.Authorize(context.Background(), encodedOptions); err == nil {
		t.Fatal("too many options accepted")
	}

	todo := NewTodo(filepath.Join(t.TempDir(), "todo.json"))
	if result, err := todo.Run(context.Background(), json.RawMessage(`{"todos":[]}`)); err != nil || result.Content[0].Text != "Todo list is empty" {
		t.Fatalf("empty todo=%#v err=%v", result, err)
	}
	tooManyTodos := make([]TodoItem, maxTodoItems+1)
	encodedTodos, _ := json.Marshal(map[string]any{"todos": tooManyTodos})
	if _, err := todo.Run(context.Background(), encodedTodos); err == nil {
		t.Fatal("oversized todo list accepted")
	}

	workspace := t.TempDir()
	notebook := NewNotebookEdit(workspace)
	path := filepath.Join(workspace, "book.ipynb")
	if err := os.WriteFile(path, []byte(`{"nbformat":4,"cells":[{"cell_type":"bad","source":"x"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := notebook.Run(context.Background(), json.RawMessage(`{"path":"book.ipynb","operation":"delete","index":0}`)); err == nil {
		t.Fatal("invalid notebook cell accepted")
	}
	if err := os.WriteFile(path, []byte(`{"nbformat":4,"cells":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{"path":"book.ipynb","operation":"unknown"}`, `{"path":"book.ipynb","operation":"insert","index":2}`, `{"path":"book.ipynb","operation":"delete","index":0}`} {
		if _, err := notebook.Run(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("notebook accepted %s", input)
		}
	}
	if _, err := replaceNotebookCell(json.RawMessage(`{"source":"x"}`), strings.Repeat("x", maxNotebookSourceBytes+1)); err == nil {
		t.Fatal("oversized replacement accepted")
	}
	if generated, err := newNotebookCell("", "", "source", "book.ipynb"); err != nil || !strings.Contains(string(generated), "cyber-") {
		t.Fatalf("generated cell=%s err=%v", generated, err)
	}

	search := NewWebSearch(func(context.Context, string, int) ([]WebResult, error) { return nil, errors.New("search failed") })
	if _, err := search.Run(context.Background(), json.RawMessage(`{"query":"q"}`)); err == nil || err.Error() != "search failed" {
		t.Fatalf("search error=%v", err)
	}
	public, _ := url.Parse("https://1.1.1.1")
	if err := validatePublicURL(context.Background(), nil, public); err != nil {
		t.Fatal(err)
	}
}

func TestNotebookContractValidationBranches(t *testing.T) {
	workspace := t.TempDir()
	notebook := NewNotebookEdit(workspace)
	if notebook.Spec().Name != "notebook_edit" {
		t.Fatal("notebook spec")
	}
	request, err := notebook.Authorize(context.Background(), json.RawMessage(`{"path":"book.ipynb"}`))
	if err != nil || len(request.Paths) != 1 {
		t.Fatalf("auth=%#v err=%v", request, err)
	}
	if _, err := notebook.Authorize(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed auth accepted")
	}
	if _, err := notebook.Run(context.Background(), json.RawMessage(`{"path":"book.txt","operation":"insert"}`)); err == nil {
		t.Fatal("non-notebook accepted")
	}
	if _, err := newNotebookCell("", "invalid", "", "book.ipynb"); err == nil {
		t.Fatal("invalid cell type accepted")
	}
	if _, err := newNotebookCell("", "code", strings.Repeat("x", maxNotebookSourceBytes+1), "book.ipynb"); err == nil {
		t.Fatal("oversized cell accepted")
	}
	if _, err := replaceNotebookCell(json.RawMessage(`{`), "x"); err == nil {
		t.Fatal("malformed cell accepted")
	}
	cells := []json.RawMessage{json.RawMessage(`{"id":"same"}`), json.RawMessage(`{"id":"same"}`)}
	if _, err := notebookCellIndex(cells, "same", 0); err == nil {
		t.Fatal("duplicate cell ID accepted")
	}
	if _, err := notebookCellIndex(cells, "missing", 0); err == nil {
		t.Fatal("missing cell ID accepted")
	}
}

func TestWebToolAuthorizationAndNetworkValidation(t *testing.T) {
	fetch := NewWebFetch(nil)
	if fetch.Spec().Name != "web_fetch" {
		t.Fatal("fetch spec")
	}
	request, err := fetch.Authorize(context.Background(), json.RawMessage(`{"url":"https://example.com/path"}`))
	if err != nil || request.Network[0] != "example.com" {
		t.Fatalf("auth=%#v err=%v", request, err)
	}
	for _, input := range []string{`{`, `{"url":"ftp://example.com"}`, `{"url":"https://user@example.com"}`} {
		if _, err := fetch.Authorize(context.Background(), json.RawMessage(input)); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	search := NewWebSearch(nil)
	if search.Spec().Name != "web_search" {
		t.Fatal("search spec")
	}
	if _, err := search.Authorize(context.Background(), json.RawMessage(`{"query":"cyber-code"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := search.Authorize(context.Background(), json.RawMessage(`{"query":" "}`)); err == nil {
		t.Fatal("empty query accepted")
	}
	if _, err := search.Run(context.Background(), json.RawMessage(`{"query":"q","limit":21}`)); err == nil {
		t.Fatal("oversized limit accepted")
	}
	if _, err := search.Run(context.Background(), json.RawMessage(`{"query":"q"}`)); err == nil {
		t.Fatal("missing provider accepted")
	}
	u, _ := url.Parse("https://127.0.0.1")
	if err := validatePublicURL(context.Background(), nil, u); err == nil {
		t.Fatal("private URL accepted")
	}
	if err := validatePublicURL(context.Background(), nil, nil); err == nil {
		t.Fatal("nil URL accepted")
	}
	dial := secureDialContext(nil)
	if _, err := dial(context.Background(), "tcp", "bad-address"); err == nil {
		t.Fatal("malformed address accepted")
	}
	if _, err := dial(context.Background(), "tcp", net.JoinHostPort("127.0.0.1", "80")); err == nil {
		t.Fatal("private dial accepted")
	}
}

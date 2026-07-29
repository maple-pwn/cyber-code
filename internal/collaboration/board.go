package collaboration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"cyber-code/internal/filelock"
)

type TaskStatus string

const (
	TaskPending   TaskStatus = "pending"
	TaskRunning   TaskStatus = "running"
	TaskCompleted TaskStatus = "completed"
	TaskFailed    TaskStatus = "failed"
	TaskCancelled TaskStatus = "cancelled"
)

type Task struct {
	ID          string     `json:"id"`
	Agent       string     `json:"agent,omitempty"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type BoardOptions struct{ MaxTasks int }

type Board struct {
	path     string
	lockPath string
	maxTasks int
	mu       sync.Mutex
}

func NewBoard(directory string, options BoardOptions) (*Board, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, fmt.Errorf("task board directory is required")
	}
	if options.MaxTasks <= 0 {
		options.MaxTasks = 4096
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	board := &Board{path: filepath.Join(directory, "board.json"), lockPath: filepath.Join(directory, ".board.lock"), maxTasks: options.MaxTasks}
	board.mu.Lock()
	defer board.mu.Unlock()
	release, err := filelock.Acquire(board.lockPath)
	if err != nil {
		return nil, err
	}
	defer release()
	tasks, err := board.read()
	if err != nil {
		return nil, err
	}
	changed := false
	for index := range tasks {
		if tasks[index].Status == TaskRunning {
			tasks[index].Status = TaskFailed
			tasks[index].Error = "interrupted by process restart"
			tasks[index].UpdatedAt = time.Now().UTC()
			changed = true
		}
	}
	if changed {
		if err := board.write(tasks); err != nil {
			return nil, err
		}
	}
	return board, nil
}

func (board *Board) Create(ctx context.Context, task Task) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	task.ID = strings.TrimSpace(task.ID)
	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	release, err := filelock.Acquire(board.lockPath)
	if err != nil {
		return err
	}
	defer release()
	tasks, err := board.read()
	if err != nil {
		return err
	}
	if len(tasks) >= board.maxTasks {
		return fmt.Errorf("task board exceeds %d tasks", board.maxTasks)
	}
	for _, existing := range tasks {
		if existing.ID == task.ID {
			return fmt.Errorf("task %q already exists", task.ID)
		}
	}
	now := time.Now().UTC()
	task.Status, task.CreatedAt, task.UpdatedAt = TaskPending, now, now
	tasks = append(tasks, task)
	return board.write(tasks)
}

func (board *Board) Transition(ctx context.Context, id string, status TaskStatus, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	release, err := filelock.Acquire(board.lockPath)
	if err != nil {
		return err
	}
	defer release()
	tasks, err := board.read()
	if err != nil {
		return err
	}
	for index := range tasks {
		if tasks[index].ID != id {
			continue
		}
		if tasks[index].Status == status {
			return nil
		}
		if !validTaskTransition(tasks[index].Status, status) {
			return fmt.Errorf("invalid task transition %s -> %s", tasks[index].Status, status)
		}
		tasks[index].Status, tasks[index].Error, tasks[index].UpdatedAt = status, message, time.Now().UTC()
		return board.write(tasks)
	}
	return fmt.Errorf("task %q was not found", id)
}

func (board *Board) Get(ctx context.Context, id string) (Task, bool, error) {
	if err := ctx.Err(); err != nil {
		return Task{}, false, err
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	release, err := filelock.Acquire(board.lockPath)
	if err != nil {
		return Task{}, false, err
	}
	defer release()
	tasks, err := board.read()
	if err != nil {
		return Task{}, false, err
	}
	for _, task := range tasks {
		if task.ID == id {
			return task, true, nil
		}
	}
	return Task{}, false, nil
}

func (board *Board) List(ctx context.Context) ([]Task, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	board.mu.Lock()
	defer board.mu.Unlock()
	release, err := filelock.Acquire(board.lockPath)
	if err != nil {
		return nil, err
	}
	defer release()
	tasks, err := board.read()
	if err != nil {
		return nil, err
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].CreatedAt.Before(tasks[j].CreatedAt) })
	return tasks, nil
}

func validTaskTransition(from, to TaskStatus) bool {
	if from == TaskPending {
		return to == TaskRunning || to == TaskCancelled
	}
	if from == TaskRunning {
		return to == TaskCompleted || to == TaskFailed || to == TaskCancelled
	}
	return false
}

func (board *Board) read() ([]Task, error) {
	data, err := os.ReadFile(board.path)
	if os.IsNotExist(err) {
		return []Task{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, fmt.Errorf("decode task board: %w", err)
	}
	return tasks, nil
}

func (board *Board) write(tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(board.path), ".board-*")
	if err != nil {
		return err
	}
	path := temporary.Name()
	defer os.Remove(path)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return replaceBoardFile(path, board.path)
}

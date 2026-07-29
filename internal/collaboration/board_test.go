package collaboration

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestBoardPersistsTransitionsAndRecoversInterruptedWork(t *testing.T) {
	directory := t.TempDir()
	board, err := NewBoard(directory, BoardOptions{MaxTasks: 10})
	if err != nil {
		t.Fatal(err)
	}
	if err := board.Create(context.Background(), Task{ID: "task-1", Description: "review"}); err != nil {
		t.Fatal(err)
	}
	if err := board.Transition(context.Background(), "task-1", TaskRunning, ""); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewBoard(directory, BoardOptions{MaxTasks: 10})
	if err != nil {
		t.Fatal(err)
	}
	task, ok, err := reopened.Get(context.Background(), "task-1")
	if err != nil || !ok || task.Status != TaskFailed || task.Error != "interrupted by process restart" {
		t.Fatalf("task=%#v ok=%v err=%v", task, ok, err)
	}
}

func TestBoardRejectsInvalidStateTransition(t *testing.T) {
	board, err := NewBoard(t.TempDir(), BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := board.Create(context.Background(), Task{ID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	if err := board.Transition(context.Background(), "task-1", TaskCompleted, ""); err == nil {
		t.Fatal("pending task completed without running")
	}
}

func TestBoardInstancesDoNotOverwriteEachOther(t *testing.T) {
	directory := t.TempDir()
	first, err := NewBoard(directory, BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewBoard(directory, BoardOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 20)
	for index := range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			board := first
			if index%2 == 0 {
				board = second
			}
			errors <- board.Create(context.Background(), Task{ID: fmt.Sprintf("task-%d", index)})
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := first.List(context.Background())
	if err != nil || len(tasks) != 20 {
		t.Fatalf("task count=%d err=%v", len(tasks), err)
	}
}

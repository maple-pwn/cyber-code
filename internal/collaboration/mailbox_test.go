package collaboration

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestMailboxPersistsMessagesAndAckCursor(t *testing.T) {
	box, err := NewMailbox(t.TempDir(), Options{MaxMessages: 10})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.Send(context.Background(), Message{From: "parent", To: "worker", Body: "inspect file"}); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Send(context.Background(), Message{From: "parent", To: "worker", Body: "report result"}); err != nil {
		t.Fatal(err)
	}
	messages, err := box.Poll(context.Background(), "worker", 0, 10)
	if err != nil || len(messages) != 2 {
		t.Fatalf("messages=%#v err=%v", messages, err)
	}
	if err := box.Ack(context.Background(), "worker", messages[0].Sequence); err != nil {
		t.Fatal(err)
	}
	messages, err = box.Poll(context.Background(), "worker", 0, 10)
	if err != nil || len(messages) != 1 || messages[0].Body != "report result" {
		t.Fatalf("after ack=%#v err=%v", messages, err)
	}
}

func TestMailboxInstancesSerializeConcurrentSend(t *testing.T) {
	directory := t.TempDir()
	first, err := NewMailbox(directory, Options{MaxMessages: 50})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewMailbox(directory, Options{MaxMessages: 50})
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	errors := make(chan error, 20)
	for index := range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			box := first
			if index%2 == 0 {
				box = second
			}
			_, err := box.Send(context.Background(), Message{From: "leader", To: "worker", Body: fmt.Sprintf("message-%d", index)})
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	messages, err := first.Poll(context.Background(), "worker", 0, 50)
	if err != nil || len(messages) != 20 {
		t.Fatalf("message count=%d err=%v", len(messages), err)
	}
}

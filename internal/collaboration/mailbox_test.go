package collaboration

import (
	"context"
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

package controlplane

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"cyber-code/internal/core"
)

func TestParseCommandSupportsQuotedUnicodeAndEscapes(t *testing.T) {
	invocation, err := Parse(`/model "deepseek v4" --note=你好\ world`)
	if err != nil {
		t.Fatal(err)
	}
	if invocation.Name != "model" || !reflect.DeepEqual(invocation.Args, []string{"deepseek v4", "--note=你好 world"}) {
		t.Fatalf("invocation = %#v", invocation)
	}
}

func TestParseRejectsNonCommandsAndMalformedInput(t *testing.T) {
	if _, err := Parse("hello"); !errors.Is(err, ErrNotCommand) {
		t.Fatalf("non-command error = %v", err)
	}
	if _, err := Parse(`/status "unterminated`); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("malformed error = %v", err)
	}
}

func TestRegistryDispatchesAliasesAndHelpWithoutShell(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(Spec{
		Name: "status", Aliases: []string{"st"}, Usage: "/status", Description: "show status",
		Handler: func(_ context.Context, invocation Invocation) ([]core.Event, error) {
			return TextEvents(strings.Join(invocation.Args, ",")), nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Spec{Name: "help", Description: "show help", Handler: registry.Help}); err != nil {
		t.Fatal(err)
	}
	events, err := registry.Dispatch(context.Background(), `/st "one two"`)
	if err != nil || len(events) != 2 || events[0].Text != "one two" {
		t.Fatalf("dispatch = %#v, error = %v", events, err)
	}
	help, err := registry.Dispatch(context.Background(), "/help")
	if err != nil || len(help) != 2 || !strings.Contains(help[0].Text, "/status") {
		t.Fatalf("help = %#v, error = %v", help, err)
	}
}

func TestRegistryRejectsUnknownCommand(t *testing.T) {
	_, err := NewRegistry().Dispatch(context.Background(), "/missing")
	if !errors.Is(err, ErrUnknownCommand) {
		t.Fatalf("error = %v", err)
	}
}

func TestRegistryRegistrationIsAtomicOnAliasConflict(t *testing.T) {
	registry := NewRegistry()
	handler := func(context.Context, Invocation) ([]core.Event, error) { return nil, nil }
	if err := registry.Register(Spec{Name: "status", Aliases: []string{"st"}, Handler: handler}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(Spec{Name: "other", Aliases: []string{"st"}, Handler: handler}); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("conflict error = %v", err)
	}
	if _, exists := registry.Lookup("other"); exists {
		t.Fatal("failed registration left a canonical command behind")
	}
}

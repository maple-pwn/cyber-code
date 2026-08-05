package adapter

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceFactoryCreatesHonestDemoAndRealLocalSources(t *testing.T) {
	t.Parallel()

	factory, err := NewSourceFactory(SourceFactoryOptions{
		StateDir: t.TempDir(), Workspace: filepath.Join(t.TempDir(), "workspace"), ClientID: "tui-client", EnableRealSources: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	options := factory.Options()
	if len(options) != 2 || options[0].Name != "demo" || options[0].Mode != "demo" ||
		options[1].Name != "local" || options[1].Mode != "local" || !options[1].Available {
		t.Fatalf("source options = %#v", options)
	}

	selection, err := factory.Create("local")
	if err != nil {
		t.Fatal(err)
	}
	client := NewClient(selection.Source, ClientOptions{ClientID: "tui-client"})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := client.Dispatch(context.Background(), Command{
		Type: CommandTaskCreate, Objective: "Inspect workspace", RuntimeID: selection.RuntimeID,
	}); err != nil {
		t.Fatal(err)
	}
	view := client.View()
	if view.State.Task == nil || view.State.Scope == nil || view.State.CommittedCursor != 2 {
		t.Fatalf("local runtime view = %#v", view)
	}
}

func TestSourceFactoryCapabilityGatesRealSourcesByDefault(t *testing.T) {
	t.Parallel()
	factory, err := NewSourceFactory(SourceFactoryOptions{StateDir: t.TempDir(), Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	options := factory.Options()
	if len(options) != 2 || !options[0].Available || options[1].Available || options[1].SetupStatus == "" {
		t.Fatalf("gated source options = %#v", options)
	}
	if _, err := factory.Create("local"); err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("Create(local) error = %v", err)
	}
}

func TestSourceFactoryRejectsUnknownSourceWithSetupGuidance(t *testing.T) {
	t.Parallel()

	factory, err := NewSourceFactory(SourceFactoryOptions{StateDir: t.TempDir(), Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := factory.Create("remote"); err == nil || err.Error() != "runtime source remote is unavailable; configure a remote runtime endpoint first" {
		t.Fatalf("Create(remote) error = %v", err)
	}
}

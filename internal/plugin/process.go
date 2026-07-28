package plugin

import (
	"context"

	"cyber-code/internal/mcp"
)

type Process interface {
	Call(context.Context, string, any, any) error
	Close(context.Context) error
}

type ProcessFactory interface {
	Start(context.Context, Manifest) (Process, error)
}

type ProcessFactoryFunc func(context.Context, Manifest) (Process, error)

func (factory ProcessFactoryFunc) Start(ctx context.Context, manifest Manifest) (Process, error) {
	return factory(ctx, manifest)
}

type DefaultProcessOptions struct {
	MaxMessageBytes int
}

type DefaultProcessFactory struct {
	transport *mcp.DefaultTransportFactory
}

func NewDefaultProcessFactory(options DefaultProcessOptions) *DefaultProcessFactory {
	return &DefaultProcessFactory{transport: mcp.NewDefaultTransportFactory(mcp.DefaultTransportOptions{MaxMessageBytes: options.MaxMessageBytes})}
}

func (factory *DefaultProcessFactory) Start(ctx context.Context, manifest Manifest) (Process, error) {
	transport, err := factory.transport.Open(ctx, mcp.ServerConfig{
		Name: manifest.Name, Transport: mcp.TransportStdio, Command: manifest.Entrypoint,
		Args: manifest.Args, Workspace: manifest.Root,
	})
	if err != nil {
		return nil, err
	}
	return &managedProcess{transport: transport}, nil
}

type managedProcess struct{ transport mcp.Transport }

func (process *managedProcess) Call(ctx context.Context, method string, params, result any) error {
	return process.transport.Call(ctx, method, params, result)
}

func (process *managedProcess) Close(ctx context.Context) error {
	closed := make(chan error, 1)
	go func() { closed <- process.transport.Close() }()
	select {
	case err := <-closed:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type PluginContent struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolResult struct {
	Content []PluginContent `json:"content"`
	IsError bool            `json:"isError,omitempty"`
}

type CallToolParams struct {
	Name      string `json:"name"`
	Arguments any    `json:"arguments"`
}

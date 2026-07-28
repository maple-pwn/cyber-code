package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrProtocol         = errors.New("invalid MCP JSON-RPC response")
	ErrResponseTooLarge = errors.New("MCP response exceeds size limit")
)

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *jsonRPCError   `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type RPCError struct {
	Code    int
	Message string
}

func (rpcError *RPCError) Error() string {
	return fmt.Sprintf("MCP JSON-RPC error %d: %s", rpcError.Code, rpcError.Message)
}

type Transport interface {
	Call(context.Context, string, any, any) error
	Close() error
}

type TransportFactory interface {
	Open(context.Context, ServerConfig) (Transport, error)
}

type TransportFactoryFunc func(context.Context, ServerConfig) (Transport, error)

func (factory TransportFactoryFunc) Open(ctx context.Context, config ServerConfig) (Transport, error) {
	return factory(ctx, config)
}

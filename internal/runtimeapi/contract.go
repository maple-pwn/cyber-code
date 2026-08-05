// Package runtimeapi defines the transport-neutral CYBER runtime contract.
package runtimeapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode"
)

const ProtocolVersion = 1

var (
	ErrInvalidHandshake           = errors.New("invalid_handshake")
	ErrIncompatible               = errors.New("incompatible")
	ErrRuntimeIdentityMismatch    = errors.New("runtime_identity_mismatch")
	ErrInvalidCommandEnvelope     = errors.New("invalid_command_envelope")
	ErrInvalidCommandReceipt      = errors.New("invalid_command_receipt")
	ErrReceiptIdempotencyMismatch = errors.New("receipt_idempotency_mismatch")
)

type SourceMode string

const (
	SourceModeDemo   SourceMode = "demo"
	SourceModeLocal  SourceMode = "local"
	SourceModeRemote SourceMode = "remote"
)

type SourceMetadata struct {
	Mode         SourceMode `json:"mode"`
	RuntimeID    string     `json:"runtimeId"`
	Principal    string     `json:"principal"`
	Capabilities []string   `json:"capabilities"`
}

type HandshakeRequest struct {
	SupportedProtocolVersions []int `json:"supportedProtocolVersions"`
	AfterCursor               int   `json:"afterCursor"`
}

type HandshakeResponse struct {
	ProtocolVersion int            `json:"protocolVersion"`
	RuntimeID       string         `json:"runtimeId"`
	Principal       string         `json:"principal"`
	Role            string         `json:"role"`
	Capabilities    []string       `json:"capabilities"`
	Source          SourceMetadata `json:"source"`
}

type CommandEnvelope struct {
	IdempotencyKey string          `json:"idempotencyKey"`
	Command        json.RawMessage `json:"command"`
}

type CommandReceipt struct {
	IdempotencyKey string `json:"idempotencyKey"`
	Status         string `json:"status"`
	ErrorCode      string `json:"errorCode,omitempty"`
}

func NegotiateHandshake(request HandshakeRequest, response HandshakeResponse) (SourceMetadata, error) {
	if len(request.SupportedProtocolVersions) == 0 || request.AfterCursor < 0 {
		return SourceMetadata{}, ErrInvalidHandshake
	}
	for _, version := range request.SupportedProtocolVersions {
		if version <= 0 {
			return SourceMetadata{}, ErrInvalidHandshake
		}
	}
	if !slices.Contains(request.SupportedProtocolVersions, response.ProtocolVersion) {
		return SourceMetadata{}, ErrIncompatible
	}
	if !validIdentityText(response.RuntimeID) || !validIdentityText(response.Principal) || !validIdentityText(response.Role) ||
		!validStrings(response.Capabilities) || !validMetadata(response.Source) {
		return SourceMetadata{}, ErrInvalidHandshake
	}
	if response.Source.RuntimeID != response.RuntimeID || response.Source.Principal != response.Principal ||
		!slices.Equal(response.Source.Capabilities, response.Capabilities) {
		return SourceMetadata{}, ErrRuntimeIdentityMismatch
	}
	return response.Source, nil
}

func ValidateCommandEnvelope(envelope CommandEnvelope) error {
	if !validText(envelope.IdempotencyKey) || len(bytes.TrimSpace(envelope.Command)) == 0 {
		return ErrInvalidCommandEnvelope
	}
	var command map[string]json.RawMessage
	decoder := json.NewDecoder(bytes.NewReader(envelope.Command))
	if err := decoder.Decode(&command); err != nil || command == nil {
		return ErrInvalidCommandEnvelope
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrInvalidCommandEnvelope
	}
	commandType, ok := commandString(command, "type")
	if !ok {
		return ErrInvalidCommandEnvelope
	}
	valid := false
	switch commandType {
	case "task.create":
		valid = exactCommandKeys(command, []string{"type", "objective", "runtimeId"}, []string{"workspace"}) &&
			commandHasText(command, "objective") && commandHasText(command, "runtimeId") && commandOptionalText(command, "workspace")
	case "scope.confirm":
		valid = exactCommandKeys(command, []string{"type", "scopeId"}, nil) && commandHasText(command, "scopeId")
	case "task.pause", "task.resume", "task.cancel":
		valid = exactCommandKeys(command, []string{"type"}, nil)
	case "approval.respond":
		decision, decisionOK := commandString(command, "decision")
		valid = exactCommandKeys(command, []string{"type", "challengeId", "decision"}, nil) &&
			commandHasText(command, "challengeId") && decisionOK && (decision == "allow_once" || decision == "deny")
	case "control.take":
		var revision int
		valid = exactCommandKeys(command, []string{"type", "expectedRevision"}, nil) &&
			json.Unmarshal(command["expectedRevision"], &revision) == nil && revision >= 0
	case "instruction.send":
		valid = exactCommandKeys(command, []string{"type", "content"}, nil) && commandHasText(command, "content")
	}
	if !valid {
		return ErrInvalidCommandEnvelope
	}
	return nil
}

func ValidateCommandReceipt(receipt CommandReceipt, envelope CommandEnvelope) error {
	if err := ValidateCommandEnvelope(envelope); err != nil {
		return err
	}
	if !validText(receipt.IdempotencyKey) || (receipt.Status != "accepted" && receipt.Status != "rejected") {
		return ErrInvalidCommandReceipt
	}
	if receipt.IdempotencyKey != envelope.IdempotencyKey {
		return ErrReceiptIdempotencyMismatch
	}
	if (receipt.Status == "accepted" && receipt.ErrorCode != "") ||
		(receipt.Status == "rejected" && !validText(receipt.ErrorCode)) {
		return ErrInvalidCommandReceipt
	}
	return nil
}

func validMetadata(metadata SourceMetadata) bool {
	return (metadata.Mode == SourceModeDemo || metadata.Mode == SourceModeLocal || metadata.Mode == SourceModeRemote) &&
		validIdentityText(metadata.RuntimeID) && validIdentityText(metadata.Principal) && validStrings(metadata.Capabilities)
}

func exactCommandKeys(command map[string]json.RawMessage, required, optional []string) bool {
	allowed := make(map[string]struct{}, len(required)+len(optional))
	for _, key := range required {
		allowed[key] = struct{}{}
		if _, exists := command[key]; !exists {
			return false
		}
	}
	for _, key := range optional {
		allowed[key] = struct{}{}
	}
	for key := range command {
		if _, exists := allowed[key]; !exists {
			return false
		}
	}
	return true
}

func commandString(command map[string]json.RawMessage, key string) (string, bool) {
	var value string
	if json.Unmarshal(command[key], &value) != nil || !validText(value) {
		return "", false
	}
	return value, true
}

func commandHasText(command map[string]json.RawMessage, key string) bool {
	_, ok := commandString(command, key)
	return ok
}

func commandOptionalText(command map[string]json.RawMessage, key string) bool {
	if _, exists := command[key]; !exists {
		return true
	}
	return commandHasText(command, key)
}

func validStrings(values []string) bool {
	if values == nil {
		return false
	}
	for _, value := range values {
		if !validIdentityText(value) {
			return false
		}
	}
	return true
}

func validText(value string) bool {
	return strings.TrimSpace(value) != ""
}

func validIdentityText(value string) bool {
	if !validText(value) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return false
		}
	}
	return true
}

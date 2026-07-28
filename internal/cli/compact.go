package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"cyber-code/internal/core"
	"cyber-code/internal/provider"
	"cyber-code/internal/session"
)

const (
	defaultCompactThreshold = 100_000
	defaultCompactKeep      = 8
)

type compactSettings struct {
	ThresholdTokens    int `json:"threshold_tokens"`
	KeepRecentMessages int `json:"keep_recent_messages"`
}

func configureCompactor(stateDir, model string, modelProvider provider.Provider) (*session.Compactor, error) {
	settings := compactSettings{}
	if err := readStateFile(filepath.Join(stateDir, "compact.json"), &settings); err != nil {
		return nil, fmt.Errorf("load compact configuration: %w", err)
	}
	if settings.ThresholdTokens == 0 {
		settings.ThresholdTokens = defaultCompactThreshold
	}
	if settings.KeepRecentMessages == 0 {
		settings.KeepRecentMessages = defaultCompactKeep
	}
	return session.NewCompactor(session.CompactOptions{
		ThresholdTokens: settings.ThresholdTokens, KeepRecentMessages: settings.KeepRecentMessages,
		EstimateOnly: true, Summarize: providerSummarizer(modelProvider, model),
	})
}

func providerSummarizer(modelProvider provider.Provider, model string) session.SummarizeFunc {
	return func(ctx context.Context, messages []core.Message) (string, error) {
		if modelProvider == nil {
			return "", fmt.Errorf("compact provider is unavailable")
		}
		stream, err := modelProvider.Stream(ctx, core.Request{
			Model:    model,
			System:   []core.ContentBlock{{Type: core.ContentText, Text: "Summarize the conversation for continuation. Preserve decisions, changed files, commands, errors, and pending work. Return only the concise summary."}},
			Messages: messages,
		})
		if err != nil {
			return "", err
		}
		if stream == nil {
			return "", fmt.Errorf("compact provider returned a nil stream")
		}
		var summary strings.Builder
		for event := range stream {
			switch event.Type {
			case core.EventTextDelta:
				summary.WriteString(event.Text)
			case core.EventAssistantMessage:
				if summary.Len() == 0 && event.Message != nil {
					for _, block := range event.Message.Content {
						if block.Type == core.ContentText {
							summary.WriteString(block.Text)
						}
					}
				}
			case core.EventToolCall, core.EventToolArgumentsDelta:
				return "", fmt.Errorf("compact provider attempted to call a tool")
			case core.EventError:
				if event.Err != nil {
					return "", event.Err
				}
				return "", fmt.Errorf("compact provider failed")
			}
		}
		return strings.TrimSpace(summary.String()), nil
	}
}

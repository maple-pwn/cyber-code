package session

import (
	"context"
	"fmt"

	"cyber-code/internal/core"
	"cyber-code/internal/services"
)

type TokenCounter interface {
	CountTokens(context.Context, core.Request) (int, error)
}

type TokenCount struct {
	Tokens int
	Exact  bool
}

func CountRequestTokens(ctx context.Context, counter TokenCounter, request core.Request, estimate func(core.Request) int) TokenCount {
	if counter != nil {
		if count, err := counter.CountTokens(ctx, request); err == nil && count >= 0 {
			return TokenCount{Tokens: count, Exact: true}
		}
	}
	if estimate == nil {
		estimate = EstimateRequestTokens
	}
	count := estimate(request)
	if count < 0 {
		count = 0
	}
	return TokenCount{Tokens: count}
}

func EstimateRequestTokens(request core.Request) int {
	return services.EstimateCoreRequestTokens(request)
}

type ModelPricing struct {
	InputPerMillion      float64
	OutputPerMillion     float64
	CacheReadPerMillion  float64
	CacheWritePerMillion float64
}

type PricingTable map[string]ModelPricing

func (table PricingTable) Cost(model string, usage core.Usage) (float64, error) {
	pricing, ok := table[model]
	if !ok {
		return 0, fmt.Errorf("pricing is not configured for model %q", model)
	}
	return pricing.Cost(usage), nil
}

func (pricing ModelPricing) Cost(usage core.Usage) float64 {
	const million = 1_000_000
	return float64(usage.InputTokens)*pricing.InputPerMillion/million +
		float64(usage.OutputTokens)*pricing.OutputPerMillion/million +
		float64(usage.CacheReadInputTokens)*pricing.CacheReadPerMillion/million +
		float64(usage.CacheCreationInputTokens)*pricing.CacheWritePerMillion/million
}

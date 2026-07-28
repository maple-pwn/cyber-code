package permissions

import (
	"context"
	"fmt"
	"time"
)

type Request struct {
	Tool      string
	Action    string
	Workspace string
	Command   string
	Paths     []string
	Network   []string
}

type Decision struct {
	Behavior PermissionBehavior
	Reason   string
	RuleID   string
}

type Confirmer func(context.Context, Request) (Decision, error)

type Options struct {
	Mode       PermissionMode
	ModeSource PermissionRuleSource
	Rules      []Rule
	Confirmer  Confirmer
	Headless   bool
	Audit      AuditSink
	Parent     *Broker
}

type Broker struct {
	mode      PermissionMode
	rules     []Rule
	confirmer Confirmer
	headless  bool
	audit     AuditSink
	parent    *Broker
}

func NewBroker(options Options) (*Broker, error) {
	if options.Mode == "" {
		options.Mode = PermissionModeDefault
	}
	if err := validatePolicy(options.Mode, options.ModeSource, options.Rules); err != nil {
		return nil, err
	}
	return &Broker{
		mode:      options.Mode,
		rules:     append([]Rule(nil), options.Rules...),
		confirmer: options.Confirmer,
		headless:  options.Headless,
		audit:     options.Audit,
		parent:    options.Parent,
	}, nil
}

// Child creates a broker whose result is intersected with its parent policy.
func (broker *Broker) Child(mode PermissionMode, source PermissionRuleSource, rules []Rule) (*Broker, error) {
	return NewBroker(Options{
		Mode:       mode,
		ModeSource: source,
		Rules:      rules,
		Confirmer:  broker.confirmer,
		Headless:   broker.headless,
		Audit:      broker.audit,
		Parent:     broker,
	})
}

func (broker *Broker) Decide(ctx context.Context, request Request) (Decision, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Decision{}, err
	}
	decision, err := broker.decide(ctx, request)
	if err != nil {
		return Decision{}, err
	}
	if err := broker.record(request, decision); err != nil {
		return Decision{}, fmt.Errorf("record permission audit: %w", err)
	}
	return decision, nil
}

func (broker *Broker) decide(ctx context.Context, request Request) (Decision, error) {
	if err := validateRequest(request); err != nil {
		return Decision{Behavior: PermissionBehaviorDeny, Reason: "permission request is invalid", RuleID: "invalid-request"}, nil
	}
	if err := validateRequestPaths(request.Workspace, request.Paths); err != nil {
		return Decision{Behavior: PermissionBehaviorDeny, Reason: "filesystem request is outside the workspace", RuleID: "workspace-boundary"}, nil
	}

	decision := broker.localDecision(request)
	if decision.Behavior == PermissionBehaviorAsk {
		var err error
		decision, err = broker.confirm(ctx, request)
		if err != nil {
			return Decision{}, err
		}
	}
	if broker.parent == nil || decision.Behavior == PermissionBehaviorDeny {
		return decision, nil
	}
	parentDecision, err := broker.parent.decide(ctx, request)
	if err != nil {
		return Decision{}, err
	}
	if behaviorRank(parentDecision.Behavior) > behaviorRank(decision.Behavior) {
		return parentDecision, nil
	}
	return decision, nil
}

func validateRequest(request Request) error {
	if request.Tool == "" {
		return fmt.Errorf("tool is required")
	}
	if request.Command != "" && request.Action != ActionExecute {
		return fmt.Errorf("commands require execute action")
	}
	if len(request.Network) > 0 && request.Action != ActionNetwork && request.Action != ActionExecute {
		return fmt.Errorf("network targets require network or execute action")
	}
	switch request.Action {
	case ActionRead, ActionWrite, ActionExecute, ActionDelete, ActionNetwork:
		return nil
	default:
		return fmt.Errorf("unsupported action %q", request.Action)
	}
}

func (broker *Broker) localDecision(request Request) Decision {
	for _, rule := range broker.rules {
		if rule.Behavior == PermissionBehaviorDeny && ruleMatches(rule, request) {
			return Decision{Behavior: PermissionBehaviorDeny, Reason: "request denied by explicit rule", RuleID: rule.ID}
		}
	}
	for _, rule := range broker.rules {
		if rule.Behavior == PermissionBehaviorAllow && ruleMatches(rule, request) {
			return Decision{Behavior: PermissionBehaviorAllow, Reason: "request allowed by explicit rule", RuleID: rule.ID}
		}
	}
	for _, rule := range broker.rules {
		if rule.Behavior == PermissionBehaviorAsk && ruleMatches(rule, request) {
			return Decision{Behavior: PermissionBehaviorAsk, Reason: "explicit rule requires confirmation", RuleID: rule.ID}
		}
	}
	return modeDefault(broker.mode, request.Action)
}

func (broker *Broker) confirm(ctx context.Context, request Request) (Decision, error) {
	if broker.headless || broker.confirmer == nil {
		return Decision{Behavior: PermissionBehaviorDeny, Reason: "confirmation is unavailable"}, nil
	}
	decision, err := broker.confirmer(ctx, request)
	if err != nil {
		return Decision{}, fmt.Errorf("confirm permission: %w", err)
	}
	if decision.Behavior == PermissionBehaviorAllow {
		return Decision{Behavior: PermissionBehaviorAllow, Reason: "request confirmed by user"}, nil
	}
	return Decision{Behavior: PermissionBehaviorDeny, Reason: "request was not confirmed"}, nil
}

func behaviorRank(behavior PermissionBehavior) int {
	switch behavior {
	case PermissionBehaviorDeny:
		return 2
	case PermissionBehaviorAsk, PermissionBehaviorPassthrough:
		return 1
	default:
		return 0
	}
}

func (broker *Broker) record(request Request, decision Decision) error {
	if broker.audit == nil {
		return nil
	}
	return broker.audit.Record(AuditRecord{
		Time:         time.Now().UTC(),
		Tool:         request.Tool,
		Action:       request.Action,
		Behavior:     decision.Behavior,
		Reason:       decision.Reason,
		RuleID:       decision.RuleID,
		PathCount:    len(request.Paths),
		NetworkCount: len(request.Network),
	})
}

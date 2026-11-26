package aigateway

import (
	"context"
	"encoding/json"
	"time"
)

const (
	StageLLMInput      = "llm_input"
	StageToolPreInvoke = "tool_pre_invoke"
	StageToolPost      = "tool_post_invoke"
	StageLLMOutput     = "llm_output"
)

// AuditRecord captures every security/policy decision made by the 4-hook AI Gateway.
type AuditRecord struct {
	TraceID          string
	Source           string // opsagent | planner | companion
	GameID           string
	ActorID          string
	HookStage        string
	Decision         string // allow | block | redact | downgrade
	ReasonCode       string
	PIIRedactedCount int
	Detail           map[string]any
	CreatedAt        int64
}

// AuditSink persists gateway audit logs and increments Prometheus counters.
type AuditSink interface {
	RecordAIGatewayAudit(ctx context.Context, rec AuditRecord)
}

// Session holds per-request state across all 4 hooks of a single AI interaction.
type Session struct {
	TraceID   string
	Source    string
	GameID    string
	ActorID   string
	Vault     *PIIVault
	LoopGuard *LoopGuard
	Corpus    *EvidenceCorpus
	Sink      AuditSink
}

func NewSession(traceID, source, gameID, actorID string, sink AuditSink, initialContext ...string) *Session {
	return &Session{
		TraceID:   traceID,
		Source:    source,
		GameID:    gameID,
		ActorID:   actorID,
		Vault:     NewPIIVault(),
		LoopGuard: NewLoopGuard(),
		Corpus:    NewEvidenceCorpus(initialContext...),
		Sink:      sink,
	}
}

func (s *Session) emit(ctx context.Context, stage, decision, reason string, piiCount int, detail map[string]any) {
	if s == nil || s.Sink == nil {
		return
	}
	s.Sink.RecordAIGatewayAudit(ctx, AuditRecord{
		TraceID:          s.TraceID,
		Source:           s.Source,
		GameID:           s.GameID,
		ActorID:          s.ActorID,
		HookStage:        stage,
		Decision:         decision,
		ReasonCode:       reason,
		PIIRedactedCount: piiCount,
		Detail:           detail,
		CreatedAt:        time.Now().Unix(),
	})
}

// HookLLMInput checks for prompt injection and redacts PII before sending text to an external LLM.
func (s *Session) HookLLMInput(ctx context.Context, text string) (masked string, blockedReason string) {
	if blocked, reason := DetectPromptInjection(text); blocked {
		s.emit(ctx, StageLLMInput, "block", reason, 0, nil)
		return "", reason
	}
	out, n := s.Vault.Mask(text)
	if n > 0 {
		s.emit(ctx, StageLLMInput, "redact", "pii_masked", n, nil)
	}
	return out, ""
}

// HookToolPreInvoke enforces JSON Schema, OPA-like policy bounds, and duplicate-call loop breaking.
func (s *Session) HookToolPreInvoke(ctx context.Context, toolName string, schema map[string]any, rawArgs json.RawMessage) (allowed bool, reasonCode string) {
	if ok, reason := s.LoopGuard.CheckAndRecord(toolName, rawArgs); !ok {
		s.emit(ctx, StageToolPreInvoke, "block", reason, 0, map[string]any{"tool": toolName})
		return false, reason
	}
	if err := ValidateArgsAgainstSchema(toolName, schema, rawArgs); err != nil {
		s.emit(ctx, StageToolPreInvoke, "block", "schema_validation_failed", 0, map[string]any{"tool": toolName, "error": err.Error()})
		return false, "schema_validation_failed"
	}
	if ok, reason := CheckToolPolicy(toolName, rawArgs); !ok {
		s.emit(ctx, StageToolPreInvoke, "block", reason, 0, map[string]any{"tool": toolName})
		return false, reason
	}
	return true, ""
}

// HookToolPostInvoke records authentic tool evidence, masks PII, and structurally compacts oversized JSON.
func (s *Session) HookToolPostInvoke(ctx context.Context, toolName, rawOutput string, maxBytes int) string {
	s.Corpus.Add(rawOutput)
	masked, n := s.Vault.Mask(rawOutput)
	if n > 0 {
		s.emit(ctx, StageToolPost, "redact", "tool_pii_masked", n, map[string]any{"tool": toolName})
	}
	return CompactToolJSON(masked, maxBytes)
}

// HookLLMOutput verifies groundedness against authentic tool evidence and unmasks safe PII placeholders.
func (s *Session) HookLLMOutput(ctx context.Context, summary, rootCause, rec string, evidenceIDs []string) (outSum, outRoot, outRec string, downgraded bool, reason string) {
	if ok, why := VerifyGroundedness(summary, rootCause, rec, evidenceIDs, s.Corpus); !ok {
		s.emit(ctx, StageLLMOutput, "downgrade", why, 0, map[string]any{"evidence_ids": evidenceIDs})
		return summary, rootCause, rec, true, why
	}
	return s.Vault.Unmask(summary), s.Vault.Unmask(rootCause), s.Vault.Unmask(rec), false, ""
}

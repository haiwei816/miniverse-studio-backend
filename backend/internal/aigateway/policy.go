package aigateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const MaxAIRecordRewardAmount = int64(10000)

// ValidateArgsAgainstSchema compiles and validates raw JSON tool arguments against a JSON Schema map.
func ValidateArgsAgainstSchema(toolName string, schema map[string]any, rawArgs []byte) error {
	if len(schema) == 0 {
		return nil
	}
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("invalid tool schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	resName := toolName + ".schema.json"
	if err := c.AddResource(resName, bytes.NewReader(schemaBytes)); err != nil {
		return fmt.Errorf("compile schema resource: %w", err)
	}
	compiled, err := c.Compile(resName)
	if err != nil {
		return fmt.Errorf("compile tool schema: %w", err)
	}
	var val any
	dec := json.NewDecoder(bytes.NewReader(rawArgs))
	dec.UseNumber()
	if err := dec.Decode(&val); err != nil {
		return fmt.Errorf("invalid_json_arguments: %w", err)
	}
	if err := compiled.Validate(val); err != nil {
		return fmt.Errorf("schema_validation_failed: %w", err)
	}
	return nil
}

// CheckToolPolicy enforces deterministic OPA-like business safety rules before tool execution.
func CheckToolPolicy(toolName string, rawArgs []byte) (bool, string) {
	switch toolName {
	case "propose_reward_compensation":
		var p struct {
			Amount int64  `json:"amount"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(rawArgs, &p); err != nil {
			return false, "invalid_tool_arguments"
		}
		if p.Amount > MaxAIRecordRewardAmount {
			return false, "policy_reward_amount_exceeded"
		}
		if len(strings.TrimSpace(p.Reason)) < 3 {
			return false, "policy_reason_required"
		}
	case "propose_player_freeze":
		var p struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(rawArgs, &p); err != nil || len(strings.TrimSpace(p.Reason)) < 3 {
			return false, "policy_reason_required"
		}
	}
	return true, ""
}

// CanonicalArgsHash normalizes JSON arguments so semantically identical calls share the same hash.
func CanonicalArgsHash(rawArgs []byte) string {
	var v any
	if err := json.Unmarshal(rawArgs, &v); err == nil {
		if norm, err := json.Marshal(v); err == nil {
			sum := sha256.Sum256(norm)
			return hex.EncodeToString(sum[:16])
		}
	}
	sum := sha256.Sum256(bytes.TrimSpace(rawArgs))
	return hex.EncodeToString(sum[:16])
}

// LoopGuard trips when an Agent invokes the exact same tool with identical arguments repeatedly.
type LoopGuard struct {
	mu     sync.Mutex
	counts map[string]int
}

func NewLoopGuard() *LoopGuard {
	return &LoopGuard{counts: make(map[string]int)}
}

// CheckAndRecord returns false with reason "loop_circuit_broken" on duplicate identical tool+args invocation.
func (g *LoopGuard) CheckAndRecord(toolName string, rawArgs []byte) (bool, string) {
	if g == nil {
		return true, ""
	}
	key := toolName + ":" + CanonicalArgsHash(rawArgs)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.counts[key]++
	if g.counts[key] >= 2 {
		return false, "loop_circuit_broken"
	}
	return true, ""
}

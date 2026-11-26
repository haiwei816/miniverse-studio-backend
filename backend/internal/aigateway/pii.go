// Package aigateway implements a unified 4-hook AI Security Gateway
// (LLMInput, ToolPreInvoke, ToolPostInvoke, LLMOutput) shared across
// opsagent, planner, and companion modules.
package aigateway

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	emailRe  = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	phoneRe  = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	ipv4Re   = regexp.MustCompile(`\b(?:(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\.){3}(?:25[0-5]|2[0-4]\d|[01]?\d\d?)\b`)
	secretRe = regexp.MustCompile(`(?i)\b(?:sk-[A-Za-z0-9_-]{8,}|Bearer\s+[A-Za-z0-9._-]{12,})\b`)
)

// PIIVault stores reversible placeholder mappings for a single AI gateway session.
type PIIVault struct {
	forward map[string]string
	reverse map[string]string
	counts  map[string]int
}

func NewPIIVault() *PIIVault {
	return &PIIVault{
		forward: make(map[string]string),
		reverse: make(map[string]string),
		counts:  make(map[string]int),
	}
}

func (v *PIIVault) redactToken(kind, raw string) string {
	if ph, ok := v.forward[raw]; ok {
		return ph
	}
	v.counts[kind]++
	ph := fmt.Sprintf("<PII_%s_%d>", kind, v.counts[kind])
	v.forward[raw] = ph
	if kind != "SECRET" {
		v.reverse[ph] = raw
	}
	return ph
}

// Mask replaces PII and secrets before outbound LLM transmission and returns the redaction count.
func (v *PIIVault) Mask(text string) (string, int) {
	if text == "" || v == nil {
		return text, 0
	}
	redacted := 0
	replace := func(re *regexp.Regexp, kind, src string, keep func(string) bool) string {
		return re.ReplaceAllStringFunc(src, func(match string) string {
			if keep != nil && keep(match) {
				return match
			}
			redacted++
			return v.redactToken(kind, match)
		})
	}
	out := replace(secretRe, "SECRET", text, nil)
	out = replace(emailRe, "EMAIL", out, nil)
	out = replace(phoneRe, "PHONE", out, nil)
	out = replace(ipv4Re, "IP", out, func(ip string) bool {
		return ip == "127.0.0.1" || ip == "0.0.0.0"
	})
	return out, redacted
}

// Unmask restores non-secret PII placeholders in the final user-facing answer.
func (v *PIIVault) Unmask(text string) string {
	if v == nil || text == "" || len(v.reverse) == 0 {
		return text
	}
	out := text
	for ph, raw := range v.reverse {
		out = strings.ReplaceAll(out, ph, raw)
	}
	return out
}

var injectionNeedles = []string{
	"ignore previous instructions",
	"ignore all previous",
	"disregard system prompt",
	"bypass approval",
	"skip human review",
	"忽略之前的所有指令",
	"忽略系统提示",
	"跳过审批直接发",
	"直接修改数据库",
}

// DetectPromptInjection flags obvious prompt-override attempts trying to bypass RBAC or human approval.
func DetectPromptInjection(text string) (bool, string) {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, needle := range injectionNeedles {
		if strings.Contains(lower, needle) {
			return true, "prompt_injection_detected"
		}
	}
	return false, ""
}

package aigateway

import (
	"regexp"
	"strings"
)

var hexIDFinder = regexp.MustCompile(`\b[a-f0-9]{24}\b`)

// EvidenceCorpus holds all authentic strings observed from tool results & initial request context.
type EvidenceCorpus struct {
	blobs []string
}

func NewEvidenceCorpus(initialContext ...string) *EvidenceCorpus {
	return &EvidenceCorpus{blobs: append([]string(nil), initialContext...)}
}

func (c *EvidenceCorpus) Add(text string) {
	if c != nil && text != "" {
		c.blobs = append(c.blobs, text)
	}
}

func (c *EvidenceCorpus) contains(id string) bool {
	if id == "" {
		return true
	}
	for _, b := range c.blobs {
		if strings.Contains(b, id) {
			return true
		}
	}
	return false
}

var forbiddenWriteClaims = []string{
	"已直接发放",
	"已直接补发",
	"已立即发放",
	"无需审核已生效",
	"granted directly without approval",
}

// VerifyGroundedness validates that the LLM final output does not fabricate request/evidence IDs
// or claim direct mutations when write tools only produce pending approval proposals.
func VerifyGroundedness(summary, rootCause, recommendation string, evidenceIDs []string, corpus *EvidenceCorpus) (bool, string) {
	combined := summary + "\n" + rootCause + "\n" + recommendation
	lower := strings.ToLower(combined)
	for _, phrase := range forbiddenWriteClaims {
		if strings.Contains(lower, phrase) {
			return false, "ungrounded_write_claim"
		}
	}
	if corpus == nil {
		return true, ""
	}
	for _, eid := range evidenceIDs {
		trimmed := strings.TrimSpace(eid)
		if trimmed != "" && !corpus.contains(trimmed) {
			return false, "hallucinated_evidence_id"
		}
	}
	for _, id := range hexIDFinder.FindAllString(combined, -1) {
		if !corpus.contains(id) {
			return false, "hallucinated_request_id"
		}
	}
	return true, ""
}

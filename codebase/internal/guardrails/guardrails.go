package guardrails

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ra1/model-gateway/internal/schema"
)

var (
	ErrGuardrailBlocked = errors.New("request blocked by content safety guardrail")
)

// Action defines what happens when a rule triggers.
type Action string

const (
	ActionMask  Action = "mask"
	ActionBlock Action = "block"
)

var (
	reSSN        = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)
	reEmail      = regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b`)
	reIPv4       = regexp.MustCompile(`\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)
	reCreditCard = regexp.MustCompile(`\b(?:\d{4}[ -]?){3}\d{4}\b|\b\d{13,16}\b`)

	// Secrets
	reAWSKey     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
	rePrivateKey = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)
	reBearerKey  = regexp.MustCompile(`\bsk-[a-zA-Z0-9]{24,}\b`)

	// Injection heuristics
	injectionPhrases = []string{
		"ignore previous instructions",
		"ignore all previous instructions",
		"disregard all instructions above",
		"dan mode",
		"jailbreak mode",
		"developer mode enabled",
		"you are no longer an ai",
	}
)

// ValidateLuhn verifies credit card numbers with the standard Luhn checksum.
func ValidateLuhn(number string) bool {
	digits := make([]int, 0, len(number))
	for _, ch := range number {
		if ch >= '0' && ch <= '9' {
			digits = append(digits, int(ch-'0'))
		}
	}
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}

	sum := 0
	alt := false
	for i := len(digits) - 1; i >= 0; i-- {
		n := digits[i]
		if alt {
			n *= 2
			if n > 9 {
				n -= 9
			}
		}
		sum += n
		alt = !alt
	}
	return sum%10 == 0
}

// Config configures guardrail behaviors.
type Config struct {
	PIIAction       Action
	SecretsAction   Action
	InjectionAction Action
}

// DefaultConfig provides recommended settings.
func DefaultConfig() Config {
	return Config{
		PIIAction:       ActionMask,
		SecretsAction:   ActionBlock,
		InjectionAction: ActionBlock,
	}
}

// InspectionResult holds guardrail findings.
type InspectionResult struct {
	Blocked    bool
	Reason     string
	Violations []string
}

// Manager orchestrates pre-call and post-call content inspection.
type Manager struct {
	cfg Config
}

// NewManager initializes the guardrails manager.
func NewManager(cfg Config) *Manager {
	if cfg.PIIAction == "" {
		cfg.PIIAction = ActionMask
	}
	if cfg.SecretsAction == "" {
		cfg.SecretsAction = ActionBlock
	}
	if cfg.InjectionAction == "" {
		cfg.InjectionAction = ActionBlock
	}
	return &Manager{cfg: cfg}
}

// InspectInput validates and moderates the incoming request prompt before dispatch.
func (m *Manager) InspectInput(ctx context.Context, req *schema.CanonicalChatRequest) (*InspectionResult, error) {
	res := &InspectionResult{Violations: make([]string, 0)}

	for i := range req.Messages {
		content := req.Messages[i].Content

		// 1. Prompt Injection detection
		contentLower := strings.ToLower(content)
		for _, phrase := range injectionPhrases {
			if strings.Contains(contentLower, phrase) {
				res.Violations = append(res.Violations, "prompt_injection")
				if m.cfg.InjectionAction == ActionBlock {
					res.Blocked = true
					res.Reason = fmt.Sprintf("Prompt injection phrase detected: '%s'", phrase)
					return res, fmt.Errorf("%w: %s", ErrGuardrailBlocked, res.Reason)
				}
			}
		}

		// 2. Secret Scanning
		if reAWSKey.MatchString(content) || rePrivateKey.MatchString(content) || reBearerKey.MatchString(content) {
			res.Violations = append(res.Violations, "secrets_detected")
			if m.cfg.SecretsAction == ActionBlock {
				res.Blocked = true
				res.Reason = "Request contains sensitive cryptographic or API keys"
				return res, fmt.Errorf("%w: %s", ErrGuardrailBlocked, res.Reason)
			}
		}

		// 3. PII Detection & Masking
		sanitized := content
		if reSSN.MatchString(sanitized) {
			res.Violations = append(res.Violations, "pii_ssn")
			if m.cfg.PIIAction == ActionBlock {
				res.Blocked = true
				res.Reason = "Request contains Social Security Number (SSN)"
				return res, fmt.Errorf("%w: %s", ErrGuardrailBlocked, res.Reason)
			}
			sanitized = reSSN.ReplaceAllString(sanitized, "[REDACTED_SSN]")
		}

		if reEmail.MatchString(sanitized) {
			res.Violations = append(res.Violations, "pii_email")
			if m.cfg.PIIAction == ActionBlock {
				res.Blocked = true
				res.Reason = "Request contains sensitive email address"
				return res, fmt.Errorf("%w: %s", ErrGuardrailBlocked, res.Reason)
			}
			sanitized = reEmail.ReplaceAllString(sanitized, "[REDACTED_EMAIL]")
		}

		// Credit card with Luhn confirmation
		sanitized = reCreditCard.ReplaceAllStringFunc(sanitized, func(match string) string {
			clean := strings.ReplaceAll(strings.ReplaceAll(match, " ", ""), "-", "")
			if _, err := strconv.ParseUint(clean, 10, 64); err == nil && ValidateLuhn(clean) {
				res.Violations = append(res.Violations, "pii_credit_card")
				return "[REDACTED_CC]"
			}
			return match
		})

		req.Messages[i].Content = sanitized
	}

	return res, nil
}

// InspectOutput inspects completed responses for sensitive data leakage.
func (m *Manager) InspectOutput(ctx context.Context, resp *schema.CanonicalChatResponse) (*InspectionResult, error) {
	res := &InspectionResult{Violations: make([]string, 0)}

	for i := range resp.Choices {
		content := resp.Choices[i].Message.Content

		// Check for accidental private key leakage
		if rePrivateKey.MatchString(content) || reAWSKey.MatchString(content) {
			res.Violations = append(res.Violations, "secret_leakage_prevented")
			resp.Choices[i].Message.Content = "[Output redacted by model-gateway security policy: internal secret detected]"
		}

		// PII sanitization in output
		if reSSN.MatchString(content) {
			res.Violations = append(res.Violations, "pii_ssn_redacted")
			resp.Choices[i].Message.Content = reSSN.ReplaceAllString(resp.Choices[i].Message.Content, "[REDACTED_SSN]")
		}
	}

	return res, nil
}

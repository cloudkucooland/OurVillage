package model

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// DefaultAgentIntentGrammar returns the built-in canonical GBNF grammar for RFC 002 agent intents.
func DefaultAgentIntentGrammar() string {
	return `root ::= "{" ws "\"thought\"" ws ":" ws string ws "," ws "\"action\"" ws ":" ws action-type ws "," ws "\"payload\"" ws ":" ws payload ws "}"
action-type ::= "\"MOVE\"" | "\"CHAT\"" | "\"TRANSFER\"" | "\"GIVE\"" | "\"FORAGE\"" | "\"COLLECT\"" | "\"CANCEL_ACTION\"" | "\"DROP\"" | "\"PICKUP\"" | "\"EQUIP\"" | "\"UNEQUIP\"" | "\"CRAFT\"" | "\"LOOK\"" | "\"STATUS\"" | "\"BUY\"" | "\"RESTOCK\"" | "\"EAT\"" | "\"REST\""
payload ::= move-payload | chat-payload | transfer-payload | forage-payload | empty-payload | item-payload | craft-payload | buy-payload | restock-payload | rest-payload
move-payload ::= "{" ws "\"target_location\"" ws ":" ws string ws "}"
chat-payload ::= "{" ws "\"message\"" ws ":" ws string ws "}"
transfer-payload ::= "{" ws "\"recipient_id\"" ws ":" ws string ws "," ws "\"amount\"" ws ":" ws integer ws ("," ws "\"item_id\"" ws ":" ws string ws "," ws "\"quantity\"" ws ":" ws integer ws)? "}"
forage-payload ::= "{" ws "\"target_resource\"" ws ":" ws string ws ("," ws "\"duration_ticks\"" ws ":" ws integer ws)? "}"
empty-payload ::= "{" ws "}"
item-payload ::= "{" ws "\"item_id\"" ws ":" ws string ws ("," ws "\"quantity\"" ws ":" ws integer ws)? "}"
craft-payload ::= "{" ws "\"recipe_id\"" ws ":" ws string ws "}"
buy-payload ::= "{" ws "\"item_id\"" ws ":" ws string ws "," ws "\"quantity\"" ws ":" ws integer ws "}"
restock-payload ::= "{" ws "\"item_id\"" ws ":" ws string ws "," ws "\"quantity\"" ws ":" ws integer ws ("," ws "\"price_gold\"" ws ":" ws integer ws)? "}"
rest-payload ::= "{" ws ("\"duration_ticks\"" ws ":" ws integer ws)? "}"
string ::= "\"" ([^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
integer ::= "-"? [0-9]+
ws ::= [ \t\n\r]*`
}

var (
	gbnfRuleRegex       = regexp.MustCompile(`^[a-zA-Z0-9_\-]+(\s*::=|\s*\+=)`)
	codeBlockRegex      = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")
	unquotedActionRegex = regexp.MustCompile(`("action"\s*:\s*)([a-zA-Z_]+)([\s,\}])`)
)

// ValidateGBNF validates the structural syntax of a GBNF grammar string.
func ValidateGBNF(grammar string) error {
	trimmed := strings.TrimSpace(grammar)
	if trimmed == "" {
		return fmt.Errorf("gbnf grammar cannot be empty")
	}

	lines := strings.Split(trimmed, "\n")
	hasRoot := false
	ruleCount := 0

	for lineIdx, line := range lines {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue // comment or blank line
		}

		if strings.Contains(l, "::=") || strings.Contains(l, "+=") {
			if !gbnfRuleRegex.MatchString(l) {
				return fmt.Errorf("syntax error on line %d: invalid rule definition %q", lineIdx+1, l)
			}
			ruleName := strings.TrimSpace(strings.Split(strings.Split(l, "::=")[0], "+=")[0])
			if ruleName == "root" {
				hasRoot = true
			}
			ruleCount++
		}
	}

	if ruleCount == 0 {
		return fmt.Errorf("no valid GBNF rules found")
	}
	if !hasRoot {
		return fmt.Errorf("gbnf grammar must define a 'root' rule")
	}

	return nil
}

// LoadGrammarFile reads a GBNF grammar from the filesystem and validates it.
func LoadGrammarFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read grammar file %q: %w", path, err)
	}

	content := string(data)
	if err := ValidateGBNF(content); err != nil {
		return "", fmt.Errorf("invalid grammar in %q: %w", path, err)
	}

	return content, nil
}

// ParsedIntent represents an intent object extracted from model outputs.
type ParsedIntent struct {
	Thought string          `json:"thought,omitempty"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload"`
}

var validActions = map[string]struct{}{
	"MOVE":          {},
	"CHAT":          {},
	"TRANSFER":      {},
	"GIVE":          {},
	"FORAGE":        {},
	"COLLECT":       {},
	"CANCEL_ACTION": {},
	"DROP":          {},
	"PICKUP":        {},
	"EQUIP":         {},
	"UNEQUIP":       {},
	"CRAFT":         {},
	"LOOK":          {},
	"STATUS":        {},
	"BUY":           {},
	"RESTOCK":       {},
	"EAT":           {},
	"REST":          {},
}

// SanitizeAndExtractJSON extracts and cleans valid JSON blocks from model responses,
// repairing markdown code fences, fixing lowercase action enums, and validating RFC 002 compliance.
func SanitizeAndExtractJSON(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("empty model response")
	}

	// 1. Check for markdown code fences (```json ... ```)
	if matches := codeBlockRegex.FindStringSubmatch(trimmed); len(matches) > 1 {
		trimmed = strings.TrimSpace(matches[1])
	}

	// 2. Find outermost JSON object braces { ... }
	startIdx := strings.Index(trimmed, "{")
	endIdx := strings.LastIndex(trimmed, "}")
	if startIdx == -1 || endIdx == -1 || endIdx <= startIdx {
		return "", fmt.Errorf("no valid JSON object found in model output: %s", raw)
	}

	jsonCandidate := trimmed[startIdx : endIdx+1]

	// 3. Unmarshal into generic map to inspect and repair
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(jsonCandidate), &data); err != nil {
		repairedCandidate := unquotedActionRegex.ReplaceAllString(jsonCandidate, `$1"$2"$3`)
		if err2 := json.Unmarshal([]byte(repairedCandidate), &data); err2 != nil {
			return "", fmt.Errorf("malformed JSON in model output: %w (candidate: %s)", err, jsonCandidate)
		}
		jsonCandidate = repairedCandidate
	}

	// 4. Validate and normalize the "action" field
	rawAction, ok := data["action"]
	if !ok || rawAction == nil {
		return "", fmt.Errorf("missing 'action' field in model JSON: %s", jsonCandidate)
	}

	actionStr, ok := rawAction.(string)
	if !ok {
		return "", fmt.Errorf("action field must be a string, got %T", rawAction)
	}

	actionUpper := strings.ToUpper(strings.TrimSpace(actionStr))
	if _, valid := validActions[actionUpper]; !valid {
		return "", fmt.Errorf("invalid action %q in model output (expected one of MOVE, CHAT, TRANSFER, GIVE, FORAGE, COLLECT, CANCEL_ACTION, DROP, PICKUP, EQUIP, UNEQUIP, CRAFT, BUY, RESTOCK, EAT, REST)", actionStr)
	}

	if actionUpper == "COLLECT" {
		actionUpper = "FORAGE"
	}
	if actionUpper == "GIVE" {
		actionUpper = "TRANSFER"
	}

	// Update action to uppercase
	data["action"] = actionUpper

	// If payload is missing, provide empty object
	if _, ok := data["payload"]; !ok {
		data["payload"] = map[string]interface{}{}
	}

	// Re-serialize clean normalized JSON
	cleanBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to remarshal clean JSON: %w", err)
	}

	return string(cleanBytes), nil
}

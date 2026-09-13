package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type StateSection string

const (
	StateSectionEndpoints StateSection = "endpoints"
	StateSectionFindings  StateSection = "findings"
	StateSectionAuth      StateSection = "auth"
	StateSectionTried     StateSection = "tried"
)

type StateAction string

const (
	StateActionAdd    StateAction = "add"
	StateActionUpdate StateAction = "update"
	StateActionRemove StateAction = "remove"
)

type Endpoint struct {
	Path         string   `json:"path"`
	Method       string   `json:"method"`
	Params       []string `json:"params,omitempty"`
	AuthRequired bool     `json:"auth_required"`
	TestedFor    []string `json:"tested_for,omitempty"`
	Status       string   `json:"status"`
	Notes        string   `json:"notes,omitempty"`
}

type Finding struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Endpoint       string          `json:"endpoint"`
	Severity       string          `json:"severity"`
	Status         string          `json:"status"`
	Evidence       FindingEvidence `json:"evidence"`
	ChainPotential []string       `json:"chain_potential,omitempty"`
}

type FindingEvidence struct {
	Command  string `json:"command"`
	Response string `json:"response"`
}

type AuthEntry struct {
	Name      string `json:"name"`
	Token     string `json:"token"`
	Type      string `json:"type"`
	ObtainedAt string `json:"obtained_at"`
	ExpiresIn  int    `json:"expires_in"`
}

type TriedApproach struct {
	Approach  string `json:"approach"`
	Result    string `json:"result"`
	Endpoint  string `json:"endpoint,omitempty"`
	Timestamp string `json:"timestamp"`
}

type CoverageState struct {
	mu        sync.RWMutex
	Endpoints []Endpoint      `json:"endpoints"`
	Findings  []Finding       `json:"findings"`
	Auth      []AuthEntry     `json:"auth"`
	Tried     []TriedApproach `json:"tried"`
}

func NewCoverageState() *CoverageState {
	return &CoverageState{
		Endpoints: make([]Endpoint, 0),
		Findings:  make([]Finding, 0),
		Auth:      make([]AuthEntry, 0),
		Tried:     make([]TriedApproach, 0),
	}
}

func (cs *CoverageState) HandleStateUpdate(_ context.Context, _ string, args json.RawMessage) (string, error) {
	var action StateUpdateAction
	if err := json.Unmarshal(args, &action); err != nil {
		return fmt.Sprintf("Invalid arguments: %v. Expected: {section, action, data, message}", err), nil
	}

	section := StateSection(strings.TrimSpace(string(action.Section)))
	op := StateAction(strings.TrimSpace(string(action.Action)))

	switch section {
	case StateSectionEndpoints:
		return cs.handleEndpointUpdate(op, action.Data)
	case StateSectionFindings:
		return cs.handleFindingUpdate(op, action.Data)
	case StateSectionAuth:
		return cs.handleAuthUpdate(op, action.Data)
	case StateSectionTried:
		return cs.handleTriedUpdate(op, action.Data)
	default:
		return fmt.Sprintf("Unknown section '%s'. Valid sections: endpoints, findings, auth, tried", section), nil
	}
}

func (cs *CoverageState) HandleGetState(_ context.Context, _ string, args json.RawMessage) (string, error) {
	var query GetStateAction
	if err := json.Unmarshal(args, &query); err != nil {
		return fmt.Sprintf("Invalid arguments: %v. Expected: {section, filter, message}", err), nil
	}

	section := StateSection(strings.TrimSpace(string(query.Section)))

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	switch section {
	case StateSectionEndpoints:
		return cs.getEndpointsView(string(query.Filter))
	case StateSectionFindings:
		return cs.getFindingsView(string(query.Filter))
	case StateSectionAuth:
		return cs.getAuthView()
	case StateSectionTried:
		return cs.getTriedView(string(query.Filter))
	case "coverage":
		return cs.getCoverageSummary()
	case "all":
		return cs.getCoverageSummary()
	default:
		return fmt.Sprintf("Unknown section '%s'. Valid sections: endpoints, findings, auth, tried, coverage, all", section), nil
	}
}

func (cs *CoverageState) handleEndpointUpdate(op StateAction, data json.RawMessage) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var ep Endpoint
	if err := json.Unmarshal(data, &ep); err != nil {
		return fmt.Sprintf("Invalid endpoint data: %v. Expected: {path, method, params, auth_required, status, notes}", err), nil
	}
	if ep.Path == "" || ep.Method == "" {
		return "Endpoint requires at least 'path' and 'method' fields", nil
	}
	if ep.Status == "" {
		ep.Status = "discovered"
	}

	switch op {
	case StateActionAdd:
		for i, existing := range cs.Endpoints {
			if existing.Path == ep.Path && existing.Method == ep.Method {
				if len(ep.Params) > 0 {
					cs.Endpoints[i].Params = mergeStringSlices(existing.Params, ep.Params)
				}
				if ep.Notes != "" {
					cs.Endpoints[i].Notes = ep.Notes
				}
				if len(ep.TestedFor) > 0 {
					cs.Endpoints[i].TestedFor = mergeStringSlices(existing.TestedFor, ep.TestedFor)
				}
				if ep.Status != "" {
					cs.Endpoints[i].Status = ep.Status
				}
				return fmt.Sprintf("Updated existing endpoint %s %s", ep.Method, ep.Path), nil
			}
		}
		cs.Endpoints = append(cs.Endpoints, ep)
		return fmt.Sprintf("Added endpoint %s %s (total: %d)", ep.Method, ep.Path, len(cs.Endpoints)), nil

	case StateActionUpdate:
		for i, existing := range cs.Endpoints {
			if existing.Path == ep.Path && existing.Method == ep.Method {
				if len(ep.TestedFor) > 0 {
					cs.Endpoints[i].TestedFor = mergeStringSlices(existing.TestedFor, ep.TestedFor)
				}
				if ep.Status != "" {
					cs.Endpoints[i].Status = ep.Status
				}
				if ep.Notes != "" {
					cs.Endpoints[i].Notes = ep.Notes
				}
				return fmt.Sprintf("Updated endpoint %s %s", ep.Method, ep.Path), nil
			}
		}
		return fmt.Sprintf("Endpoint %s %s not found. Use action 'add' to create it first.", ep.Method, ep.Path), nil

	case StateActionRemove:
		for i, existing := range cs.Endpoints {
			if existing.Path == ep.Path && existing.Method == ep.Method {
				cs.Endpoints = append(cs.Endpoints[:i], cs.Endpoints[i+1:]...)
				return fmt.Sprintf("Removed endpoint %s %s", ep.Method, ep.Path), nil
			}
		}
		return fmt.Sprintf("Endpoint %s %s not found", ep.Method, ep.Path), nil

	default:
		return fmt.Sprintf("Unknown action '%s'. Valid actions: add, update, remove", op), nil
	}
}

func (cs *CoverageState) handleFindingUpdate(op StateAction, data json.RawMessage) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var f Finding
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Sprintf("Invalid finding data: %v. Expected: {id, type, endpoint, severity, status, evidence}", err), nil
	}

	switch op {
	case StateActionAdd:
		if f.ID == "" {
			f.ID = fmt.Sprintf("F%03d", len(cs.Findings)+1)
		}
		if f.Status == "" {
			f.Status = "lead"
		}
		cs.Findings = append(cs.Findings, f)
		return fmt.Sprintf("Added finding %s: %s on %s [%s] (total: %d)", f.ID, f.Type, f.Endpoint, f.Severity, len(cs.Findings)), nil

	case StateActionUpdate:
		for i, existing := range cs.Findings {
			if existing.ID == f.ID {
				if f.Status != "" {
					cs.Findings[i].Status = f.Status
				}
				if f.Severity != "" {
					cs.Findings[i].Severity = f.Severity
				}
				if f.Evidence.Command != "" {
					cs.Findings[i].Evidence = f.Evidence
				}
				return fmt.Sprintf("Updated finding %s", f.ID), nil
			}
		}
		return fmt.Sprintf("Finding %s not found", f.ID), nil

	case StateActionRemove:
		for i, existing := range cs.Findings {
			if existing.ID == f.ID {
				cs.Findings = append(cs.Findings[:i], cs.Findings[i+1:]...)
				return fmt.Sprintf("Removed finding %s", f.ID), nil
			}
		}
		return fmt.Sprintf("Finding %s not found", f.ID), nil

	default:
		return fmt.Sprintf("Unknown action '%s'. Valid actions: add, update, remove", op), nil
	}
}

func (cs *CoverageState) handleAuthUpdate(op StateAction, data json.RawMessage) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var auth AuthEntry
	if err := json.Unmarshal(data, &auth); err != nil {
		return fmt.Sprintf("Invalid auth data: %v. Expected: {name, token, type, obtained_at, expires_in}", err), nil
	}

	switch op {
	case StateActionAdd, StateActionUpdate:
		if auth.ObtainedAt == "" {
			auth.ObtainedAt = time.Now().UTC().Format(time.RFC3339)
		}
		for i, existing := range cs.Auth {
			if existing.Name == auth.Name {
				cs.Auth[i] = auth
				return fmt.Sprintf("Updated auth session '%s'", auth.Name), nil
			}
		}
		cs.Auth = append(cs.Auth, auth)
		return fmt.Sprintf("Added auth session '%s' (total: %d)", auth.Name, len(cs.Auth)), nil

	case StateActionRemove:
		for i, existing := range cs.Auth {
			if existing.Name == auth.Name {
				cs.Auth = append(cs.Auth[:i], cs.Auth[i+1:]...)
				return fmt.Sprintf("Removed auth session '%s'", auth.Name), nil
			}
		}
		return fmt.Sprintf("Auth session '%s' not found", auth.Name), nil

	default:
		return fmt.Sprintf("Unknown action '%s'. Valid actions: add, update, remove", op), nil
	}
}

func (cs *CoverageState) handleTriedUpdate(op StateAction, data json.RawMessage) (string, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	var t TriedApproach
	if err := json.Unmarshal(data, &t); err != nil {
		return fmt.Sprintf("Invalid tried data: %v. Expected: {approach, result, endpoint, timestamp}", err), nil
	}

	if op == StateActionAdd || op == StateActionUpdate {
		if t.Timestamp == "" {
			t.Timestamp = time.Now().UTC().Format(time.RFC3339)
		}
		cs.Tried = append(cs.Tried, t)
		return fmt.Sprintf("Recorded approach: %s (total: %d)", truncateStr(t.Approach, 80), len(cs.Tried)), nil
	}

	return fmt.Sprintf("Unknown action '%s'. Use 'add' to record tried approaches.", op), nil
}

func (cs *CoverageState) getEndpointsView(filter string) (string, error) {
	if len(cs.Endpoints) == 0 {
		return "No endpoints discovered yet. Run reconnaissance first.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ENDPOINTS (%d total):\n", len(cs.Endpoints)))
	for _, ep := range cs.Endpoints {
		if filter != "" && !strings.Contains(strings.ToLower(ep.Path+ep.Status+ep.Method), strings.ToLower(filter)) {
			continue
		}
		params := ""
		if len(ep.Params) > 0 {
			params = " params:" + strings.Join(ep.Params, ",")
		}
		tested := ""
		if len(ep.TestedFor) > 0 {
			tested = " tested:" + strings.Join(ep.TestedFor, ",")
		}
		authStr := ""
		if ep.AuthRequired {
			authStr = " [AUTH]"
		}
		sb.WriteString(fmt.Sprintf("  %s %s%s%s%s [%s]", ep.Method, ep.Path, params, authStr, tested, ep.Status))
		if ep.Notes != "" {
			sb.WriteString(fmt.Sprintf(" — %s", truncateStr(ep.Notes, 100)))
		}
		sb.WriteString("\n")
	}
	return sb.String(), nil
}

func (cs *CoverageState) getFindingsView(filter string) (string, error) {
	if len(cs.Findings) == 0 {
		return "No findings recorded yet.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("FINDINGS (%d total):\n", len(cs.Findings)))
	for _, f := range cs.Findings {
		if filter != "" && !strings.Contains(strings.ToLower(f.Type+f.Status+f.Endpoint+f.Severity), strings.ToLower(filter)) {
			continue
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s: %s on %s [%s]\n", f.Status, f.ID, f.Type, f.Endpoint, f.Severity))
	}
	return sb.String(), nil
}

func (cs *CoverageState) getAuthView() (string, error) {
	if len(cs.Auth) == 0 {
		return "No auth sessions established.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("AUTH SESSIONS (%d):\n", len(cs.Auth)))
	for _, a := range cs.Auth {
		tokenPreview := truncateStr(a.Token, 20) + "..."
		sb.WriteString(fmt.Sprintf("  %s: type=%s token=%s\n", a.Name, a.Type, tokenPreview))
	}
	return sb.String(), nil
}

func (cs *CoverageState) getTriedView(filter string) (string, error) {
	if len(cs.Tried) == 0 {
		return "No approaches recorded yet.", nil
	}

	var sb strings.Builder
	shown := 0
	for i := len(cs.Tried) - 1; i >= 0 && shown < 20; i-- {
		t := cs.Tried[i]
		if filter != "" && !strings.Contains(strings.ToLower(t.Approach+t.Endpoint), strings.ToLower(filter)) {
			continue
		}
		sb.WriteString(fmt.Sprintf("  %s: %s → %s\n", t.Endpoint, truncateStr(t.Approach, 60), truncateStr(t.Result, 60)))
		shown++
	}
	return fmt.Sprintf("TRIED APPROACHES (last %d of %d):\n%s", shown, len(cs.Tried), sb.String()), nil
}

func (cs *CoverageState) getCoverageSummary() (string, error) {
	var sb strings.Builder

	totalEndpoints := len(cs.Endpoints)
	testedEndpoints := 0
	totalCombinations := 0
	testedCombinations := 0

	vulnClasses := []string{"sqli", "xss", "cmdi", "ssti", "ssrf", "idor", "lfi", "xxe", "auth_bypass"}

	for _, ep := range cs.Endpoints {
		applicable := applicableVulnClasses(ep, vulnClasses)
		totalCombinations += len(applicable)
		testedCombinations += len(ep.TestedFor)
		if len(ep.TestedFor) > 0 {
			testedEndpoints++
		}
	}

	coveragePct := 0.0
	if totalCombinations > 0 {
		coveragePct = float64(testedCombinations) / float64(totalCombinations) * 100
	}

	confirmedFindings := 0
	leadFindings := 0
	for _, f := range cs.Findings {
		switch f.Status {
		case "confirmed":
			confirmedFindings++
		case "lead":
			leadFindings++
		}
	}

	sb.WriteString(fmt.Sprintf("COVERAGE: %.0f%% (%d/%d endpoint-vuln combinations tested)\n", coveragePct, testedCombinations, totalCombinations))
	sb.WriteString(fmt.Sprintf("ENDPOINTS: %d discovered, %d tested\n", totalEndpoints, testedEndpoints))
	sb.WriteString(fmt.Sprintf("FINDINGS: %d confirmed, %d leads\n", confirmedFindings, leadFindings))
	sb.WriteString(fmt.Sprintf("AUTH: %d sessions\n", len(cs.Auth)))

	if totalEndpoints > 0 {
		sb.WriteString("\nENDPOINTS:\n")
		for _, ep := range cs.Endpoints {
			params := ""
			if len(ep.Params) > 0 {
				params = " params:" + strings.Join(ep.Params, ",")
			}
			tested := "UNTESTED"
			if len(ep.TestedFor) > 0 {
				applicable := applicableVulnClasses(ep, vulnClasses)
				untested := diffStringSlices(applicable, ep.TestedFor)
				if len(untested) == 0 {
					tested = "FULLY TESTED"
				} else {
					tested = "TESTED:" + strings.Join(ep.TestedFor, ",") + " UNTESTED:" + strings.Join(untested, ",")
				}
			}
			sb.WriteString(fmt.Sprintf("  %s %s%s [%s]\n", ep.Method, ep.Path, params, tested))
		}
	}

	if confirmedFindings+leadFindings > 0 {
		sb.WriteString("\nFINDINGS:\n")
		for _, f := range cs.Findings {
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s on %s [%s]\n", f.Status, f.ID, f.Type, f.Endpoint, f.Severity))
		}
	}

	return sb.String(), nil
}

func (cs *CoverageState) GetCoverageSummary() string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	s, _ := cs.getCoverageSummary()
	return s
}

func (cs *CoverageState) FindingCount() int {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return len(cs.Findings)
}

func (cs *CoverageState) CoveragePercent() float64 {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	vulnClasses := []string{"sqli", "xss", "cmdi", "ssti", "ssrf", "idor", "lfi", "xxe", "auth_bypass"}
	total := 0
	tested := 0
	for _, ep := range cs.Endpoints {
		applicable := applicableVulnClasses(ep, vulnClasses)
		total += len(applicable)
		tested += len(ep.TestedFor)
	}
	if total == 0 {
		return 0
	}
	return float64(tested) / float64(total) * 100
}

func applicableVulnClasses(ep Endpoint, allClasses []string) []string {
	if len(ep.Params) == 0 {
		return []string{"auth_bypass", "idor"}
	}
	return allClasses
}

func mergeStringSlices(a, b []string) []string {
	seen := make(map[string]bool)
	for _, s := range a {
		seen[s] = true
	}
	result := make([]string, len(a))
	copy(result, a)
	for _, s := range b {
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}

func diffStringSlices(all, tested []string) []string {
	testedSet := make(map[string]bool)
	for _, s := range tested {
		testedSet[s] = true
	}
	var result []string
	for _, s := range all {
		if !testedSet[s] {
			result = append(result, s)
		}
	}
	return result
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

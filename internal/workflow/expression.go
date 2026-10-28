package workflow

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var expressionOps = []string{">=", "<=", "==", "!=", " contains ", " matches ", ">", "<"}
var jsonFuncRe = regexp.MustCompile(`^(jsonpath|jq)\((.*),\s*(['"][^'"]+['"])\)$`)
var jsonFuncFindRe = regexp.MustCompile(`(jsonpath|jq)\([^)]*\)`)
var singleTemplateVarRe = regexp.MustCompile(`^\{\{\s*([a-zA-Z0-9_.-]+)\s*\}\}$`)

func validateConditionExpression(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("condition expression cannot be empty")
	}
	for _, part := range splitBoolExpr(expr, "||") {
		for _, atom := range splitBoolExpr(part, "&&") {
			if err := validateConditionAtom(atom); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateConditionAtom(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("condition expression has an empty fragment")
	}
	if strings.Count(expr, "{{") != strings.Count(expr, "}}") {
		return fmt.Errorf("condition expression template parentheses mismatch: %s", expr)
	}
	if err := validateJSONFunctions(expr); err != nil {
		return err
	}
	if left, right, ok := splitExpressionAtom(expr, " matches "); ok {
		if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
			return fmt.Errorf("matches expression sides cannot be empty: %s", expr)
		}
		pattern := cleanComparable(resolveStaticTemplate(right))
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("matches regex invalid: %w", err)
		}
		return nil
	}
	for _, op := range expressionOps {
		if op == " matches " {
			continue
		}
		if left, right, ok := splitExpressionAtom(expr, op); ok {
			if strings.TrimSpace(left) == "" || strings.TrimSpace(right) == "" {
				return fmt.Errorf("expression %q sides cannot be empty: %s", strings.TrimSpace(op), expr)
			}
			return nil
		}
	}
	return nil
}

func evalCondition(expr string, state *WorkflowLocalState) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	orParts := splitBoolExpr(expr, "||")
	for _, orPart := range orParts {
		andOK := true
		for _, atom := range splitBoolExpr(orPart, "&&") {
			if !evalConditionAtom(atom, state) {
				andOK = false
				break
			}
		}
		if andOK {
			return true
		}
	}
	return false
}

func evalConditionAtom(expr string, state *WorkflowLocalState) bool {
	expr = strings.TrimSpace(expr)
	for _, op := range expressionOps {
		if left, right, ok := splitExpressionAtom(expr, op); ok {
			left = strings.TrimSpace(fmt.Sprint(resolveExpressionOperand(left, state)))
			right = strings.TrimSpace(fmt.Sprint(resolveExpressionOperand(right, state)))
			switch strings.TrimSpace(op) {
			case "==":
				return cleanComparable(left) == cleanComparable(right)
			case "!=":
				return cleanComparable(left) != cleanComparable(right)
			case ">":
				return compareNumeric(left, right, func(a, b float64) bool { return a > b })
			case ">=":
				return compareNumeric(left, right, func(a, b float64) bool { return a >= b })
			case "<":
				return compareNumeric(left, right, func(a, b float64) bool { return a < b })
			case "<=":
				return compareNumeric(left, right, func(a, b float64) bool { return a <= b })
			case "contains":
				return strings.Contains(cleanComparable(left), cleanComparable(right))
			case "matches":
				matched, _ := regexp.MatchString(cleanComparable(right), cleanComparable(left))
				return matched
			}
		}
	}
	resolved := strings.TrimSpace(fmt.Sprint(resolveExpressionOperand(expr, state)))
	v := strings.ToLower(cleanComparable(resolved))
	return v != "" && v != "false" && v != "0" && v != "null"
}

func splitBoolExpr(expr, sep string) []string {
	parts := strings.Split(expr, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{strings.TrimSpace(expr)}
	}
	return out
}

func splitExpressionAtom(expr, op string) (string, string, bool) {
	if strings.TrimSpace(op) == "contains" || strings.TrimSpace(op) == "matches" {
		idx := strings.Index(expr, op)
		if idx < 0 {
			return "", "", false
		}
		return expr[:idx], expr[idx+len(op):], true
	}
	idx := strings.Index(expr, op)
	if idx < 0 {
		return "", "", false
	}
	return expr[:idx], expr[idx+len(op):], true
}

func compareNumeric(left, right string, cmp func(float64, float64) bool) bool {
	a, errA := strconv.ParseFloat(cleanComparable(left), 64)
	b, errB := strconv.ParseFloat(cleanComparable(right), 64)
	if errA != nil || errB != nil {
		return false
	}
	return cmp(a, b)
}

func resolveStaticTemplate(s string) string {
	return templateVarRe.ReplaceAllString(s, "value")
}

func resolveExpressionOperand(raw string, state *WorkflowLocalState) any {
	raw = strings.TrimSpace(raw)
	if m := jsonFuncRe.FindStringSubmatch(raw); len(m) == 4 {
		inputExpr := strings.TrimSpace(m[2])
		path := strings.Trim(m[3], `"'`)
		input := resolveExpressionOperand(inputExpr, state)
		return evalJSONPathValue(input, path)
	}
	if m := singleTemplateVarRe.FindStringSubmatch(raw); len(m) == 2 {
		return valueFromPath(m[1], state)
	}
	return resolveTemplate(raw, state)
}

func validateJSONFunctions(expr string) error {
	for _, candidate := range jsonFuncFindRe.FindAllString(expr, -1) {
		candidate = strings.TrimSpace(candidate)
		m := jsonFuncRe.FindStringSubmatch(candidate)
		if len(m) != 4 {
			return fmt.Errorf("JSONPath/JQ function format should be jsonpath(value, \"$.path\") or jq(value, \".path\")")
		}
		if err := validateJSONPathSyntax(strings.Trim(m[3], `"'`)); err != nil {
			return err
		}
	}
	return nil
}

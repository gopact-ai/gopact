package gopacttest

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestCodeStylePolicy(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "mixed control depth", source: `package sample
func run() { for { if true { for {} } } }
`, want: "control nesting depth 3 exceeds 2"},
		{name: "if range if depth", source: `package sample
func run(values []int) { if len(values) > 0 { for range values { if true {} } } }
`, want: "control nesting depth 3 exceeds 2"},
		{name: "switch if for depth", source: `package sample
func run(v int) { switch v { case 0: if true { for {} } } }
`, want: "control nesting depth 3 exceeds 2"},
		{name: "select for if depth", source: `package sample
func run(ch <-chan int) { select { case <-ch: for { if true {} } } }
`, want: "control nesting depth 3 exceeds 2"},
		{name: "else branch depth", source: `package sample
func run() { if true {} else { for { if true {} } } }
`, want: "control nesting depth 3 exceeds 2"},
		{name: "positive magic number", source: `package sample
func run() int { return 3 }
`, want: "magic number 3"},
		{name: "negative magic number", source: `package sample
func run() int { return -1 }
`, want: "magic number -1"},
		{name: "floating point magic number", source: `package sample
func run() float64 { return 0.5 }
`, want: "magic number 0.5"},
		{name: "local constant is not an escape", source: `package sample
func run() int { const limit = 3; return limit }
`, want: "magic number 3"},
		{name: "local variable is not an escape", source: `package sample
func run() int { limit := 3; return limit }
`, want: "magic number 3"},
		{name: "too many parameters", source: `package sample
func run(a, b, c, d, e, f int) {}
`, want: "more than 5 parameters"},
		{name: "multiline signature", source: `package sample
func run(
	a int,
) {}
`, want: "signature must stay on one line"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := parseStyleViolations(t, tt.source)
			if !containsStyleViolation(violations, tt.want) {
				t.Fatalf("violations = %v, want %q", violations, tt.want)
			}
		})
	}
}

func TestCodeStylePolicyAllowsPackageBoundaryAndFlatBranches(t *testing.T) {
	source := `package sample
const retryLimit = 3
var defaultDelay = 0.5
func run(v int) int {
	if v == 0 { return retryLimit }
	if v == 1 { return v }
	return retryLimit
}
`
	if violations := parseStyleViolations(t, source); len(violations) != 0 {
		t.Fatalf("violations = %v, want none", violations)
	}
}

func TestCodeStylePolicyCountsEachFunctionIndependently(t *testing.T) {
	source := `package sample
func inner() { for { if true {} } }
func outer() { if true { inner() } }
`
	if violations := parseStyleViolations(t, source); len(violations) != 0 {
		t.Fatalf("violations = %v, want none", violations)
	}
}

func parseStyleViolations(t *testing.T, source string) []styleViolation {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", source, 0)
	if err != nil {
		t.Fatalf("ParseFile() error = %v", err)
	}
	return fileStyleViolations(fset, file, true)
}

func containsStyleViolation(violations []styleViolation, want string) bool {
	for _, violation := range violations {
		if strings.Contains(violation.Error(), want) {
			return true
		}
	}
	return false
}

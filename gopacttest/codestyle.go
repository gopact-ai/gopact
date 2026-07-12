package gopacttest

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const (
	maxControlDepth = 2
	maxParameters   = 5
	maxParamName    = 12
	maxSwitchCases  = 5
	maxCaseBody     = 2
)

// RequireCodeStyle verifies the repository-wide gopact source policy.
func RequireCodeStyle(t testing.TB, root string) {
	t.Helper()
	violations, err := inspectCodeStyle(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, violation := range violations {
		t.Error(violation)
	}
}

type styleViolation struct {
	position token.Position
	message  string
}

func (v styleViolation) Error() string {
	return v.position.String() + ": " + v.message
}

type styleChecker struct {
	fset       *token.FileSet
	checked    map[token.Pos]struct{}
	violations []styleViolation
}

func inspectCodeStyle(root string) ([]styleViolation, error) {
	root = filepath.Clean(root)
	fset := token.NewFileSet()
	checker := styleChecker{fset: fset, checked: make(map[token.Pos]struct{})}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return styleDirectoryAction(root, path, entry)
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		checker.file(file, !strings.HasSuffix(path, "_test.go"))
		return nil
	})
	return checker.violations, err
}

func styleDirectoryAction(root, path string, entry fs.DirEntry) error {
	if path == root {
		return nil
	}
	if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" || entry.Name() == "vendor" {
		return filepath.SkipDir
	}
	return nil
}

func fileStyleViolations(fset *token.FileSet, file *ast.File, numbers bool) []styleViolation {
	checker := styleChecker{fset: fset, checked: make(map[token.Pos]struct{})}
	checker.file(file, numbers)
	return checker.violations
}

func (c *styleChecker) file(file *ast.File, numbers bool) {
	ast.Inspect(file, func(node ast.Node) bool {
		switch fn := node.(type) {
		case *ast.FuncDecl:
			c.checked[fn.Type.Func] = struct{}{}
			c.function(fn.Type, fn.Body, numbers)
		case *ast.FuncLit:
			c.checked[fn.Type.Func] = struct{}{}
			c.function(fn.Type, fn.Body, numbers)
		case *ast.FuncType:
			if _, ok := c.checked[fn.Func]; !ok {
				c.signature(fn, fn.End())
			}
		}
		return true
	})
}

func (c *styleChecker) function(signature *ast.FuncType, body *ast.BlockStmt, numbers bool) {
	end := signature.End()
	if body != nil {
		end = body.Lbrace
	}
	c.signature(signature, end)
	if body == nil {
		return
	}
	c.block(body.List, 0)
	if numbers {
		c.numbers(body)
	}
}

func (c *styleChecker) signature(signature *ast.FuncType, end token.Pos) {
	startPos := signature.Func
	if !startPos.IsValid() {
		startPos = signature.Params.Opening
	}
	start := c.fset.Position(startPos)
	if start.Line != c.fset.Position(end).Line {
		c.add(startPos, "function signature must stay on one line")
	}
	if countFields(signature.Params) > maxParameters {
		c.addf(startPos, "function has more than %d parameters; use a request or options", maxParameters)
	}
	for _, field := range signature.Params.List {
		c.parameterNames(field)
	}
}

func (c *styleChecker) parameterNames(field *ast.Field) {
	for _, name := range field.Names {
		if len(name.Name) > maxParamName {
			c.addf(name.Pos(), "parameter %q is too long", name.Name)
		}
	}
}

func (c *styleChecker) numbers(body *ast.BlockStmt) {
	ast.Inspect(body, func(node ast.Node) bool {
		switch item := node.(type) {
		case *ast.FuncLit:
			return false
		case *ast.UnaryExpr:
			return c.unaryNumber(item)
		case *ast.BasicLit:
			c.literalNumber(item)
		}
		return true
	})
}

func (c *styleChecker) unaryNumber(expr *ast.UnaryExpr) bool {
	if expr.Op != token.ADD && expr.Op != token.SUB {
		return true
	}
	literal, ok := expr.X.(*ast.BasicLit)
	if !ok || !isNumeric(literal.Kind) {
		return true
	}
	value := constant.MakeFromLiteral(literal.Value, literal.Kind, 0)
	c.number(expr.Pos(), expr.Op.String()+literal.Value, constant.UnaryOp(expr.Op, value, 0))
	return false
}

func (c *styleChecker) literalNumber(literal *ast.BasicLit) {
	if !isNumeric(literal.Kind) {
		return
	}
	value := constant.MakeFromLiteral(literal.Value, literal.Kind, 0)
	c.number(literal.Pos(), literal.Value, value)
}

func (c *styleChecker) number(pos token.Pos, text string, value constant.Value) {
	if isTrivialNumber(value) {
		return
	}
	c.addf(pos, "magic number %s must be a package-level predefined value, boundary, capacity, or default", text)
}

func isNumeric(kind token.Token) bool {
	return kind == token.INT || kind == token.FLOAT || kind == token.IMAG
}

func isTrivialNumber(value constant.Value) bool {
	if value.Kind() == constant.Unknown {
		return false
	}
	return constant.Compare(value, token.EQL, constant.MakeInt64(0)) || constant.Compare(value, token.EQL, constant.MakeInt64(1))
}

func countFields(fields *ast.FieldList) int {
	if fields == nil {
		return 0
	}
	count := 0
	for _, field := range fields.List {
		count += max(1, len(field.Names))
	}
	return count
}

func (c *styleChecker) block(statements []ast.Stmt, depth int) {
	for _, statement := range statements {
		c.statement(statement, depth)
	}
}

func (c *styleChecker) statement(statement ast.Stmt, depth int) {
	switch stmt := statement.(type) {
	case *ast.IfStmt:
		c.ifStatement(stmt, depth)
	case *ast.ForStmt:
		c.control(stmt, stmt.Body.List, depth)
	case *ast.RangeStmt:
		c.control(stmt, stmt.Body.List, depth)
	case *ast.SwitchStmt:
		c.switchStatement(stmt, stmt.Body.List, depth)
	case *ast.TypeSwitchStmt:
		c.switchStatement(stmt, stmt.Body.List, depth)
	case *ast.SelectStmt:
		c.switchStatement(stmt, stmt.Body.List, depth)
	case *ast.BlockStmt:
		c.block(stmt.List, depth)
	case *ast.LabeledStmt:
		c.statement(stmt.Stmt, depth)
	}
}

func (c *styleChecker) ifStatement(stmt *ast.IfStmt, depth int) {
	c.control(stmt, stmt.Body.List, depth)
	switch branch := stmt.Else.(type) {
	case *ast.IfStmt:
		c.statement(branch, depth)
	case *ast.BlockStmt:
		c.block(branch.List, depth+1)
	}
}

func (c *styleChecker) control(node ast.Node, statements []ast.Stmt, depth int) {
	next := depth + 1
	if next > maxControlDepth {
		c.addf(node.Pos(), "control nesting depth %d exceeds %d", next, maxControlDepth)
	}
	c.block(statements, next)
}

func (c *styleChecker) switchStatement(node ast.Node, clauses []ast.Stmt, depth int) {
	next := depth + 1
	if next > maxControlDepth {
		c.addf(node.Pos(), "control nesting depth %d exceeds %d", next, maxControlDepth)
	}
	limit := maxCaseBody
	if next > 1 || len(clauses) > maxSwitchCases {
		limit = 1
	}
	rule := caseRule{depth: next, limit: limit}
	for _, clause := range clauses {
		c.clause(clause, rule)
	}
}

type caseRule struct {
	depth int
	limit int
}

func (c *styleChecker) clause(clause ast.Stmt, rule caseRule) {
	switch item := clause.(type) {
	case *ast.CaseClause:
		c.caseBody(item, item.Body, rule)
	case *ast.CommClause:
		c.caseBody(item, item.Body, rule)
	}
}

func (c *styleChecker) caseBody(clause ast.Node, body []ast.Stmt, rule caseRule) {
	if len(body) > rule.limit {
		c.addf(clause.Pos(), "switch case has %d statements; dispatch through a function", len(body))
	}
	c.block(body, rule.depth)
}

func (c *styleChecker) add(pos token.Pos, message string) {
	c.violations = append(c.violations, styleViolation{position: c.fset.Position(pos), message: message})
}

func (c *styleChecker) addf(pos token.Pos, format string, args ...any) {
	c.add(pos, fmt.Sprintf(format, args...))
}

package fasteval

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
)

//nolint:gochecknoglobals // The immutable default compiler is initialized once.
var loadDefaultCompiler = sync.OnceValue(func() *Compiler {
	compiler, err := NewCompiler()
	if err != nil {
		panic(fmt.Sprintf("initialize default compiler: %v", err))
	}

	return compiler
})

// Compiler builds expressions and templates with an immutable function registry.
type Compiler struct {
	functions map[string]*callable
}

type compilerOptions struct {
	functions map[string]any
}

// CompilerOption changes how a [Compiler] is built.
type CompilerOption func(*compilerOptions) error

// WithFunction adds one Go function to a compiler.
func WithFunction(name string, function any) CompilerOption {
	return func(options *compilerOptions) error {
		if options.functions == nil {
			options.functions = make(map[string]any)
		}

		if _, exists := options.functions[name]; exists {
			return newDetailError("function %q is registered more than once", name)
		}

		options.functions[name] = function

		return nil
	}
}

// WithFunctions adds a map of named Go functions to a compiler.
func WithFunctions(functions map[string]any) CompilerOption {
	copyOfFunctions := make(map[string]any, len(functions))
	maps.Copy(copyOfFunctions, functions)

	return func(options *compilerOptions) error {
		for name, function := range copyOfFunctions {
			if options.functions == nil {
				options.functions = make(map[string]any)
			}

			if _, exists := options.functions[name]; exists {
				return newDetailError("function %q is registered more than once", name)
			}

			options.functions[name] = function
		}

		return nil
	}
}

// NewCompiler returns an immutable compiler with every builtin enabled.
func NewCompiler(options ...CompilerOption) (*Compiler, error) {
	config := compilerOptions{functions: make(map[string]any)}

	for _, option := range options {
		if option == nil {
			return nil, newDetailError("compiler option cannot be nil")
		}

		err := option(&config)
		if err != nil {
			return nil, fmt.Errorf("configure compiler: %w", err)
		}
	}

	functions := makeBuiltins()
	for name, function := range config.functions {
		if _, reserved := functions[name]; reserved {
			return nil, newDetailError("function name %q is reserved by a builtin", name)
		}

		callableFunction, err := newCallable(name, function)
		if err != nil {
			return nil, err
		}

		functions[name] = callableFunction
	}

	return &Compiler{functions: functions}, nil
}

func defaultCompiler() *Compiler {
	return loadDefaultCompiler()
}

// Expression is an immutable compiled expression with a dynamic result.
type Expression struct {
	source         string
	constantValue  any
	ast            *Node
	root           *Node
	compiler       *Compiler
	constantResult bool
	resultReady    bool
}

type rootResultPolicy struct {
	constantValue  any
	constantResult bool
	resultReady    bool
}

// Compile builds an [Expression] from source with the default builtin-only compiler.
func Compile(source string) (*Expression, error) {
	return defaultCompiler().compile(context.Background(), source)
}

// Compile builds an [Expression] from source with c's function registry.
func (c *Compiler) Compile(source string) (*Expression, error) {
	return c.compile(context.Background(), source)
}

func (c *Compiler) compile(ctx context.Context, source string) (*Expression, error) {
	if c == nil || c.functions == nil {
		return nil, newDetailError("compiler is not initialized")
	}

	parsed, diagnostics := parse(source)
	if diagnostics != nil {
		return nil, diagnostics
	}

	root, diagnostics := preparePlan(ctx, parsed)
	if diagnostics != nil {
		return nil, diagnostics
	}

	resultPolicy := rootResultProperties(root)

	return &Expression{
		source: source, constantValue: resultPolicy.constantValue,
		ast: parsed, root: root, compiler: c,
		constantResult: resultPolicy.constantResult, resultReady: resultPolicy.resultReady,
	}, nil
}

func rootResultProperties(root *Node) rootResultPolicy {
	if root.kind == NodeLiteral {
		literal, numeric := root.value.(numberLiteral)
		if numeric {
			return rootResultPolicy{
				constantValue: literal.defaultValue, constantResult: literal.defaultOK, resultReady: false,
			}
		}

		ready := !isLiteralCollection(root.value)

		return rootResultPolicy{constantValue: root.value, constantResult: ready, resultReady: ready}
	}

	if root.kind == NodeUnary {
		return rootResultPolicy{constantValue: nil, constantResult: false, resultReady: root.text == "!"}
	}

	if root.kind != NodeBinary {
		return rootResultPolicy{constantValue: nil, constantResult: false, resultReady: false}
	}

	switch root.text {
	case "==", "!=", ">", ">=", "<", "<=", "=~", "!~", "in", "&&", "||":
		return rootResultPolicy{constantValue: nil, constantResult: false, resultReady: true}
	default:
		return rootResultPolicy{constantValue: nil, constantResult: false, resultReady: false}
	}
}

// AST returns the immutable root of the parsed source tree.
func (e *Expression) AST() *Node {
	if e == nil {
		return nil
	}

	return e.ast
}

// Source returns the expression text supplied at compile time.
func (e *Expression) Source() string {
	if e == nil {
		return ""
	}

	return e.source
}

// Eval runs e with variables and evaluation options.
func (e *Expression) Eval(ctx context.Context, variables map[string]any, options ...EvalOption) (any, error) {
	value, err := e.eval(ctx, variables, false, options...)

	return value, err
}

func (e *Expression) eval(
	ctx context.Context,
	variables map[string]any,
	preserveLiterals bool,
	options ...EvalOption,
) (any, error) {
	if e == nil || e.compiler == nil || e.root == nil {
		return nil, newDetailError("expression is not initialized")
	}

	var config evalOptions

	if len(options) != 0 {
		var err error

		config, err = applyEvalOptions(options)
		if err != nil {
			return nil, err
		}
	}

	ctx = contextOrBackground(ctx)

	err := ctx.Err()
	if err != nil {
		return nil, evalError(ErrCanceled, "evaluate expression", e.root.span, "", err)
	}

	if value, ready := e.cachedConstantValue(preserveLiterals); ready {
		return value, nil
	}

	state := evalState{
		variables: variables, resolver: config.resolver, functions: e.compiler.functions,
		preserveLiterals: preserveLiterals, stepsUntilContextCheck: 0,
	}

	state.stepsUntilContextCheck = evaluationContextCheckInterval - 1

	value, err := state.evaluate(ctx, e.root)
	if err != nil {
		return nil, err
	}

	if e.resultReady {
		return value, nil
	}

	value, err = state.evaluationValue(ctx, value, e.root.span)

	return value, err
}

func (e *Expression) cachedConstantValue(preserveLiterals bool) (any, bool) {
	if preserveLiterals || !e.constantResult {
		return nil, false
	}

	return e.constantValue, true
}

// Eval compiles source with the default compiler, then evaluates it.
func Eval(ctx context.Context, source string, variables map[string]any, options ...EvalOption) (any, error) {
	ctx = contextOrBackground(ctx)

	expression, err := defaultCompiler().compile(ctx, source)
	if err != nil {
		return nil, err
	}

	return expression.Eval(ctx, variables, options...)
}

// Program is an immutable compiled expression whose result is converted to T.
type Program[T any] struct {
	expression *Expression
}

// CompileAs compiles source and checks conversion to T during evaluation.
func CompileAs[T any](source string) (*Program[T], error) {
	expression, err := Compile(source)
	if err != nil {
		return nil, err
	}

	return &Program[T]{expression: expression}, nil
}

// CompileAsWith compiles source as T with c.
func CompileAsWith[T any](c *Compiler, source string) (*Program[T], error) {
	expression, err := c.Compile(source)
	if err != nil {
		return nil, err
	}

	return &Program[T]{expression: expression}, nil
}

// Eval runs p and recursively converts its result to T without loss.
func (p *Program[T]) Eval(ctx context.Context, variables map[string]any, options ...EvalOption) (T, error) {
	var zero T
	if p == nil || p.expression == nil {
		return zero, newDetailError("program is nil")
	}

	value, err := p.expression.eval(ctx, variables, true, options...)
	if err != nil {
		return zero, err
	}

	converted, err := convertToWithAssignableScan[T](
		contextOrBackground(ctx), value, isLiteralCollection(value),
	)
	if err != nil {
		var evaluationErr *EvalError
		if errors.As(err, &evaluationErr) {
			return zero, withEvalErrorSpan(evaluationErr, p.expression.root.span)
		}

		return zero, evalError(ErrType, "convert expression result", p.expression.root.span, "", err)
	}

	return converted, nil
}

// EvalAs compiles source with the default compiler and evaluates it as T.
func EvalAs[T any](ctx context.Context, source string, variables map[string]any, options ...EvalOption) (T, error) {
	program, err := CompileAs[T](source)
	if err != nil {
		var zero T

		return zero, err
	}

	return program.Eval(ctx, variables, options...)
}

// EvalAsWith compiles source with c and evaluates it as T.
func EvalAsWith[T any](
	ctx context.Context,
	c *Compiler,
	source string,
	variables map[string]any,
	options ...EvalOption,
) (T, error) {
	program, err := CompileAsWith[T](c, source)
	if err != nil {
		var zero T

		return zero, err
	}

	return program.Eval(ctx, variables, options...)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}

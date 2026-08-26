package fasteval

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"time"
)

const (
	binaryOperandCount             = 2
	evaluationContextCheckInterval = 32
	mapEntryWidth                  = 2
)

type evaluationResultMode uint8

const (
	normalizedResult evaluationResultMode = iota
	rawResult
)

type evalState struct {
	variables              map[string]any
	resolver               ResolveFunc
	functions              map[string]*callable
	preserveLiterals       bool
	stepsUntilContextCheck uint8
}

type planPreparer struct {
	diagnostics []Diagnostic
}

func (s *evalState) evaluate(ctx context.Context, currentNode *Node) (any, error) {
	return s.evaluateResult(ctx, currentNode, normalizedResult)
}

func (s *evalState) evaluateRaw(ctx context.Context, currentNode *Node) (any, error) {
	return s.evaluateResult(ctx, currentNode, rawResult)
}

func (s *evalState) evaluateResult(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
) (any, error) {
	err := s.checkEvaluationContext(ctx, currentNode.span)
	if err != nil {
		return nil, err
	}

	normalize := mode == normalizedResult
	if basicNodeKind(currentNode.kind) {
		return s.evaluateBasicNode(ctx, currentNode, mode, normalize)
	}

	return s.evaluateCompoundNode(ctx, currentNode, mode)
}

func basicNodeKind(kind NodeKind) bool {
	switch kind {
	case NodeInvalid, NodeLiteral, NodeIdentifier, NodeUnary, NodeBinary:
		return true
	case NodeConditional, NodeCall, NodeAccess, NodeIndex, NodeList, NodeMap:
		return false
	}

	return false
}

func (s *evalState) evaluateBasicNode(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
	normalize bool,
) (any, error) {
	switch currentNode.kind {
	case NodeInvalid:
		return nil, evalError(ErrType, "evaluate expression", currentNode.span, "", newDetailError("invalid AST node"))
	case NodeLiteral:
		return currentNode.value, nil
	case NodeIdentifier:
		return s.lookupValue(ctx, currentNode, normalize)
	case NodeUnary:
		return s.evaluateUnary(ctx, currentNode)
	case NodeBinary:
		if mode == rawResult && currentNode.text == "??" {
			left, err := s.evaluateRaw(ctx, currentNode.children[0])
			if err != nil {
				return nil, err
			}

			return s.shortCircuit(ctx, currentNode, left, rawResult)
		}

		return s.binary(ctx, currentNode)
	case NodeConditional, NodeCall, NodeAccess, NodeIndex, NodeList, NodeMap:
	}

	return unsupportedNodeError(currentNode)
}

func (s *evalState) evaluateCompoundNode(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
) (any, error) {
	switch currentNode.kind {
	case NodeConditional:
		return s.evaluateConditionalResult(ctx, currentNode, mode)
	case NodeCall:
		return s.evaluateCallResult(ctx, currentNode, mode)
	case NodeAccess:
		return s.evaluateAccessResult(ctx, currentNode, mode)
	case NodeIndex:
		return s.evaluateIndexResult(ctx, currentNode, mode)
	case NodeList:
		return s.evaluateList(ctx, currentNode)
	case NodeMap:
		return s.evaluateMap(ctx, currentNode)
	case NodeInvalid, NodeLiteral, NodeIdentifier, NodeUnary, NodeBinary:
	}

	return unsupportedNodeError(currentNode)
}

func unsupportedNodeError(currentNode *Node) (any, error) {
	return nil, evalError(
		ErrType, "evaluate expression", currentNode.span, "",
		newDetailError("unsupported AST node %v", currentNode.kind),
	)
}

func (s *evalState) checkEvaluationContext(ctx context.Context, span Span) error {
	if s.stepsUntilContextCheck != 0 {
		s.stepsUntilContextCheck--

		return nil
	}

	s.stepsUntilContextCheck = evaluationContextCheckInterval - 1

	return contextCheckAt(ctx, "evaluate expression", span)
}

func (s *evalState) evaluateUnary(ctx context.Context, currentNode *Node) (any, error) {
	value, err := s.evaluate(ctx, currentNode.children[0])
	if err != nil {
		return nil, err
	}

	return unaryOperation(currentNode.text, value, currentNode.span)
}

func (s *evalState) evaluateConditionalResult(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
) (any, error) {
	condition, err := s.evaluate(ctx, currentNode.children[0])
	if err != nil {
		return nil, err
	}

	boolean, valid := condition.(bool)
	if !valid {
		return nil, evalError(
			ErrType, "evaluate conditional", currentNode.children[0].span, "",
			newDetailError("condition has type %T, want bool", condition),
		)
	}

	if boolean {
		return s.evaluateResult(ctx, currentNode.children[1], mode)
	}

	return s.evaluateResult(ctx, currentNode.children[2], mode)
}

func (s *evalState) evaluateCallResult(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
) (any, error) {
	function, err := s.callableForNode(ctx, currentNode)
	if err != nil {
		return nil, err
	}

	arguments, err := s.callArguments(ctx, function, currentNode.children[1:])
	if err != nil {
		return nil, err
	}

	value, err := function.invokeRaw(ctx, arguments, currentNode.span)
	if err != nil {
		return nil, err
	}

	if mode == normalizedResult && !isLiteralCollection(value) {
		value = normalizeValue(value)
	}

	return value, nil
}

func (s *evalState) callableForNode(ctx context.Context, currentNode *Node) (*callable, error) {
	calleeNode := currentNode.children[0]
	if calleeNode.kind == NodeIdentifier {
		if function, found := s.functions[calleeNode.text]; found {
			return function, nil
		}
	}

	callee, err := s.evaluate(ctx, calleeNode)
	if err != nil {
		return nil, err
	}

	if function, valid := callee.(*callable); valid {
		return function, nil
	}

	reflected := reflect.ValueOf(callee)
	if !reflected.IsValid() || reflected.Kind() != reflect.Func {
		return nil, evalError(
			ErrType, "call value", currentNode.span, calleeNode.text,
			newDetailError("value has type %T, want function", callee),
		)
	}

	function, err := newReflectCallable(calleeNode.text, reflected)
	if err != nil {
		return nil, evalError(ErrFunction, "prepare function", currentNode.span, calleeNode.text, err)
	}

	return function, nil
}

func (s *evalState) callArguments(ctx context.Context, function *callable, nodes []*Node) ([]any, error) {
	arguments := make([]any, len(nodes))

	for position, argumentNode := range nodes {
		if function.argumentMode&secondArgumentFunctionReference != 0 &&
			position == 1 && argumentNode.kind == NodeIdentifier {
			if reference, found := s.functions[argumentNode.text]; found {
				arguments[position] = reference

				continue
			}
		}

		var (
			value any
			err   error
		)

		if function.argumentMode&allArgumentsRaw != 0 ||
			function.argumentMode&firstArgumentRaw != 0 && position == 0 {
			value, err = s.evaluateRaw(ctx, argumentNode)
		} else {
			value, err = s.evaluate(ctx, argumentNode)
		}

		if err != nil {
			return nil, err
		}

		arguments[position] = value
	}

	return arguments, nil
}

func (s *evalState) evaluateAccessResult(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
) (any, error) {
	receiverNode := currentNode.children[0]

	receiver, err := s.evaluateRaw(ctx, receiverNode)
	if err != nil {
		return nil, err
	}

	return accessValue(receiver, currentNode.text, currentNode.optional, currentNode.span, mode)
}

func (s *evalState) evaluateIndexResult(
	ctx context.Context,
	currentNode *Node,
	mode evaluationResultMode,
) (any, error) {
	receiver, err := s.evaluateRaw(ctx, currentNode.children[0])
	if err != nil {
		return nil, err
	}

	if currentNode.optional {
		value, valid, indirectErr := indirectValue(receiver)
		if indirectErr != nil {
			return nil, evalError(ErrAccess, "index value", currentNode.span, "", indirectErr)
		}

		if !valid || nilable(value.Kind()) && reflectValueIsNil(value) {
			return nil, nil
		}
	}

	indexNode := currentNode.children[1]

	index, err := s.evaluateRaw(ctx, indexNode)
	if err != nil {
		return nil, err
	}

	return indexValue(receiver, index, currentNode.span, mode)
}

func (s *evalState) evaluateList(ctx context.Context, currentNode *Node) (rawSequence, error) {
	values := make(rawSequence, len(currentNode.children))

	for position, child := range currentNode.children {
		value, err := s.evaluateRaw(ctx, child)
		if err != nil {
			return nil, err
		}

		values[position] = value
	}

	return values, nil
}

func (s *evalState) evaluateMap(ctx context.Context, currentNode *Node) (rawMap, error) {
	values := make(rawMap, len(currentNode.children)/mapEntryWidth)

	var literalKeys literalMapKeyIndex

	for position := 0; position < len(currentNode.children); position += mapEntryWidth {
		keyNode := currentNode.children[position]

		key, err := s.evaluateRaw(ctx, keyNode)
		if err != nil {
			return nil, err
		}

		if isNilValue(key) {
			key = nil
		}

		err = validateMapKey(map[any]any(values), &literalKeys, key, keyNode.span)
		if err != nil {
			return nil, err
		}

		value, err := s.evaluateRaw(ctx, currentNode.children[position+1])
		if err != nil {
			return nil, err
		}

		values[key] = value
	}

	return values, nil
}

func (s *evalState) evaluationValue(ctx context.Context, value any, span Span) (any, error) {
	if s.preserveLiterals {
		return value, nil
	}

	if !isLiteralCollection(value) {
		return materializeLiteral(value, span)
	}

	return materializeEvaluationValue(ctx, value, span, 0)
}

func isLiteralCollection(value any) bool {
	switch value.(type) {
	case rawSequence, rawMap, rawGroupedMap:
		return true
	}

	return false
}

func isLiteralMap(value any) bool {
	switch value.(type) {
	case rawMap, rawGroupedMap:
		return true
	}

	return false
}

func materializeEvaluationValue(
	ctx context.Context,
	value any,
	span Span,
	depth int,
) (any, error) {
	err := checkValueDepth(depth)
	if err != nil {
		return nil, err
	}

	err = contextCheckAt(ctx, "materialize expression result", span)
	if err != nil {
		return nil, err
	}

	if !isLiteralCollection(value) {
		return materializeLiteral(value, span)
	}

	if values, ok := value.(rawSequence); ok {
		reuse, err := reusableRawSequence(ctx, values, span)
		if err != nil {
			return nil, err
		}

		if reuse {
			return []any(values), nil
		}
	}

	if values, ok := value.(rawMap); ok {
		reuse, err := reusableRawMap(ctx, values, span)
		if err != nil {
			return nil, err
		}

		if reuse {
			return map[any]any(values), nil
		}
	}

	return materializeReflectCollection(ctx, reflect.ValueOf(value), value, span, depth)
}

func materializeReflectCollection(
	ctx context.Context,
	reflected reflect.Value,
	value any,
	span Span,
	depth int,
) (any, error) {
	switch reflected.Kind() {
	case reflect.Slice:
		return materializeReflectSequence(ctx, reflected, span, depth)
	case reflect.Map:
		return materializeReflectMap(ctx, reflected, span, depth)
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer,
		reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return materializeLiteral(value, span)
}

func materializeReflectSequence(
	ctx context.Context,
	reflected reflect.Value,
	span Span,
	depth int,
) (any, error) {
	resultType := reflected.Type()
	if resultType == reflect.TypeFor[rawSequence]() {
		resultType = reflect.TypeFor[[]any]()
	}

	result := reflect.MakeSlice(resultType, reflected.Len(), reflected.Len())

	for position := range reflected.Len() {
		item, err := materializeEvaluationValue(ctx, reflected.Index(position).Interface(), span, depth+1)
		if err != nil {
			return nil, err
		}

		converted, err := materializedCollectionValue(item, result.Index(position).Type())
		if err != nil {
			return nil, err
		}

		result.Index(position).Set(converted)
	}

	return result.Interface(), nil
}

func materializeReflectMap(
	ctx context.Context,
	reflected reflect.Value,
	span Span,
	depth int,
) (any, error) {
	resultType := reflected.Type()
	if resultType == reflect.TypeFor[rawMap]() {
		resultType = reflect.TypeFor[map[any]any]()
	} else if resultType == reflect.TypeFor[rawGroupedMap]() {
		resultType = reflect.TypeFor[map[any][]any]()
	}

	result := reflect.MakeMapWithSize(resultType, reflected.Len())

	iterator := reflected.MapRange()
	for iterator.Next() {
		key, err := materializeEvaluationValue(ctx, iterator.Key().Interface(), span, depth+1)
		if err != nil {
			return nil, err
		}

		convertedKey, err := exactMapKey(key, result.Type().Key())
		if err != nil {
			return nil, err
		}

		if result.MapIndex(convertedKey).IsValid() {
			return nil, evalError(
				ErrType, "materialize map key", span, "", newDetailError("duplicate key %#v", key),
			)
		}

		item, err := materializeEvaluationValue(ctx, iterator.Value().Interface(), span, depth+1)
		if err != nil {
			return nil, err
		}

		convertedItem, err := materializedCollectionValue(item, result.Type().Elem())
		if err != nil {
			return nil, err
		}

		result.SetMapIndex(convertedKey, convertedItem)
	}

	return result.Interface(), nil
}

func reusableRawSequence(ctx context.Context, values rawSequence, span Span) (bool, error) {
	for position, value := range values {
		if position%evaluationContextCheckInterval == 0 {
			cancellationErr := contextCheckAt(ctx, "materialize expression result", span)
			if cancellationErr != nil {
				return false, cancellationErr
			}
		}

		if valueNeedsMaterialization(value) {
			return false, nil
		}
	}

	return true, nil
}

func reusableRawMap(ctx context.Context, values rawMap, span Span) (bool, error) {
	position := 0
	for key, value := range values {
		if position%evaluationContextCheckInterval == 0 {
			cancellationErr := contextCheckAt(ctx, "materialize expression result", span)
			if cancellationErr != nil {
				return false, cancellationErr
			}
		}

		position++

		if valueNeedsMaterialization(key) || valueNeedsMaterialization(value) {
			return false, nil
		}
	}

	return true, nil
}

func valueNeedsMaterialization(value any) bool {
	if _, literal := value.(numberLiteral); literal || isLiteralCollection(value) {
		return true
	}

	return reflect.TypeOf(normalizeValue(value)) != reflect.TypeOf(value)
}

func materializedCollectionValue(value any, target reflect.Type) (reflect.Value, error) {
	if value == nil {
		if nilable(target.Kind()) {
			return reflect.Zero(target), nil
		}

		return reflect.Value{}, newDetailError("nil cannot be materialized as %v", target)
	}

	reflected := reflect.ValueOf(value)
	if reflected.Type().AssignableTo(target) {
		return reflected, nil
	}

	if reflected.Type().ConvertibleTo(target) {
		return reflected.Convert(target), nil
	}

	return reflect.Value{}, newDetailError("%v cannot be materialized as %v", reflected.Type(), target)
}

func validateMapKey(values map[any]any, literals *literalMapKeyIndex, key any, span Span) error {
	if !comparableMapKey(key) {
		return evalError(
			ErrType, "evaluate map key", span, "",
			newDetailError("key has non-comparable type %T", key),
		)
	}

	if _, duplicate := existingEquivalentMapKey(values, literals, key); duplicate {
		return evalError(
			ErrType, "evaluate map literal", span, "",
			newDetailError("duplicate key %#v", key),
		)
	}

	return nil
}

func (s *evalState) lookupValue(ctx context.Context, currentNode *Node, normalize bool) (any, error) {
	if value, found := s.variables[currentNode.text]; found {
		if normalize {
			value = normalizeValue(value)
		}

		return value, nil
	}

	if s.resolver == nil {
		return nil, unknownVariableError(currentNode)
	}

	return s.resolveValue(ctx, currentNode, normalize)
}

func (s *evalState) resolveValue(
	ctx context.Context,
	currentNode *Node,
	normalize bool,
) (_ any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = evalError(
				ErrPanic, "resolve variable", currentNode.span, currentNode.text,
				newDetailError("panic: %v", recovered),
			)
		}
	}()

	value, found, resolveErr := s.resolver(ctx, currentNode.text)

	cancellationErr := contextCheckAt(ctx, "resolve variable", currentNode.span)
	if cancellationErr != nil {
		return nil, cancellationErr
	}

	if resolveErr != nil {
		return nil, evalError(ErrUnknownVariable, "resolve variable", currentNode.span, currentNode.text, resolveErr)
	}

	if !found {
		return nil, unknownVariableError(currentNode)
	}

	if normalize {
		value = normalizeValue(value)
	}

	return value, nil
}

func unknownVariableError(currentNode *Node) error {
	return evalError(
		ErrUnknownVariable, "resolve variable", currentNode.span, currentNode.text,
		newDetailError("name is not defined"),
	)
}

func (s *evalState) binary(ctx context.Context, currentNode *Node) (any, error) {
	left, err := s.evaluate(ctx, currentNode.children[0])
	if err != nil {
		return nil, err
	}

	if shortCircuitOperator(currentNode.text) {
		return s.shortCircuit(ctx, currentNode, left, normalizedResult)
	}

	right, err := s.evaluate(ctx, currentNode.children[1])
	if err != nil {
		return nil, err
	}

	return binaryValues(ctx, currentNode, left, right)
}

func shortCircuitOperator(operator string) bool {
	return operator == "&&" || operator == "||" || operator == "??"
}

func (s *evalState) shortCircuit(
	ctx context.Context,
	currentNode *Node,
	left any,
	mode evaluationResultMode,
) (any, error) {
	if currentNode.text == "??" {
		if !isNilValue(left) {
			return left, nil
		}

		return s.evaluateResult(ctx, currentNode.children[1], mode)
	}

	boolean, valid := left.(bool)
	if !valid {
		return nil, booleanOperandError(currentNode.text, "left", currentNode.children[0].span, left)
	}

	if currentNode.text == "&&" && !boolean || currentNode.text == "||" && boolean {
		return boolean, nil
	}

	right, err := s.evaluate(ctx, currentNode.children[1])
	if err != nil {
		return nil, err
	}

	rightBoolean, valid := right.(bool)
	if !valid {
		return nil, booleanOperandError(currentNode.text, "right", currentNode.children[1].span, right)
	}

	return rightBoolean, nil
}

func booleanOperandError(operator, side string, span Span, value any) error {
	return evalError(
		ErrType, "evaluate "+operator, span, "",
		newDetailError("%s operand has type %T, want bool", side, value),
	)
}

func binaryValues(ctx context.Context, currentNode *Node, left, right any) (any, error) {
	switch currentNode.text {
	case "==", "!=":
		equal, err := equalValuesContext(ctx, left, right)
		if err != nil {
			return nil, err
		}

		if currentNode.text == "!=" {
			equal = !equal
		}

		return equal, nil
	case "=~", "!~":
		return regexpOperation(currentNode, left, right)
	case "in":
		return containsValue(ctx, right, left)
	case ">", ">=", "<", "<=":
		return compareOperation(currentNode.text, left, right, currentNode.span)
	default:
		return arithmeticBinaryValues(currentNode, left, right)
	}
}

func arithmeticBinaryValues(currentNode *Node, left, right any) (any, error) {
	if isTimeOperand(left) || isTimeOperand(right) {
		value, handled, err := timeOperation(currentNode.text, left, right, currentNode.span)
		if handled {
			return value, err
		}
	}

	return binaryNumericOrString(currentNode.text, left, right, currentNode.span)
}

func regexpOperation(currentNode *Node, left, right any) (bool, error) {
	leftString, leftOK := left.(string)
	if !leftOK {
		return false, evalError(
			ErrType, "evaluate "+currentNode.text, currentNode.span, "",
			newDetailError("left operand must be a string, got %T", left),
		)
	}

	pattern, err := regexpPattern(currentNode, right)
	if err != nil {
		return false, err
	}

	matched := pattern.MatchString(leftString)
	if currentNode.text == "!~" {
		matched = !matched
	}

	return matched, nil
}

func regexpPattern(currentNode *Node, right any) (*regexp.Regexp, error) {
	if pattern, precompiled := currentNode.value.(*regexp.Regexp); precompiled {
		return pattern, nil
	}

	rightString, valid := right.(string)
	if !valid {
		return nil, evalError(
			ErrType, "evaluate "+currentNode.text, currentNode.span, "",
			newDetailError("right operand must be a string, got %T", right),
		)
	}

	pattern, err := regexp.Compile(rightString)
	if err != nil {
		return nil, evalError(ErrType, "compile regular expression", currentNode.children[1].span, "", err)
	}

	return pattern, nil
}

func isTimeOperand(value any) bool {
	switch value.(type) {
	case time.Time, time.Duration:
		return true
	default:
		return false
	}
}

func timeOperation(operator string, left, right any, span Span) (any, bool, error) {
	left, right, err := adaptDurationLiterals(left, right)
	if err != nil {
		return nil, true, evalError(ErrType, "evaluate "+operator, span, "", err)
	}

	if leftTime, valid := left.(time.Time); valid {
		return timeLeftOperation(operator, leftTime, right, span)
	}

	if leftDuration, valid := left.(time.Duration); valid {
		return durationLeftOperation(operator, leftDuration, right, span)
	}

	if isTimeOperand(right) {
		return invalidTimeOperation(operator, left, right, span)
	}

	return nil, false, nil
}

func adaptDurationLiterals(left, right any) (any, any, error) {
	if _, duration := left.(time.Duration); duration {
		if literal, ok := right.(numberLiteral); ok {
			converted, err := convertNumberLiteral(literal, reflect.TypeFor[time.Duration]())
			if err != nil {
				return nil, nil, err
			}

			right = time.Duration(converted.Int())
		}
	}

	if _, duration := right.(time.Duration); duration {
		if literal, ok := left.(numberLiteral); ok {
			converted, err := convertNumberLiteral(literal, reflect.TypeFor[time.Duration]())
			if err != nil {
				return nil, nil, err
			}

			left = time.Duration(converted.Int())
		}
	}

	return left, right, nil
}

func timeLeftOperation(operator string, left time.Time, right any, span Span) (any, bool, error) {
	if duration, valid := right.(time.Duration); valid {
		switch operator {
		case "+":
			return left.Add(duration), true, nil
		case "-":
			if duration == time.Duration(math.MinInt64) {
				return left.Add(time.Duration(math.MaxInt64)).Add(time.Nanosecond), true, nil
			}

			return left.Add(-duration), true, nil
		}
	}

	if rightTime, valid := right.(time.Time); valid && operator == "-" {
		return left.Sub(rightTime), true, nil
	}

	return invalidTimeOperation(operator, left, right, span)
}

func durationLeftOperation(operator string, left time.Duration, right any, span Span) (any, bool, error) {
	if duration, valid := right.(time.Duration); valid && (operator == "+" || operator == "-") {
		value, err := integerOperation(operator, int64(left), int64(duration), span)

		return time.Duration(value), true, err
	}

	if rightTime, valid := right.(time.Time); valid && operator == "+" {
		return rightTime.Add(left), true, nil
	}

	return invalidTimeOperation(operator, left, right, span)
}

func invalidTimeOperation(operator string, left, right any, span Span) (any, bool, error) {
	return nil, true, evalError(
		ErrType, "evaluate "+operator, span, "",
		newDetailError("operator is not valid for %T and %T", left, right),
	)
}

func containsValue(ctx context.Context, container, needle any) (bool, error) {
	err := contextCheckAt(ctx, "search values", emptySpan())
	if err != nil {
		return false, err
	}

	container = normalizeValue(container)

	value := reflect.ValueOf(container)
	if !value.IsValid() {
		return equalValuesContext(ctx, container, needle)
	}

	switch value.Kind() {
	case reflect.Array, reflect.Slice:
		for position := range value.Len() {
			equal, err := equalValuesContext(ctx, value.Index(position).Interface(), needle)
			if err != nil || equal {
				return equal, err
			}
		}

		return false, nil
	case reflect.Map:
		return containsMapValue(ctx, value, needle)
	case reflect.Invalid, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer,
		reflect.String, reflect.Struct, reflect.UnsafePointer:
	}

	return equalValuesContext(ctx, container, needle)
}

func containsMapValue(ctx context.Context, value reflect.Value, needle any) (bool, error) {
	key, err := exactMapKey(needle, value.Type().Key())
	if err == nil {
		if value.MapIndex(key).IsValid() {
			return true, nil
		}

		if exactMapMissIsFinal(value.Type().Key(), key) {
			return false, nil
		}
	}

	iterator := value.MapRange()
	for iterator.Next() {
		equal, comparisonErr := equalValuesContext(ctx, iterator.Key().Interface(), needle)
		if comparisonErr != nil || equal {
			return equal, comparisonErr
		}
	}

	return false, nil
}

func exactMapMissIsFinal(keyType reflect.Type, key reflect.Value) bool {
	if keyType.Kind() == reflect.Interface && isNilValue(key.Interface()) {
		return false
	}

	hashKey, state := makeEqualityHashKey(key.Interface())
	switch state {
	case equalityHashAlwaysDistinct:
		return true
	case equalityHashAvailable:
		if keyType.Kind() != reflect.Interface {
			return true
		}

		kind := key.Kind()

		return !hashKey.numeric && kind != reflect.Bool && kind != reflect.String
	case equalityHashUnavailable, equalityHashRequiresFullScan:
		return false
	}

	return false
}

func preparePlan(ctx context.Context, root *Node) (*Node, *DiagnosticsError) {
	preparer := planPreparer{diagnostics: nil}
	if numericConstantTree(root) {
		if prepared, folded := preparer.foldConstant(ctx, root); folded {
			return prepared, nil
		}

		preparer.diagnostics = nil
	}

	prepared, _ := preparer.prepare(ctx, root)

	if len(preparer.diagnostics) != 0 {
		return nil, newDiagnostics(preparer.diagnostics...)
	}

	return prepared, nil
}

func numericConstantTree(currentNode *Node) bool {
	switch currentNode.kind {
	case NodeLiteral:
		_, numeric := currentNode.value.(numberLiteral)

		return numeric
	case NodeUnary:
		return numericUnaryConstantTree(currentNode)
	case NodeBinary:
		return numericBinaryConstantTree(currentNode)
	case NodeInvalid, NodeIdentifier, NodeConditional, NodeCall, NodeAccess, NodeIndex, NodeList, NodeMap:
		return false
	}

	return false
}

func numericUnaryConstantTree(currentNode *Node) bool {
	switch currentNode.text {
	case "+", "-", "~":
	default:
		return false
	}

	return len(currentNode.children) == 1 && numericConstantTree(currentNode.children[0])
}

func numericBinaryConstantTree(currentNode *Node) bool {
	if _, validOperator := arithmeticInfixPrecedence(currentNode.text); !validOperator {
		return false
	}

	if len(currentNode.children) != binaryOperandCount {
		return false
	}

	return numericConstantTree(currentNode.children[0]) && numericConstantTree(currentNode.children[1])
}

func (p *planPreparer) prepare(ctx context.Context, currentNode *Node) (*Node, bool) {
	canFoldIntoParent := true
	prepared := currentNode

	for position, child := range currentNode.children {
		preparedChild, childCanFoldIntoParent := p.prepare(ctx, child)
		if !childCanFoldIntoParent {
			canFoldIntoParent = false
		}

		if preparedChild != child {
			if prepared == currentNode {
				prepared = copyNode(currentNode)
			}

			prepared.children[position] = preparedChild
		}
	}

	if currentNode.kind == NodeIdentifier || currentNode.kind == NodeCall {
		canFoldIntoParent = false
	}

	var regexpValid bool

	prepared, regexpValid = p.prepareRegexp(currentNode, prepared)
	if !regexpValid {
		return prepared, false
	}

	if !canFoldIntoParent || currentNode.kind == NodeLiteral {
		return prepared, canFoldIntoParent
	}

	return p.foldConstant(ctx, prepared)
}

func (p *planPreparer) prepareRegexp(source, prepared *Node) (*Node, bool) {
	if source.kind != NodeBinary || source.text != "=~" && source.text != "!~" {
		return prepared, true
	}

	patternNode := prepared.children[1]

	pattern, valid := patternNode.value.(string)
	if !valid || patternNode.kind != NodeLiteral {
		return prepared, true
	}

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		p.diagnostics = append(p.diagnostics, newErrorDiagnostic(
			patternNode.span, fmt.Sprintf("invalid regular expression: %v", err),
		))

		return prepared, false
	}

	if prepared == source {
		prepared = copyNode(source)
	}

	prepared.value = compiled

	return prepared, true
}

func (p *planPreparer) foldConstant(ctx context.Context, currentNode *Node) (*Node, bool) {
	value, folded := p.evaluateConstant(ctx, currentNode)
	if !folded {
		return currentNode, false
	}

	return newNode(NodeLiteral, currentNode.span, "constant", value, nil, false), true
}

func (p *planPreparer) evaluateConstant(ctx context.Context, currentNode *Node) (any, bool) {
	state := evalState{
		variables: nil, resolver: nil, functions: nil,
		preserveLiterals: false, stepsUntilContextCheck: 0,
	}

	value, err := state.evaluate(ctx, currentNode)
	if err != nil {
		var sizeLimitErr *constantSizeLimitError
		if errors.As(err, &sizeLimitErr) {
			p.diagnostics = append(p.diagnostics, Diagnostic{
				Code: ErrOverflow, Severity: SeverityError, Span: currentNode.span,
				Message: sizeLimitErr.Error(), Notes: nil,
			})
		}

		return nil, false
	}

	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && (reflected.Kind() == reflect.Map || reflected.Kind() == reflect.Slice) {
		// Mutable constants must be evaluated for each run. Their parents cannot
		// fold without evaluating the same mutable subtree again.
		return nil, false
	}

	return value, true
}

func copyNode(source *Node) *Node {
	clone := newNode(source.kind, source.span, source.text, source.value, nil, source.optional)
	if len(source.children) != 0 {
		clone.children = make([]*Node, len(source.children))
		copy(clone.children, source.children)
	}

	return clone
}

package fasteval

import (
	"fmt"
	"strconv"
)

const (
	// maxChildChunkCapacity bounds retained storage for invalid input while consolidating fixed-arity child lists.
	maxChildChunkCapacity    = 32
	initialNodeChunkCapacity = 8
	conditionalOperandCount  = 3
	maxExpressionDepth       = 1_024
	nodeChunkGrowthFactor    = 2
	minimumEstimatedTokens   = 3
	unaryOperandCount        = 1

	precedenceTernary        = 10
	precedenceLogicalOr      = 20
	precedenceLogicalAnd     = 30
	precedenceComparator     = 40
	precedenceBitwise        = 50
	precedenceShift          = 60
	precedenceAdditive       = 70
	precedenceMultiplicative = 80
	precedenceExponent       = 90
	precedencePrefix         = 100
)

type parser struct {
	tokens          []lexToken
	lineOffsets     []int
	index           int
	nodes           *nodeArena
	expressionDepth int
	depthFailure    *parseFailure
}

// nodeArena owns backing storage that remains reachable through returned node pointers.
type nodeArena struct {
	chunk                  []Node
	index                  int
	remainingCapacity      int
	childChunk             []*Node
	childIndex             int
	remainingChildCapacity int
}

type parseFailure struct {
	span    Span
	message string
}

func parse(source string) (*Node, *DiagnosticsError) {
	lineOffsets := sourceLineOffsets(source)
	tokenizer := lexer{
		source: source, offset: 0, lineOffsets: lineOffsets,
		listBracketOffsets: nil, token: emptyLexToken(), classifyBrackets: false, canEnd: false,
	}
	tokens := make([]lexToken, 0, initialTokenCapacity)

	for tokenizer.offset < len(source) {
		token, diagnostic := tokenizer.next()
		if diagnostic != nil {
			return nil, newDiagnostics(*diagnostic)
		}

		if token.kind != tokenEOF {
			tokens = append(tokens, token)
		}

		if len(tokens) == cap(tokens)-1 {
			// Reserve one slot for EOF; larger sources continue in a separate stack frame.
			tokenizer.skipWhitespace()

			if tokenizer.offset < len(source) {
				return parseWithLargeTokenBuffer(tokenizer, tokens)
			}
		}
	}

	tokens = append(tokens, newLexToken(tokenEOF, "", nil, len(source), len(source)))

	return parseTokens(tokens, lineOffsets)
}

func parseWithLargeTokenBuffer(
	tokenizer lexer,
	tokens []lexToken,
) (*Node, *DiagnosticsError) {
	// Keep collection in this function so the fixed backing storage remains stack-local.
	largeTokens := make([]lexToken, len(tokens), largeTokenCapacity)
	copy(largeTokens, tokens)

	for tokenizer.offset < len(tokenizer.source) {
		token, diagnostic := tokenizer.next()
		if diagnostic != nil {
			return nil, newDiagnostics(*diagnostic)
		}

		if token.kind != tokenEOF {
			largeTokens = append(largeTokens, token)
		}
	}

	sourceLength := len(tokenizer.source)
	largeTokens = append(largeTokens, newLexToken(tokenEOF, "", nil, sourceLength, sourceLength))

	return parseTokens(largeTokens, tokenizer.lineOffsets)
}

func parseTokens(tokens []lexToken, lineOffsets []int) (*Node, *DiagnosticsError) {
	nodeCapacity, childCapacity := 0, 0
	if len(tokens) >= minimumEstimatedTokens {
		nodeCapacity, childCapacity = estimatedASTCapacity(tokens)
	}

	initialCapacity := min(nodeCapacity, initialNodeChunkCapacity)
	nodes := nodeArena{
		chunk: make([]Node, initialCapacity), index: 0,
		remainingCapacity:      nodeCapacity - initialCapacity,
		childChunk:             nil,
		childIndex:             0,
		remainingChildCapacity: childCapacity,
	}

	parserState := &parser{
		tokens: tokens, lineOffsets: lineOffsets, index: 0, nodes: &nodes,
		expressionDepth: 0, depthFailure: nil,
	}
	if parserState.peek().kind == tokenEOF {
		return nil, newDiagnostics(newErrorDiagnostic(parserState.tokenSpan(parserState.peek()), "expression is empty"))
	}

	root, failure := parserState.expression(0)
	if failure == nil && parserState.peek().kind != tokenEOF {
		failure = parserState.fail(
			parserState.peek(), "unexpected token "+strconv.Quote(parserState.peek().text),
		)
	}

	if failure == nil {
		failure = parserState.depthFailure
	}

	if failure != nil {
		return nil, newDiagnostics(newErrorDiagnostic(failure.span, failure.message))
	}

	return root, nil
}

func (p *parser) expression(minPrecedence int) (*Node, *parseFailure) {
	if p.expressionDepth >= maxExpressionDepth {
		return nil, p.fail(
			p.peek(), fmt.Sprintf("expression nesting exceeds maximum depth of %d", maxExpressionDepth),
		)
	}

	p.expressionDepth++
	root, failure := p.expressionAtDepth(minPrecedence)
	p.expressionDepth--

	return root, failure
}

func (p *parser) expressionAtDepth(minPrecedence int) (*Node, *parseFailure) {
	left, failure := p.prefix()
	if failure != nil {
		return nil, failure
	}

	for {
		left, failure = p.postfix(left)
		if failure != nil {
			return nil, failure
		}

		token := p.peek()
		if token.kind == tokenQuestion {
			if precedenceTernary < minPrecedence {
				break
			}

			left, failure = p.conditional(left, token)
			if failure != nil {
				return nil, failure
			}

			continue
		}

		operator, precedence, ok := infix(token)
		if !ok || precedence < minPrecedence {
			break
		}

		p.take()

		right, nestedFailure := p.expression(precedence + 1)
		if nestedFailure != nil {
			return nil, nestedFailure
		}

		children := p.nodes.retainFixedChildren(left, right)
		left = p.newNode(NodeBinary, mergeSpan(left.span, right.span), operator, nil, children, false)
	}

	return left, nil
}

func (p *parser) conditional(condition *Node, token lexToken) (*Node, *parseFailure) {
	p.take()

	whenTrue, failure := p.expression(0)
	if failure != nil {
		return nil, failure
	}

	var whenFalse *Node

	end := whenTrue.span.End

	if p.peek().kind == tokenColon {
		p.take()

		whenFalse, failure = p.expression(precedenceTernary)
		if failure != nil {
			return nil, failure
		}

		end = whenFalse.span.End
	} else {
		whenFalse = p.newNode(NodeLiteral, p.tokenSpan(token), "nil", nil, nil, false)
	}

	tokenSpan := p.tokenSpan(token)
	conditionalSpan := Span{
		Start: tokenSpan.Start, End: end, Line: tokenSpan.Line, Column: tokenSpan.Column,
	}

	return p.newNode(
		NodeConditional, mergeSpan(condition.span, conditionalSpan), "?:", nil,
		p.nodes.retainFixedChildren(condition, whenTrue, whenFalse), false,
	), nil
}

func (p *parser) prefix() (*Node, *parseFailure) {
	token := p.take()
	switch token.kind {
	case tokenNumber, tokenString, tokenTrue, tokenFalse, tokenNil, tokenIdentifier:
		return p.prefixValue(token), nil
	case tokenOperator:
		return p.prefixOperator(token)
	case tokenLParen:
		return p.parenthesized(token)
	case tokenLBracket:
		return p.list(token)
	case tokenLBrace:
		return p.mapLiteral(token)
	case tokenEOF:
		return nil, p.fail(token, "expected an expression")
	case tokenRParen, tokenRBracket, tokenRBrace, tokenComma, tokenColon, tokenDot,
		tokenQuestion, tokenOptionalDot, tokenOptionalIndex:
		return nil, p.fail(token, fmt.Sprintf("expected an expression, found %q", token.text))
	}

	return nil, p.fail(token, "unknown token kind")
}

func (p *parser) prefixValue(token lexToken) *Node {
	span := p.tokenSpan(token)

	switch token.kind {
	case tokenNumber, tokenString:
		return p.newNode(NodeLiteral, span, token.text, token.value, nil, false)
	case tokenTrue:
		return p.newNode(NodeLiteral, span, token.text, true, nil, false)
	case tokenFalse:
		return p.newNode(NodeLiteral, span, token.text, false, nil, false)
	case tokenNil:
		return p.newNode(NodeLiteral, span, token.text, nil, nil, false)
	case tokenIdentifier:
		return p.newNode(NodeIdentifier, span, token.text, nil, nil, false)
	case tokenEOF, tokenLParen, tokenRParen, tokenLBracket, tokenRBracket, tokenLBrace, tokenRBrace,
		tokenComma, tokenColon, tokenDot, tokenQuestion, tokenOptionalDot, tokenOptionalIndex, tokenOperator:
		return nil
	}

	return nil
}

func (p *parser) prefixOperator(token lexToken) (*Node, *parseFailure) {
	if token.text != "-" && token.text != "!" && token.text != "~" && token.text != "+" {
		return nil, p.fail(token, fmt.Sprintf("operator %q requires a left operand", token.text))
	}

	operand, failure := p.expression(precedencePrefix)
	if failure != nil {
		return nil, failure
	}

	return p.newNode(
		NodeUnary, mergeSpan(p.tokenSpan(token), operand.span), token.text, nil,
		p.nodes.retainFixedChildren(operand), false,
	), nil
}

func (p *parser) postfix(left *Node) (*Node, *parseFailure) {
	for {
		token := p.peek()
		switch token.kind {
		case tokenLParen:
			p.take()

			arguments, closeToken, failure := p.arguments(tokenRParen)
			if failure != nil {
				return nil, failure
			}

			children := make([]*Node, 1, len(arguments)+1)
			children[0] = left
			children = append(children, arguments...)
			left = p.newNode(NodeCall, mergeSpan(left.span, p.tokenSpan(closeToken)), "call", nil, children, false)
		case tokenDot, tokenOptionalDot:
			p.take()

			name := p.take()
			if name.kind != tokenIdentifier {
				return nil, p.fail(name, "expected an accessor name")
			}

			left = p.newNode(
				NodeAccess, mergeSpan(left.span, p.tokenSpan(name)), name.text, nil,
				p.nodes.retainFixedChildren(left),
				token.kind == tokenOptionalDot,
			)
		case tokenLBracket, tokenOptionalIndex:
			p.take()

			index, failure := p.expression(0)
			if failure != nil {
				return nil, failure
			}

			closeToken := p.take()
			if closeToken.kind != tokenRBracket {
				return nil, p.fail(closeToken, "expected closing bracket")
			}

			left = p.newNode(
				NodeIndex, mergeSpan(left.span, p.tokenSpan(closeToken)), "index", nil,
				p.nodes.retainFixedChildren(left, index),
				token.kind == tokenOptionalIndex,
			)
		case tokenEOF, tokenIdentifier, tokenNumber, tokenString, tokenTrue, tokenFalse,
			tokenNil, tokenRParen, tokenRBracket, tokenLBrace, tokenRBrace, tokenComma,
			tokenColon, tokenQuestion, tokenOperator:
			return left, nil
		}
	}
}

func (p *parser) parenthesized(open lexToken) (*Node, *parseFailure) {
	if p.peek().kind == tokenRParen {
		return nil, p.fail(p.peek(), "empty parentheses are not an expression")
	}

	first, failure := p.expression(0)
	if failure != nil {
		return nil, failure
	}

	if p.peek().kind != tokenComma {
		closeToken := p.take()
		if closeToken.kind != tokenRParen {
			return nil, p.fail(closeToken, "expected closing parenthesis")
		}

		first.span = mergeSpan(p.tokenSpan(open), p.tokenSpan(closeToken))

		return first, nil
	}

	items := []*Node{first}

	for p.peek().kind == tokenComma {
		p.take()

		if p.peek().kind == tokenRParen {
			break
		}

		item, nestedFailure := p.expression(0)
		if nestedFailure != nil {
			return nil, nestedFailure
		}

		items = append(items, item)
	}

	closeToken := p.take()
	if closeToken.kind != tokenRParen {
		return nil, p.fail(closeToken, "expected closing parenthesis")
	}

	return p.newNode(NodeList, mergeSpan(p.tokenSpan(open), p.tokenSpan(closeToken)), "list", nil, items, false), nil
}

func (p *parser) list(open lexToken) (*Node, *parseFailure) {
	items, closeToken, failure := p.arguments(tokenRBracket)
	if failure != nil {
		return nil, failure
	}

	return p.newNode(NodeList, mergeSpan(p.tokenSpan(open), p.tokenSpan(closeToken)), "list", nil, items, false), nil
}

func (p *parser) mapLiteral(open lexToken) (*Node, *parseFailure) {
	var entries []*Node

	if p.peek().kind == tokenRBrace {
		closeToken := p.take()

		return p.newNode(NodeMap, mergeSpan(p.tokenSpan(open), p.tokenSpan(closeToken)), "map", nil, nil, false), nil
	}

	for {
		key, failure := p.expression(precedenceTernary + 1)
		if failure != nil {
			return nil, failure
		}

		colon := p.take()
		if colon.kind != tokenColon {
			return nil, p.fail(colon, "expected ':' after map key")
		}

		value, nestedFailure := p.expression(0)
		if nestedFailure != nil {
			return nil, nestedFailure
		}

		entries = append(entries, key, value)

		if p.peek().kind != tokenComma {
			break
		}

		p.take()

		if p.peek().kind == tokenRBrace {
			break
		}
	}

	closeToken := p.take()
	if closeToken.kind != tokenRBrace {
		return nil, p.fail(closeToken, "expected closing brace")
	}

	return p.newNode(NodeMap, mergeSpan(p.tokenSpan(open), p.tokenSpan(closeToken)), "map", nil, entries, false), nil
}

func estimatedASTCapacity(tokens []lexToken) (int, int) {
	nodeCount, childCount := 0, 0

	for position, token := range tokens {
		nodeCount += estimatedTokenNodes(tokens, position, token)
		childCount += estimatedFixedTokenChildren(tokens, position, token)
	}

	return nodeCount, childCount
}

func estimatedTokenNodes(tokens []lexToken, position int, token lexToken) int {
	switch token.kind {
	case tokenIdentifier:
		if position == 0 || tokens[position-1].kind != tokenDot &&
			tokens[position-1].kind != tokenOptionalDot {
			return 1
		}
	case tokenNumber, tokenString, tokenTrue, tokenFalse, tokenNil,
		tokenLBracket, tokenLBrace, tokenDot, tokenOptionalDot, tokenOptionalIndex,
		tokenQuestion, tokenOperator:
		return 1
	case tokenLParen:
		if position != 0 && tokenCanEnd(tokens[position-1].kind) {
			return 1
		}
	case tokenEOF, tokenRParen, tokenRBracket, tokenRBrace, tokenComma, tokenColon:
	}

	return 0
}

func estimatedFixedTokenChildren(tokens []lexToken, position int, token lexToken) int {
	previousCanEnd := position != 0 && tokenCanEnd(tokens[position-1].kind)

	switch token.kind {
	case tokenDot, tokenOptionalDot:
		return unaryOperandCount
	case tokenOptionalIndex:
		return binaryOperandCount
	case tokenQuestion:
		return conditionalOperandCount
	case tokenLBracket:
		if previousCanEnd {
			return binaryOperandCount
		}
	case tokenOperator:
		if previousCanEnd {
			return binaryOperandCount
		}

		return unaryOperandCount
	case tokenEOF, tokenIdentifier, tokenNumber, tokenString, tokenTrue, tokenFalse, tokenNil,
		tokenLParen, tokenRParen, tokenRBracket, tokenLBrace, tokenRBrace, tokenComma, tokenColon:
	}

	return 0
}

func (p *parser) newNode(
	kind NodeKind,
	span Span,
	text string,
	value any,
	children []*Node,
	optional bool,
) *Node {
	depthValue := 1
	for _, child := range children {
		depthValue = max(depthValue, int(child.depth)+1)
	}

	depthValue = min(depthValue, maxExpressionDepth+1)

	depth := uint16(depthValue)

	if depthValue > maxExpressionDepth && p.depthFailure == nil {
		p.depthFailure = &parseFailure{
			span: span, message: fmt.Sprintf("expression nesting exceeds maximum depth of %d", maxExpressionDepth),
		}
	}

	currentNode := p.nodes.allocate()
	*currentNode = Node{
		kind: kind, depth: depth, span: span, text: text,
		value: value, children: children, optional: optional,
	}

	return currentNode
}

func (a *nodeArena) allocate() *Node {
	if a.index == len(a.chunk) {
		if a.remainingCapacity == 0 {
			return new(Node)
		}

		nextCapacity := min(len(a.chunk)*nodeChunkGrowthFactor, a.remainingCapacity)
		// Nodes in earlier chunks remain reachable through the pointers
		// already stored in the syntax tree.
		a.chunk = make([]Node, nextCapacity)
		a.index = 0
		a.remainingCapacity -= nextCapacity
	}

	node := &a.chunk[a.index]
	a.index++

	return node
}

// retainFixedChildren copies a fixed-arity child list into AST-owned storage.
func (a *nodeArena) retainFixedChildren(children ...*Node) []*Node {
	if len(a.childChunk)-a.childIndex < len(children) {
		nextCapacity := len(children)
		if a.remainingChildCapacity != 0 {
			nextCapacity = min(maxChildChunkCapacity, a.remainingChildCapacity)
			nextCapacity = max(nextCapacity, len(children))
		}

		a.childChunk = make([]*Node, nextCapacity)
		a.childIndex = 0
		a.remainingChildCapacity = max(a.remainingChildCapacity-nextCapacity, 0)
	}

	start := a.childIndex
	a.childIndex += len(children)
	// Limit capacity so a future append cannot overwrite the next node's children.
	retained := a.childChunk[start:a.childIndex:a.childIndex]
	copy(retained, children)

	return retained
}

func (p *parser) arguments(closeKind tokenKind) ([]*Node, lexToken, *parseFailure) {
	if p.peek().kind == closeKind {
		return nil, p.take(), nil
	}

	var items []*Node

	for {
		item, failure := p.expression(0)
		if failure != nil {
			return nil, emptyLexToken(), failure
		}

		items = append(items, item)

		if p.peek().kind != tokenComma {
			break
		}

		p.take()

		if p.peek().kind == closeKind {
			break
		}
	}

	closeToken := p.take()
	if closeToken.kind != closeKind {
		return nil, emptyLexToken(), p.fail(closeToken, "expected a closing delimiter")
	}

	return items, closeToken, nil
}

func infix(token lexToken) (string, int, bool) {
	if token.kind != tokenOperator {
		return "", 0, false
	}

	precedence, found := logicalInfixPrecedence(token.text)
	if found {
		return token.text, precedence, true
	}

	precedence, found = arithmeticInfixPrecedence(token.text)

	return token.text, precedence, found
}

func logicalInfixPrecedence(operator string) (int, bool) {
	switch operator {
	case "??":
		return precedenceTernary, true
	case "||":
		return precedenceLogicalOr, true
	case "&&":
		return precedenceLogicalAnd, true
	case "==", "!=", ">", ">=", "<", "<=", "=~", "!~", "in":
		return precedenceComparator, true
	default:
		return 0, false
	}
}

func arithmeticInfixPrecedence(operator string) (int, bool) {
	switch operator {
	case "&", "|", "^":
		return precedenceBitwise, true
	case "<<", ">>":
		return precedenceShift, true
	case "+", "-":
		return precedenceAdditive, true
	case "*", "/", "%":
		return precedenceMultiplicative, true
	case "**":
		return precedenceExponent, true
	default:
		return 0, false
	}
}

func (p *parser) peek() lexToken { return p.tokens[p.index] }

func (p *parser) tokenSpan(token lexToken) Span {
	return spanFromLineOffsets(p.lineOffsets, token.start, token.end)
}

func (p *parser) take() lexToken {
	token := p.tokens[p.index]
	if token.kind != tokenEOF {
		p.index++
	}

	return token
}

func (p *parser) fail(token lexToken, message string) *parseFailure {
	return &parseFailure{span: p.tokenSpan(token), message: message}
}

func mergeSpan(left, right Span) Span {
	return Span{Start: left.Start, End: right.End, Line: left.Line, Column: left.Column}
}

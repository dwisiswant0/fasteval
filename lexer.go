package fasteval

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind uint8

const (
	decimalRadix                     = uint64(10)
	initialTokenCapacity             = 8
	largeTokenCapacity               = 32
	maxExactFloat64Integer           = uint64(1) << 53
	maxSimpleDecimalDigits           = 20
	maxSimpleDecimalFractionalDigits = 19
	nilKeyword                       = "nil"
)

//nolint:gochecknoglobals // This immutable backing array avoids one allocation for single-line sources.
var singleLineOffsets = [...]int{0}

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenNumber
	tokenString
	tokenTrue
	tokenFalse
	tokenNil
	tokenLParen
	tokenRParen
	tokenLBracket
	tokenRBracket
	tokenLBrace
	tokenRBrace
	tokenComma
	tokenColon
	tokenDot
	tokenQuestion
	tokenOptionalDot
	tokenOptionalIndex
	tokenOperator
)

type lexToken struct {
	kind  tokenKind
	text  string
	value any
	start int
	end   int
}

func newLexToken(kind tokenKind, text string, value any, start, end int) lexToken {
	return lexToken{kind: kind, text: text, value: value, start: start, end: end}
}

func emptyLexToken() lexToken {
	return newLexToken(tokenEOF, "", nil, 0, 0)
}

type numberLiteral struct {
	// Keep the common float64 cache together and pack the wide fields.
	// A nil value with non-empty text is a validated lazy decimal; consumers
	// must use exactValue and constantKind instead of reading value directly.
	value        constant.Value
	text         string
	float64Box   any
	float64Value float64
	defaultOK    bool
	float32OK    bool
	float64OK    bool
	complexOK    bool
	float32Value float32
	defaultValue any
	complexBox   any
}

func emptyNumberLiteral() numberLiteral {
	return numberLiteral{
		value: nil, text: "", defaultValue: nil, defaultOK: false,
		float32Value: 0, float32OK: false, float64Value: 0, float64OK: false, float64Box: nil,
		complexOK: false, complexBox: nil,
	}
}

type lexer struct {
	source             string
	offset             int
	lineOffsets        []int
	listBracketOffsets []bool
	// token is the value produced by the most recent successful call to next.
	token            lexToken
	classifyBrackets bool
	canEnd           bool
}

func sourceLineOffsets(source string) []int {
	offsets := singleLineOffsets[:]
	searchStart := 0

	for searchStart < len(source) {
		newline := strings.IndexByte(source[searchStart:], '\n')
		if newline < 0 {
			break
		}

		searchStart += newline + 1
		offsets = append(offsets, searchStart)
	}

	return offsets
}

func (l *lexer) next() (lexToken, *Diagnostic) {
	l.token = emptyLexToken()
	l.skipWhitespace()

	if l.offset == len(l.source) {
		return l.token, nil
	}

	start := l.offset

	runeValue, size := utf8.DecodeRuneInString(l.source[l.offset:])
	if runeValue == utf8.RuneError && size == 1 {
		return l.token, l.diagnostic(start, start+1, "invalid UTF-8 byte")
	}

	handled, diagnostic := l.tryEscapedIdentifier(runeValue)
	if diagnostic != nil || handled {
		return l.token, diagnostic
	}

	handled, diagnostic = l.lexValue(runeValue, size)
	if handled {
		return l.token, diagnostic
	}

	if l.emitMultiCharacterOperator(start) {
		return l.token, nil
	}

	diagnostic = l.emitSingleCharacterToken(start, runeValue, size)

	return l.token, diagnostic
}

func (l *lexer) lexValue(runeValue rune, size int) (bool, *Diagnostic) {
	if isIdentifierStart(runeValue) {
		return true, l.identifier()
	}

	if l.isNumberStart(runeValue) {
		return true, l.number()
	}

	if runeValue == '\'' || runeValue == '"' {
		return true, l.stringLiteral(runeValue, size)
	}

	return false, nil
}

func (l *lexer) skipWhitespace() {
	for l.offset < len(l.source) {
		runeValue, size := utf8.DecodeRuneInString(l.source[l.offset:])
		if !unicode.IsSpace(runeValue) {
			break
		}

		l.offset += size
	}
}

func (l *lexer) tryEscapedIdentifier(runeValue rune) (bool, *Diagnostic) {
	if l.canEnd || runeValue != '[' {
		return false, nil
	}

	if l.classifyBrackets {
		if l.listBracketOffsets == nil {
			l.listBracketOffsets, _ = l.classifyListBracketsState()
		}

		if l.listBracketOffsets[l.offset] {
			return false, nil
		}
	}

	token, found, diagnostic := l.escapedIdentifier()
	if diagnostic != nil || !found {
		if diagnostic == nil {
			l.classifyBrackets = true
		}

		return found, diagnostic
	}

	l.emit(token)

	return true, nil
}

type bracketFrame struct {
	offset   int
	hasComma bool
}

func (l *lexer) classifyListBracketsState() ([]bool, bool) {
	listBrackets := make([]bool, len(l.source))

	var brackets []bracketFrame

	uncertain := false

	for cursor := 0; cursor < len(l.source); {
		runeValue, size := utf8.DecodeRuneInString(l.source[cursor:])
		if runeValue == utf8.RuneError && size == 1 {
			return listBrackets, uncertain
		}

		next, handled, quoteUncertain, diagnostic := l.skipIdentifierContent(cursor, runeValue, size)
		if diagnostic != nil {
			return listBrackets, uncertain
		}

		uncertain = uncertain || quoteUncertain

		if handled {
			cursor = next

			continue
		}

		classifyBracketRune(l.source, runeValue, cursor, &brackets, listBrackets)

		cursor += size
	}

	return listBrackets, uncertain
}

func (l *lexer) skipIdentifierContent(
	cursor int,
	runeValue rune,
	size int,
) (int, bool, bool, *Diagnostic) {
	if runeValue == '\\' {
		next, diagnostic := l.skipEscapedIdentifierRune(cursor, size)

		return next, true, false, diagnostic
	}

	if runeValue != '\'' && runeValue != '"' {
		return cursor, false, false, nil
	}

	next, closed := l.quotedContentEnd(cursor, runeValue, size)
	if closed {
		return next, true, false, nil
	}

	return cursor, false, true, nil
}

func classifyBracketRune(
	source string,
	runeValue rune,
	cursor int,
	brackets *[]bracketFrame,
	listBrackets []bool,
) {
	switch runeValue {
	case '[':
		*brackets = append(*brackets, bracketFrame{offset: cursor, hasComma: false})
	case ',':
		if len(*brackets) != 0 {
			(*brackets)[len(*brackets)-1].hasComma = true
		}
	case ']':
		if len(*brackets) != 0 {
			last := len(*brackets) - 1
			bracket := (*brackets)[last]
			*brackets = (*brackets)[:last]

			listBrackets[bracket.offset] = bracket.hasComma || cursor == bracket.offset+1 ||
				bracketHasReceiver(source, bracket.offset)
		}
	}
}

type delimiterNesting struct {
	quote                    byte
	quoteEscaped             bool
	identifierEscaped        bool
	parentheses              int
	brackets                 int
	braces                   int
	identifierBracketNesting int
}

func (l *lexer) findTemplateEnd(startDelimiter, endDelimiter string) (int, bool, bool, error) {
	state := delimiterNesting{
		quote: 0, quoteEscaped: false, identifierEscaped: false,
		parentheses: 0, brackets: 0, braces: 0, identifierBracketNesting: 0,
	}

	var listBrackets []bool

	uncertain := false
	if strings.IndexByte(l.source, '[') >= 0 {
		listBrackets, uncertain = l.classifyListBracketsState()
	}

	for cursor := 0; cursor < len(l.source); cursor++ {
		next, skipped := l.skipBracketedIdentifierQuote(&state, cursor)
		if skipped {
			cursor = next

			continue
		}

		if state.consumeTemplateState(l.source[cursor]) {
			continue
		}

		if state.matchesDelimiter(l.source[cursor:], endDelimiter) {
			return cursor, true, uncertain, nil
		}

		if isQuoteByte(l.source[cursor]) {
			state.quote = l.source[cursor]

			continue
		}

		if state.matchesDelimiter(l.source[cursor:], startDelimiter) {
			return 0, false, uncertain, newDetailError("nested template tags are not supported")
		}

		listBracket := listBrackets != nil && listBrackets[cursor]
		state.consumeDelimiter(l.source[cursor], listBracket)
	}

	return 0, false, uncertain, nil
}

func (l *lexer) skipBracketedIdentifierQuote(state *delimiterNesting, cursor int) (int, bool) {
	if state.identifierBracketNesting == 0 || !isQuoteByte(l.source[cursor]) {
		return cursor, false
	}

	quotedEnd, closed := l.quotedContentEnd(cursor, rune(l.source[cursor]), 1)

	return quotedEnd - 1, closed
}

func (s *delimiterNesting) consumeTemplateState(character byte) bool {
	return s.consumeBracketedIdentifier(character) || s.consumeQuoted(character)
}

func (s *delimiterNesting) matchesDelimiter(source, delimiter string) bool {
	return s.topLevel() && strings.HasPrefix(source, delimiter)
}

func isQuoteByte(character byte) bool {
	return character == '\'' || character == '"'
}

func (s *delimiterNesting) consumeQuoted(character byte) bool {
	if s.quote == 0 {
		return false
	}

	if s.quoteEscaped {
		s.quoteEscaped = false

		return true
	}

	if character == '\\' {
		s.quoteEscaped = true

		return true
	}

	if character == s.quote {
		s.quote = 0
	}

	return true
}

func (s *delimiterNesting) topLevel() bool {
	return s.parentheses == 0 && s.brackets == 0 && s.braces == 0 && s.identifierBracketNesting == 0
}

func (s *delimiterNesting) consumeBracketedIdentifier(character byte) bool {
	if s.identifierBracketNesting == 0 {
		return false
	}

	if s.identifierEscaped {
		s.identifierEscaped = false

		return true
	}

	if character == '\\' {
		s.identifierEscaped = true

		return true
	}

	switch character {
	case '[':
		s.identifierBracketNesting++
	case ']':
		s.identifierBracketNesting--
	}

	return true
}

func (s *delimiterNesting) consumeDelimiter(character byte, listBracket bool) {
	switch character {
	case '(':
		s.parentheses++
	case ')':
		decrementPositive(&s.parentheses)
	case '[':
		if listBracket {
			s.brackets++
		} else {
			s.identifierBracketNesting = 1
		}
	case ']':
		decrementPositive(&s.brackets)
	case '{':
		s.braces++
	case '}':
		decrementPositive(&s.braces)
	}
}

func decrementPositive(value *int) {
	if *value > 0 {
		*value--
	}
}

func bracketHasReceiver(source string, offset int) bool {
	cursor := offset
	for cursor > 0 {
		runeValue, size := utf8.DecodeLastRuneInString(source[:cursor])
		if !unicode.IsSpace(runeValue) {
			return runeHasReceiver(source, cursor, offset, runeValue, size)
		}

		cursor -= size
	}

	return false
}

func runeHasReceiver(source string, cursor, offset int, runeValue rune, size int) bool {
	if runeValue == '?' {
		return cursor == offset
	}

	if isReceiverTerminalRune(runeValue) {
		return true
	}

	if !unicode.IsLetter(runeValue) && runeValue != '_' {
		return false
	}

	wordStart := cursor - size
	for wordStart > 0 {
		previous, previousSize := utf8.DecodeLastRuneInString(source[:wordStart])
		if !unicode.IsLetter(previous) && !unicode.IsDigit(previous) && previous != '_' {
			break
		}

		wordStart -= previousSize
	}

	word := source[wordStart:cursor]

	return word != "in" && word != "IN"
}

func isReceiverTerminalRune(runeValue rune) bool {
	return strings.ContainsRune(")]}'\"", runeValue) || unicode.IsDigit(runeValue)
}

func isIdentifierStart(runeValue rune) bool {
	return unicode.IsLetter(runeValue) || runeValue == '_' || runeValue == '\\'
}

func (l *lexer) isNumberStart(runeValue rune) bool {
	if unicode.IsDigit(runeValue) {
		return true
	}

	return runeValue == '.' && l.offset+1 < len(l.source) && isASCIIDigit(l.source[l.offset+1])
}

func (l *lexer) emitMultiCharacterOperator(start int) bool {
	if l.offset+1 >= len(l.source) {
		return false
	}

	operator := l.source[l.offset : l.offset+2]
	kind := tokenOperator

	switch operator {
	case "**", ">=", "<=", "==", "!=", "=~", "!~", "&&", "||", "<<", ">>", "??":
	case "?.":
		kind = tokenOptionalDot
	case "?[":
		kind = tokenOptionalIndex
	default:
		return false
	}

	l.offset += len(operator)
	l.emit(newLexToken(kind, operator, nil, start, l.offset))

	return true
}

func (l *lexer) emitSingleCharacterToken(start int, runeValue rune, size int) *Diagnostic {
	l.offset += size

	kind, ok := singleCharacterTokenKind(runeValue)
	if !ok {
		return l.diagnostic(start, l.offset, fmt.Sprintf("invalid token %q", string(runeValue)))
	}

	l.emit(newLexToken(kind, l.source[start:l.offset], nil, start, l.offset))

	return nil
}

func singleCharacterTokenKind(runeValue rune) (tokenKind, bool) {
	if strings.ContainsRune("+-*/%&|^!~><", runeValue) {
		return tokenOperator, true
	}

	if kind, ok := delimiterTokenKind(runeValue); ok {
		return kind, true
	}

	return separatorTokenKind(runeValue)
}

func delimiterTokenKind(runeValue rune) (tokenKind, bool) {
	switch runeValue {
	case '(':
		return tokenLParen, true
	case ')':
		return tokenRParen, true
	case '[':
		return tokenLBracket, true
	case ']':
		return tokenRBracket, true
	case '{':
		return tokenLBrace, true
	case '}':
		return tokenRBrace, true
	default:
		return tokenEOF, false
	}
}

func separatorTokenKind(runeValue rune) (tokenKind, bool) {
	switch runeValue {
	case ',':
		return tokenComma, true
	case ':':
		return tokenColon, true
	case '.':
		return tokenDot, true
	case '?':
		return tokenQuestion, true
	default:
		return tokenEOF, false
	}
}

func (l *lexer) emit(token lexToken) {
	l.token = token
	l.canEnd = tokenCanEnd(token.kind)
}

func tokenCanEnd(kind tokenKind) bool {
	switch kind {
	case tokenIdentifier, tokenNumber, tokenString, tokenTrue, tokenFalse, tokenNil,
		tokenRParen, tokenRBracket, tokenRBrace:
		return true
	case tokenEOF, tokenLParen, tokenLBracket, tokenLBrace, tokenComma, tokenColon,
		tokenDot, tokenQuestion, tokenOptionalDot, tokenOptionalIndex, tokenOperator:
		return false
	}

	return false
}

func (l *lexer) escapedIdentifier() (lexToken, bool, *Diagnostic) {
	start := l.offset
	cursor := start + 1
	hasComma := false
	depth := 1

	for cursor < len(l.source) {
		runeValue, size := utf8.DecodeRuneInString(l.source[cursor:])
		if invalidDecodedRune(runeValue, size) {
			return emptyLexToken(), false, l.diagnostic(cursor, cursor+1, "invalid UTF-8 byte")
		}

		next, handled, _, diagnostic := l.skipIdentifierContent(cursor, runeValue, size)
		if diagnostic != nil {
			return emptyLexToken(), false, diagnostic
		}

		if handled {
			cursor = next

			continue
		}

		if runeValue == '[' {
			depth++
		}

		if runeValue == ',' && depth == 1 {
			hasComma = true
		}

		if runeValue == ']' {
			depth--
			if depth == 0 {
				token, ok := l.finishEscapedIdentifier(start, cursor, hasComma)

				return token, ok, nil
			}
		}

		cursor += size
	}

	return emptyLexToken(), false, l.diagnostic(start, len(l.source), "unclosed parameter bracket")
}

func (l *lexer) quotedContentEnd(cursor int, quote rune, quoteSize int) (int, bool) {
	cursor += quoteSize
	escaped := false

	for cursor < len(l.source) {
		runeValue, size := utf8.DecodeRuneInString(l.source[cursor:])
		if runeValue == utf8.RuneError && size == 1 {
			return cursor, false
		}

		if !escaped && runeValue == quote {
			return cursor + size, true
		}

		if !escaped && runeValue == '\\' {
			escaped = true
		} else {
			escaped = false
		}

		cursor += size
	}

	return cursor, false
}

func (l *lexer) skipEscapedIdentifierRune(cursor, size int) (int, *Diagnostic) {
	cursor += size
	if cursor == len(l.source) {
		return cursor, nil
	}

	escapedRune, escapedSize := utf8.DecodeRuneInString(l.source[cursor:])
	if escapedRune == utf8.RuneError && escapedSize == 1 {
		return cursor, l.diagnostic(cursor, cursor+1, "invalid UTF-8 byte")
	}

	return cursor + escapedSize, nil
}

func (l *lexer) finishEscapedIdentifier(start, cursor int, hasComma bool) (lexToken, bool) {
	if hasComma {
		return emptyLexToken(), false
	}

	name := unescapeIdentifier(l.source[start+1 : cursor])
	if name == "" {
		return emptyLexToken(), false
	}

	l.offset = cursor + 1

	return newLexToken(tokenIdentifier, name, nil, start, l.offset), true
}

func (l *lexer) identifier() *Diagnostic {
	start := l.offset

	var (
		builder strings.Builder
		escaped bool
	)

	for l.offset < len(l.source) {
		character := l.source[l.offset]
		if character < utf8.RuneSelf {
			consumed, diagnostic := l.consumeASCIIIdentifier(character, start, &builder, &escaped)
			if diagnostic != nil {
				return diagnostic
			}

			if !consumed {
				break
			}

			continue
		}

		if !l.consumeUnicodeIdentifier(&builder, escaped) {
			break
		}
	}

	text := l.source[start:l.offset]
	if escaped {
		text = builder.String()
	}

	kind, text := identifierKind(text)
	l.emit(newLexToken(kind, text, nil, start, l.offset))

	return nil
}

func (l *lexer) consumeASCIIIdentifier(
	character byte,
	start int,
	builder *strings.Builder,
	escaped *bool,
) (bool, *Diagnostic) {
	if character == '\\' {
		if !*escaped {
			*escaped = true

			builder.WriteString(l.source[start:l.offset])
		}

		diagnostic := l.appendEscapedIdentifierRune(builder, start, 1)

		return true, diagnostic
	}

	if !isASCIIAlphaNumeric(character) && character != '_' {
		return false, nil
	}

	if *escaped {
		builder.WriteByte(character)
	}

	l.offset++

	return true, nil
}

func invalidDecodedRune(runeValue rune, size int) bool {
	return runeValue == utf8.RuneError && size == 1
}

func (l *lexer) consumeUnicodeIdentifier(builder *strings.Builder, escaped bool) bool {
	runeValue, size := utf8.DecodeRuneInString(l.source[l.offset:])
	if !unicode.IsLetter(runeValue) && !unicode.IsDigit(runeValue) && runeValue != '_' {
		return false
	}

	if escaped {
		builder.WriteRune(runeValue)
	}

	l.offset += size

	return true
}

func identifierKind(text string) (tokenKind, string) {
	switch text {
	case "true":
		return tokenTrue, text
	case "false":
		return tokenFalse, text
	case nilKeyword:
		return tokenNil, text
	case "in", "IN":
		return tokenOperator, "in"
	default:
		return tokenIdentifier, text
	}
}

func (l *lexer) appendEscapedIdentifierRune(builder *strings.Builder, start, escapeSize int) *Diagnostic {
	l.offset += escapeSize
	if l.offset == len(l.source) {
		return l.diagnostic(start, l.offset, "identifier ends with an escape")
	}

	runeValue, size := utf8.DecodeRuneInString(l.source[l.offset:])
	if runeValue == utf8.RuneError && size == 1 {
		return l.diagnostic(l.offset, l.offset+1, "invalid UTF-8 byte")
	}

	builder.WriteRune(runeValue)

	l.offset += size

	return nil
}

func (l *lexer) number() *Diagnostic {
	start := l.offset
	previous := byte(0)

	for l.offset < len(l.source) {
		character := l.source[l.offset]
		if numberContinues(character, previous) {
			previous = character
			l.offset++

			continue
		}

		break
	}

	text := l.source[start:l.offset]

	kind := gotoken.INT

	if strings.HasSuffix(text, "i") {
		kind = gotoken.IMAG
	} else if strings.ContainsAny(text, ".eEpP") {
		kind = gotoken.FLOAT
	}

	if kind == gotoken.FLOAT {
		if literal, ok := makeSimpleDecimalLiteral(text); ok {
			l.emit(newLexToken(tokenNumber, text, literal, start, l.offset))

			return nil
		}
	}

	value := constant.MakeFromLiteral(text, kind, 0)

	if value.Kind() == constant.Unknown {
		return l.diagnostic(start, l.offset, fmt.Sprintf("invalid numeric literal %q", text))
	}

	l.emit(newLexToken(tokenNumber, text, makeNumberLiteral(value, text), start, l.offset))

	return nil
}

func simpleDecimalConstant(text string) (constant.Value, bool) {
	mantissa, fractionalDigits, ok := scanSimpleDecimal(text)
	if !ok {
		return nil, false
	}

	denominator := uint64(1)
	for range fractionalDigits {
		denominator *= decimalRadix
	}

	if mantissa%denominator == 0 {
		integer := mantissa / denominator
		if integer <= maxExactFloat64Integer {
			return constant.MakeFloat64(float64(integer)), true
		}
	}

	value := constant.BinaryOp(constant.MakeUint64(mantissa), gotoken.QUO, constant.MakeUint64(denominator))

	return value, true
}

func scanSimpleDecimal(text string) (uint64, int, bool) {
	var mantissa uint64

	digitCount, fractionalDigits := 0, 0
	dotSeen := false

	for position := range len(text) {
		character := text[position]
		if character == '.' && !dotSeen {
			dotSeen = true

			continue
		}

		if !isASCIIDigit(character) {
			return 0, 0, false
		}

		digitCount++

		digit := uint64(character - '0')
		if mantissa > (math.MaxUint64-digit)/decimalRadix {
			return 0, 0, false
		}

		mantissa = mantissa*decimalRadix + digit

		if dotSeen {
			fractionalDigits++
			if fractionalDigits > maxSimpleDecimalFractionalDigits {
				return 0, 0, false
			}
		}
	}

	if !dotSeen || digitCount == 0 {
		return 0, 0, false
	}

	return mantissa, fractionalDigits, true
}

func numberContinues(character, previous byte) bool {
	if isASCIIAlphaNumeric(character) || character == '_' || character == '.' {
		return true
	}

	sign := character == '+' || character == '-'
	exponent := previous == 'e' || previous == 'E' || previous == 'p' || previous == 'P'

	return sign && exponent
}

func (l *lexer) stringLiteral(quote rune, quoteSize int) *Diagnostic {
	start := l.offset
	l.offset += quoteSize
	contentStart := l.offset
	escaped := false

	for l.offset < len(l.source) {
		runeValue, size := utf8.DecodeRuneInString(l.source[l.offset:])
		if runeValue == utf8.RuneError && size == 1 {
			return l.diagnostic(l.offset, l.offset+1, "invalid UTF-8 byte")
		}

		if !escaped && runeValue == quote {
			raw := l.source[contentStart:l.offset]
			value, err := unquoteContent(raw, quote)

			l.offset += size

			if err != nil {
				return l.diagnostic(start, l.offset, fmt.Sprintf("invalid string literal: %v", err))
			}

			l.emit(newLexToken(tokenString, l.source[start:l.offset], value, start, l.offset))

			return nil
		}

		if !escaped && runeValue == '\\' {
			escaped = true
		} else {
			escaped = false
		}

		l.offset += size
	}

	return l.diagnostic(start, len(l.source), "unclosed string literal")
}

func unquoteContent(raw string, quote rune) (string, error) {
	if quote < 0 || quote > math.MaxUint8 {
		return "", newDetailError("quote rune %U is outside the byte range", quote)
	}

	quoteByte := byte(quote)

	var builder strings.Builder

	for raw != "" {
		value, _, tail, err := strconv.UnquoteChar(raw, quoteByte)
		if err != nil {
			return "", fmt.Errorf("unquote string content: %w", err)
		}

		builder.WriteRune(value)

		raw = tail
	}

	return builder.String(), nil
}

func unescapeIdentifier(raw string) string {
	var builder strings.Builder

	builder.Grow(len(raw))

	for cursor := 0; cursor < len(raw); cursor++ {
		if raw[cursor] == '\\' && cursor+1 < len(raw) {
			cursor++
		}

		builder.WriteByte(raw[cursor])
	}

	return builder.String()
}

func (l *lexer) diagnostic(start, end int, message string) *Diagnostic {
	diagnostic := newErrorDiagnostic(l.span(start, end), message)

	return &diagnostic
}

func (l *lexer) span(start, end int) Span {
	if l.lineOffsets == nil {
		l.lineOffsets = sourceLineOffsets(l.source)
	}

	return spanFromLineOffsets(l.lineOffsets, start, end)
}

func spanFromLineOffsets(lineOffsets []int, start, end int) Span {
	if len(lineOffsets) == 1 {
		return Span{Start: start, End: end, Line: 1, Column: start + 1}
	}

	lineIndex := max(sort.Search(len(lineOffsets), func(cursor int) bool { return lineOffsets[cursor] > start })-1, 0)

	return Span{Start: start, End: end, Line: lineIndex + 1, Column: start - lineOffsets[lineIndex] + 1}
}

func isASCIIDigit(character byte) bool { return character >= '0' && character <= '9' }

func isASCIIAlphaNumeric(character byte) bool {
	return isASCIIDigit(character) || character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

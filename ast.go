package fasteval

// NodeKind classifies a public AST node.
type NodeKind uint8

const (
	// NodeInvalid is an unavailable node.
	NodeInvalid NodeKind = iota
	// NodeLiteral is a literal value.
	NodeLiteral
	// NodeIdentifier is a variable or function name.
	NodeIdentifier
	// NodeUnary is a prefix operation.
	NodeUnary
	// NodeBinary is a binary operation.
	NodeBinary
	// NodeConditional is a ternary expression.
	NodeConditional
	// NodeCall is a function or method call.
	NodeCall
	// NodeAccess is field or map-dot access.
	NodeAccess
	// NodeIndex is an index operation.
	NodeIndex
	// NodeList is a list literal.
	NodeList
	// NodeMap is a map literal.
	NodeMap
)

// Node is an immutable node in a compiled expression AST.
type Node struct {
	// Keep wide fields before scalar metadata to avoid alignment padding.
	span     Span
	text     string
	value    any
	children []*Node
	depth    uint16
	kind     NodeKind
	optional bool
}

func newNode(kind NodeKind, span Span, text string, value any, children []*Node, optional bool) *Node {
	return &Node{
		span: span, text: text, value: value, children: children,
		depth: 1, kind: kind, optional: optional,
	}
}

// Kind returns n's node category.
func (n *Node) Kind() NodeKind {
	if n == nil {
		return NodeInvalid
	}

	return n.kind
}

// Span returns n's source span.
func (n *Node) Span() Span {
	if n == nil {
		return emptySpan()
	}

	return n.span
}

// Text returns the source text associated with n.
func (n *Node) Text() string {
	if n == nil {
		return ""
	}

	return n.text
}

// Children returns a copy of n's child list.
func (n *Node) Children() []*Node {
	if n == nil || len(n.children) == 0 {
		return nil
	}

	children := make([]*Node, len(n.children))
	copy(children, n.children)

	return children
}

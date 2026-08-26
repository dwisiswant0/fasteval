package fasteval_test

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"go.dw1.io/fasteval"
)

func TestEscapedVariableAndSingletonListAreDistinct(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "[response-time]", map[string]any{"response-time": 42})
	if err != nil || got != 42 {
		t.Fatalf("escaped variable = %#v, %v", got, err)
	}

	got, err = fasteval.Eval(context.Background(), "[response-time,]", map[string]any{"response": 1, "time": 2})
	if err != nil {
		t.Fatalf("singleton list error = %v", err)
	}

	if !reflect.DeepEqual(got, []any{-1}) {
		t.Fatalf("singleton list = %#v", got)
	}

	for source, name := range map[string]string{
		"['name']": "'name'", "[foo'bar]": "foo'bar",
		"['name,part']": "'name,part'", `[name\,part]`: "name,part", `[name\]part]`: "name]part",
	} {
		got, err = fasteval.Eval(context.Background(), source, map[string]any{name: 42})
		if err != nil || got != 42 {
			t.Fatalf("escaped variable %q = %#v, %v", source, got, err)
		}
	}
}

func TestLiteralShiftRejectsCountThatDoesNotFitUint(t *testing.T) {
	t.Parallel()

	if strconv.IntSize != 32 {
		t.Skip("requires a 32-bit target")
	}

	_, err := fasteval.Eval(context.Background(), "1 << 4294967296", nil)
	if err == nil {
		t.Fatal("Eval() error = nil, want shift-count range error")
	}
}

func TestIdentifierEscape(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), `response\-time`, map[string]any{"response-time": 42})
	if err != nil || got != 42 {
		t.Fatalf("escaped identifier = %#v, %v", got, err)
	}
}

func TestListStringCanContainClosingBracket(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "[']',]", nil)

	want := []any{"]"}

	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("Eval() = %#v, %v; want %#v, nil", got, err, want)
	}
}

func TestEscapedIdentifierQuoteCanContainClosingBracket(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "[']']", map[string]any{"']'": 42})
	if err != nil || got != 42 {
		t.Fatalf("Eval() = %#v, %v; want 42, nil", got, err)
	}
}

func TestNestedBracketDisambiguation(t *testing.T) {
	t.Parallel()

	variables := map[string]any{testFoo: 1}
	tests := []struct {
		source string
		want   any
	}{
		{source: "[[foo], 2]", want: []any{1, 2}},
		{source: "[[], 1]", want: []any{[]any{}, 1}},
		{source: "[[foo],]", want: []any{1}},
	}

	for _, testCase := range tests {
		got, err := fasteval.Eval(context.Background(), testCase.source, variables)
		if err != nil || !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestTernaryExpressionKeepsEscapedIdentifierAfterQuestionMark(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(
		context.Background(), "[true,][0] ? [foo] : 0", map[string]any{testFoo: 42},
	)
	if err != nil || got != 42 {
		t.Fatalf("Eval() = %#v, %v; want 42, nil", got, err)
	}
}

func TestUnicodeIdentifier(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(
		context.Background(), "café + \u6570\u5b572", map[string]any{"café": 20, "\u6570\u5b572": 22},
	)
	if err != nil || got != 42 {
		t.Fatalf("unicode identifier = %#v, %v", got, err)
	}
}

func TestLargeExpression(t *testing.T) {
	t.Parallel()

	source := strings.Repeat("true && ", 31) + "true"

	got, err := fasteval.Eval(context.Background(), source, nil)
	if err != nil || got != true {
		t.Fatalf("large expression = %#v, %v", got, err)
	}
}

func TestExactBasicArithmeticEnforcesConstantSizeLimit(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"(1 << 1048575) * (1 << 1048575)",
		"1 / (1 << 524289) / (1 << 524289)",
	} {
		_, err := fasteval.Compile(source)
		if err == nil || !strings.Contains(err.Error(), "size limit") {
			t.Fatalf("Compile(%q) error = %v, want size-limit error", source, err)
		}
	}
}

func TestMapLookupUsesExactKeyIdentity(t *testing.T) {
	t.Parallel()

	variables := map[string]any{testValues: map[any]string{int(1): testInt, int64(1): testInt64}}

	got, err := fasteval.Eval(context.Background(), "values[key]", map[string]any{
		testValues: variables[testValues], testKey: int64(1),
	})
	if err != nil || got != testInt64 {
		t.Fatalf("Eval() = %#v, %v", got, err)
	}

	_, err = fasteval.Eval(context.Background(), "values[key]", map[string]any{
		testValues: variables[testValues], testKey: int8(1),
	})

	var evalErr *fasteval.EvalError

	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrAccess {
		t.Fatalf("Eval() error = %v, want ErrAccess", err)
	}
}

func TestMapMembershipPreservesCrossTypeNumericEquality(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "needle in values", map[string]any{
		testNeedle: int8(1), testValues: map[any]string{int64(1): testOne},
	})
	if err != nil || got != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
	}

	got, err = fasteval.Eval(context.Background(), "needle in values", map[string]any{
		testNeedle: testMissing, testValues: map[string]int{"present": 1},
	})
	if err != nil || got != false {
		t.Fatalf("Eval() = %#v, %v; want false, nil", got, err)
	}
}

func TestMapMembershipTreatsLogicalNilKeysAsEqual(t *testing.T) {
	t.Parallel()

	var nilPointer *int

	tests := []struct {
		name   string
		needle any
		values map[any]string
	}{
		{name: "nil matches typed nil", needle: nil, values: map[any]string{nilPointer: testFound}},
		{name: "typed nil matches nil", needle: nilPointer, values: map[any]string{nil: testFound}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := fasteval.Eval(context.Background(), "needle in values", map[string]any{
				testNeedle: testCase.needle, testValues: testCase.values,
			})
			if err != nil || got != true {
				t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
			}
		})
	}
}

func TestMapDuplicateEvaluatedKeyIsRejected(t *testing.T) {
	t.Parallel()

	_, err := fasteval.Eval(context.Background(), "{first: 1, second: 2}", map[string]any{
		testFirst: testSame, "second": testSame,
	})
	if err == nil {
		t.Fatal("Eval() error = nil, want duplicate-key error")
	}
}

func TestMapDuplicateLogicalNilKeyIsRejected(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"{nil: 1, nil?.x: 2}",
		"len({nil: 1, nil?.x: 2})",
	} {
		_, err := fasteval.Eval(context.Background(), source, nil)
		if err == nil {
			t.Fatalf("Eval(%q) error = nil, want duplicate-key error", source)
		}
	}
}

func TestMapLiteralCanonicalizesTypedNilKeys(t *testing.T) {
	t.Parallel()

	var key *int

	_, err := fasteval.Eval(context.Background(), "len({nil: 1, key: 2})", map[string]any{testKey: key})
	if err == nil {
		t.Fatal("duplicate logical-nil map key error = nil")
	}

	got, err := fasteval.Eval(
		context.Background(), "get({key: 'found'}, nil, 'missing')", map[string]any{testKey: key},
	)
	if err != nil || got != testFound {
		t.Fatalf("composed typed-nil map lookup = %#v, %v; want found, nil", got, err)
	}
}

func TestOptionalAccessOnlySuppressesNilReceiver(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "value?.name", map[string]any{testValue: nil})
	if err != nil || got != nil {
		t.Fatalf("nil optional access = %#v, %v", got, err)
	}

	_, err = fasteval.Eval(context.Background(), "value?.missing", map[string]any{testValue: map[string]any{}})
	if err == nil {
		t.Fatal("missing optional key error = nil")
	}
}

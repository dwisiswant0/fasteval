package fasteval_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"go.dw1.io/fasteval"
)

const (
	testAbsValueSource     = "abs(value)"
	testAda                = "Ada"
	testAnswer             = "answer"
	testCount              = "count"
	testDouble             = "double"
	testDuration           = "duration"
	testFallback           = "fallback"
	testFasteval           = "fasteval"
	testFirst              = "first"
	testFoo                = "foo"
	testFound              = "found"
	testInt                = "int"
	testInt8               = "int8"
	testInt64              = "int64"
	testKey                = "key"
	testKeys               = "keys"
	testLen                = "len"
	testLeft               = "left"
	testMap                = "map"
	testMax                = "max"
	testMeanValuesSource   = "mean(values)"
	testMedianValuesSource = "median(values)"
	testMin                = "min"
	testMissing            = "missing"
	testName               = "name"
	testNeedle             = "needle"
	testNegateValueSource  = "-value"
	testOne                = "one"
	testPairs              = "pairs"
	testPattern            = "pattern"
	testPlusValueSource    = "+value"
	testRight              = "right"
	testRightShiftSource   = "left >> right"
	testSame               = "same"
	testScore              = "score"
	testSequence           = "sequence"
	testShadow             = "shadow"
	testSlice              = "slice"
	testStart              = "start"
	testString             = "string"
	testSortValuesSource   = "sort(values)"
	testText               = "text"
	testToJSONValueSource  = "toJSON(value)"
	testUint64             = "uint64"
	testUser               = "user"
	testValue              = "value"
	testValues             = "values"
	testZero               = "zero"
)

type account struct {
	Name string
	note string
}

type namedScalar string

func (value namedScalar) Greeting() string { return "hello " + string(value) }

type namedBoolean bool

type namedSequence []int

func (values namedSequence) Total() int {
	total := 0
	for _, value := range values {
		total += value
	}

	return total
}

type namedInteger int

func (value namedInteger) Double() int { return int(value) * 2 }

type namedMapKey int

func TestCallResultsPreserveNamedValuesForAccessAndIndex(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction(
		"named", func() namedInteger { return 21 },
	))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	tests := []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "named().Double()", vars: nil, want: 42},
		{
			source: "values[named()]", vars: map[string]any{
				testValues: map[namedInteger]string{21: testFound},
			}, want: testFound,
		},
		{source: "named()", vars: nil, want: int(21)},
		{source: "named() + 1", vars: nil, want: int(22)},
	}

	for _, testCase := range tests {
		expression, compileErr := compiler.Compile(testCase.source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", testCase.source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), testCase.vars)
		if evalErr != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, evalErr, testCase.want)
		}
	}
}

func TestSelectedCallResultsPreserveNamedValuesForAccessAndIndex(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction(
		"named", func() namedInteger { return 21 },
	))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	tests := []struct {
		source string
		vars   map[string]any
		want   any
	}{
		{source: "(true ? named() : named()).Double()", vars: nil, want: 42},
		{source: "(nil ?? named()).Double()", vars: nil, want: 42},
		{
			source: "values[true ? named() : named()]", vars: map[string]any{
				testValues: map[namedInteger]string{21: testFound},
			}, want: testFound,
		},
		{
			source: "values[nil ?? named()]", vars: map[string]any{
				testValues: map[namedInteger]string{21: testFound},
			}, want: testFound,
		},
	}

	for _, testCase := range tests {
		expression, compileErr := compiler.Compile(testCase.source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", testCase.source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), testCase.vars)
		if evalErr != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, evalErr, testCase.want)
		}
	}
}

type namedValueHolder struct {
	Value  namedInteger
	Key    namedMapKey
	Values map[namedMapKey]namedInteger
}

type accountDetails struct {
	Name string
}

type accountWithDetails struct {
	*accountDetails
}

func (a account) Greeting(prefix string) string { return prefix + a.Name }

func TestAccessFieldsMapsMethodsAndOptionalNil(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler()
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expr, err := compiler.Compile("user.Greeting('hi ') == 'hi Ada' && data.answer == 42 && absent?.Name == nil")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expr.Eval(context.Background(), map[string]any{
		testUser: account{Name: testAda, note: "private"},
		"data":   map[string]any{testAnswer: 42},
		"absent": (*account)(nil),
	})
	if err != nil || got != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
	}
}

func TestTypedMapLiteralNumericKeyLookup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		source string
		want   any
	}{
		{source: "{1: value}[1]", want: testFound},
		{source: "get({1: value}, 1, 'missing')", want: testFound},
		{source: "1 in {1: value}", want: true},
	}

	for _, testCase := range tests {
		got, err := fasteval.EvalAs[any](context.Background(), testCase.source, map[string]any{
			testValue: testFound,
		})
		if err != nil || got != testCase.want {
			t.Fatalf("EvalAs[any](%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}
}

func TestConcreteNumericQueryFindsInternalLiteralMapKey(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"{1: 'found'}[key]",
		"get({1: 'found'}, key, 'missing')",
	} {
		got, err := fasteval.Eval(context.Background(), source, map[string]any{testKey: int64(1)})
		if err != nil || got != testFound {
			t.Fatalf("Eval(%q) = %#v, %v; want found, nil", source, got, err)
		}
	}
}

func TestLiteralQueryFindsConcreteNumericEvaluatorMapKey(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"{key: 'found'}[1]",
		"get({key: 'found'}, 1, 'missing')",
	} {
		got, err := fasteval.Eval(context.Background(), source, map[string]any{testKey: int64(1)})
		if err != nil || got != testFound {
			t.Fatalf("Eval(%q) = %#v, %v; want found, nil", source, got, err)
		}
	}
}

func TestKeysPreserveNamedIdentityThroughIndexing(t *testing.T) {
	t.Parallel()

	key := namedMapKey(1)
	values := map[namedMapKey]string{key: testFound}

	got, err := fasteval.Eval(context.Background(), "values[keys(values)[0]]", map[string]any{testValues: values})
	if err != nil || got != testFound {
		t.Fatalf("Eval() = %#v, %v; want found, nil", got, err)
	}

	keys, err := fasteval.Eval(context.Background(), "keys(values)", map[string]any{testValues: values})
	if err != nil || !reflect.DeepEqual(keys, []any{int(1)}) {
		t.Fatalf("outward keys = %#v, %v; want []any{int(1)}, nil", keys, err)
	}
}

func TestMapIndexPreservesNamedNumericKey(t *testing.T) {
	t.Parallel()

	key := namedMapKey(1)

	got, err := fasteval.Eval(context.Background(), "values[key]", map[string]any{
		testKey: key, testValues: map[namedMapKey]string{key: testFound},
	})
	if err != nil || got != testFound {
		t.Fatalf("Eval() = %#v, %v; want found, nil", got, err)
	}
}

func TestInterfaceMapIndexFallsBackToNormalizedNamedNumericKey(t *testing.T) {
	t.Parallel()

	key := namedMapKey(1)

	got, err := fasteval.Eval(context.Background(), "{key: 'found'}[key]", map[string]any{testKey: key})
	if err != nil || got != testFound {
		t.Fatalf("self-key Eval() = %#v, %v; want found, nil", got, err)
	}

	got, err = fasteval.Eval(context.Background(), "values[key]", map[string]any{
		testKey: key, testValues: map[any]string{key: "exact", int(1): "normalized"},
	})
	if err != nil || got != "exact" {
		t.Fatalf("exact-priority Eval() = %#v, %v; want exact, nil", got, err)
	}
}

func TestInterfaceMapIndexFallsBackToNormalizedDefinedScalarKey(t *testing.T) {
	t.Parallel()

	for _, key := range []any{namedBoolean(true), namedScalar(testKey)} {
		got, err := fasteval.Eval(
			context.Background(), "{key: 'found'}[key]", map[string]any{testKey: key},
		)
		if err != nil || got != testFound {
			t.Fatalf("Eval() with %T key = %#v, %v; want found, nil", key, got, err)
		}
	}
}

func TestEvaluatorMapLookupUsesNormalizedDefinedScalarEquivalence(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		source string
		key    any
	}{
		{source: "{key: 'found'}['x']", key: namedScalar("x")},
		{source: "get({key: 'found'}, 'x', 'missing')", key: namedScalar("x")},
		{source: "{key: 'found'}.x", key: namedScalar("x")},
		{source: "{key: 'found'}[true]", key: namedBoolean(true)},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, map[string]any{testKey: testCase.key})
		if err != nil || got != testFound {
			t.Fatalf("Eval(%q) with %T key = %#v, %v; want found, nil", testCase.source, testCase.key, got, err)
		}
	}
}

func TestIndirectInterfacePreservesNamedValueMethods(t *testing.T) {
	t.Parallel()

	var value any = namedInteger(21)

	got, err := fasteval.Eval(context.Background(), "value.Double()", map[string]any{testValue: &value})
	if err != nil || got != 42 {
		t.Fatalf("Eval() = %#v, %v; want 42, nil", got, err)
	}
}

func TestCompositeLiteralsPreserveNamedValuesUntilConsumption(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(context.Background(), "[value,][0].Greeting()", map[string]any{
		testValue: namedScalar(testAda),
	})
	if err != nil || got != "hello Ada" {
		t.Fatalf("list method Eval() = %#v, %v; want hello Ada, nil", got, err)
	}

	got, err = fasteval.Eval(context.Background(), "[{key: 'found'},][0][key]", map[string]any{
		testKey: namedScalar(testKey),
	})
	if err != nil || got != testFound {
		t.Fatalf("map key Eval() = %#v, %v; want found, nil", got, err)
	}
}

func TestAccessMethodsOnNamedNonStructValues(t *testing.T) {
	t.Parallel()

	got, err := fasteval.Eval(
		context.Background(),
		"scalar.Greeting() == 'hello Ada' && sequence.Total() == 6 && integer.Double() == 8",
		map[string]any{
			"scalar": namedScalar(testAda), testSequence: namedSequence{1, 2, 3}, "integer": namedInteger(4),
		},
	)
	if err != nil || got != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
	}
}

func TestDefinedBoolAndStringValuesNormalizeAtConsumerBoundaries(t *testing.T) {
	t.Parallel()

	variables := map[string]any{
		"flag": namedBoolean(true), "other": namedBoolean(true), testText: namedScalar(testAda),
	}
	for _, testCase := range []struct {
		source string
		want   any
	}{
		{source: "flag ? 'yes' : 'no'", want: "yes"},
		{source: "flag && other", want: true},
		{source: "text + '!'", want: "Ada!"},
		{source: "text =~ '^A'", want: true},
		{source: "upper(text)", want: "ADA"},
	} {
		got, err := fasteval.Eval(context.Background(), testCase.source, variables)
		if err != nil || got != testCase.want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", testCase.source, got, err, testCase.want)
		}
	}

	template, err := fasteval.CompileTemplate("{{flag ? text : 'missing'}}")
	if err != nil {
		t.Fatalf("CompileTemplate() error = %v", err)
	}

	rendered, err := template.Render(context.Background(), variables)
	if err != nil || rendered != testAda {
		t.Fatalf("Render() = %q, %v; want %q, nil", rendered, err, testAda)
	}
}

func TestDefinedStringValuesKeepRawMethodAndMapKeyIdentity(t *testing.T) {
	t.Parallel()

	key := namedScalar(testAda)

	got, err := fasteval.Eval(
		context.Background(),
		"key.Greeting() == 'hello Ada' && values[key] == 'found'",
		map[string]any{testKey: key, testValues: map[namedScalar]string{key: testFound}},
	)
	if err != nil || got != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
	}
}

func TestDefinedBoolAndStringEqualityIsConsistentInsideCollections(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		left  any
		right any
	}{
		{left: []namedScalar{"x"}, right: []string{"x"}},
		{left: []namedBoolean{true}, right: []bool{true}},
		{left: map[string]namedScalar{testKey: "x"}, right: map[string]string{testKey: "x"}},
		{left: map[any]int{namedScalar("x"): 1}, right: map[any]int{"x": 1}},
	} {
		got, err := fasteval.Eval(context.Background(), "left == right", map[string]any{
			testLeft: testCase.left, testRight: testCase.right,
		})
		if err != nil || got != true {
			t.Fatalf("collection equality for %#v and %#v = %#v, %v; want true, nil", testCase.left, testCase.right, got, err)
		}
	}

	for _, needle := range []any{"x", namedScalar("x"), true, namedBoolean(true)} {
		got, err := fasteval.Eval(context.Background(), "needle in values", map[string]any{
			testNeedle: needle,
			testValues: map[any]int{namedScalar("x"): 1, namedBoolean(true): 2},
		})
		if err != nil || got != true {
			t.Fatalf("membership for %#v = %#v, %v; want true, nil", needle, got, err)
		}
	}
}

func TestEvaluatorMapsRejectNormalizedDefinedScalarDuplicates(t *testing.T) {
	t.Parallel()

	for _, keys := range [][2]any{
		{namedScalar("x"), "x"},
		{namedBoolean(true), true},
	} {
		variables := map[string]any{
			testLeft: keys[0], testRight: keys[1],
			testPairs: []any{[]any{keys[0], 1}, []any{keys[1], 2}},
		}
		for _, source := range []string{
			"len({left: 1, right: 2})",
			"len(fromPairs(pairs))",
		} {
			_, err := fasteval.Eval(context.Background(), source, variables)
			if err == nil {
				t.Fatalf("Eval(%q) with keys %#v error = nil, want duplicate-key error", source, keys)
			}
		}
	}

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("groupKey", func(value int) any {
		if value == 1 {
			return namedScalar("x")
		}

		return "x"
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("len(groupBy([1, 2], groupKey))")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), nil)
	if err != nil || got != 1 {
		t.Fatalf("groupBy normalized-key Eval() = %#v, %v; want 1, nil", got, err)
	}
}

func TestGroupByCanonicalizesTypedNilKeys(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(fasteval.WithFunction("nilKey", func(value int) any {
		if value == 1 {
			var key *int

			return key
		}

		return nil
	}))
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	expression, err := compiler.Compile("len(groupBy([1, 2], nilKey))")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), nil)
	if err != nil || got != 1 {
		t.Fatalf("Eval() = %#v, %v; want 1, nil", got, err)
	}
}

func TestCollectionExtractorsPreserveRawDefinedScalars(t *testing.T) {
	t.Parallel()

	key := namedScalar(testAda)

	variables := map[string]any{
		testSequence: []namedInteger{21},
		"mapping":    map[string]namedInteger{testValue: 21},
		"keyed":      map[namedScalar]string{key: testFound},
	}
	for _, source := range []string{
		"first(sequence).Double() == 42",
		"last(sequence).Double() == 42",
		"values(mapping)[0].Double() == 42",
		"toPairs(mapping)[0][1].Double() == 42",
		"keyed[toPairs(keyed)[0][0]] == 'found'",
	} {
		got, err := fasteval.Eval(context.Background(), source, variables)
		if err != nil || got != true {
			t.Fatalf("Eval(%q) = %#v, %v; want true, nil", source, got, err)
		}
	}

	for source, want := range map[string]any{
		"first(sequence)":  int(21),
		"values(mapping)":  []any{int(21)},
		"toPairs(mapping)": []any{[]any{testValue, int(21)}},
	} {
		got, err := fasteval.Eval(context.Background(), source, variables)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", source, got, err, want)
		}
	}
}

func TestGetPreservesRawSelectedValuesAndKeys(t *testing.T) {
	t.Parallel()

	key := namedScalar(testAda)

	variables := map[string]any{
		testSequence: []namedInteger{21},
		testKey:      key,
		testValues:   map[namedScalar]namedInteger{key: 22},
		testFallback: namedInteger(23),
	}
	for source, want := range map[string]any{
		"get(sequence, 0).Double()":                 42,
		"get(values, key, fallback).Double()":       44,
		"get(values, 'missing', fallback).Double()": 46,
	} {
		got, err := fasteval.Eval(context.Background(), source, variables)
		if err != nil || got != want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", source, got, err, want)
		}
	}

	got, err := fasteval.Eval(context.Background(), "get(sequence, 0)", variables)
	if err != nil || got != int(21) {
		t.Fatalf("direct get Eval() = %#v, %v; want 21, nil", got, err)
	}
}

func TestHigherOrderSelectionPreservesRawDefinedScalars(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("keepInt", func(value int) bool { return value >= 21 }),
		fasteval.WithFunction("keepString", func(value string) bool { return value == testAda }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	key := namedScalar(testAda)

	variables := map[string]any{
		testSequence: []namedInteger{21, 22},
		testKeys:     []namedScalar{key},
		testValues:   map[namedScalar]string{key: testFound},
	}

	for source, want := range map[string]any{
		"filter(sequence, keepInt)[0].Double()": 42,
		"find(sequence, keepInt).Double()":      42,
		"findLast(sequence, keepInt).Double()":  44,
		"values[find(keys, keepString)]":        testFound,
		"values[filter(keys, keepString)[0]]":   testFound,
	} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), variables)
		if evalErr != nil || got != want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", source, got, evalErr, want)
		}
	}

	for source, want := range map[string]any{
		"filter(sequence, keepInt)": []any{int(21), int(22)},
		"find(sequence, keepInt)":   int(21),
	} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), variables)
		if evalErr != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", source, got, evalErr, want)
		}
	}
}

func TestSequenceTransformsPreserveRawDefinedScalars(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("identity", func(value int) int { return value }),
		fasteval.WithFunction("identityString", func(value string) string { return value }),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	key := namedScalar(testAda)

	variables := map[string]any{
		testSequence: []namedInteger{21, 22},
		"keys":       []namedScalar{key},
		testValues:   map[namedScalar]string{key: testFound},
	}

	for source, want := range map[string]any{
		"concat(sequence)[0].Double()":                    42,
		"flatten([sequence,])[0].Double()":                42,
		"uniq(sequence)[0].Double()":                      42,
		"take(sequence, 1)[0].Double()":                   42,
		"reverse(sequence)[0].Double()":                   44,
		"sort(reverse(sequence))[0].Double()":             42,
		"sortBy(reverse(sequence), identity)[0].Double()": 42,
		"values[take(keys, 1)[0]]":                        testFound,
		"values[sortBy(keys, identityString)[0]]":         testFound,
	} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), variables)
		if evalErr != nil || got != want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", source, got, evalErr, want)
		}
	}

	expression, err := compiler.Compile("concat(sequence)")
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}

	got, err := expression.Eval(context.Background(), variables)
	if err != nil || !reflect.DeepEqual(got, []any{int(21), int(22)}) {
		t.Fatalf("direct transform Eval() = %#v, %v; want normalized integers, nil", got, err)
	}
}

func TestValueProducingBuiltinsPreserveRawDefinedScalars(t *testing.T) {
	t.Parallel()

	compiler, err := fasteval.NewCompiler(
		fasteval.WithFunction("toNamed", func(value int) namedInteger { return namedInteger(value) }),
		fasteval.WithFunction("reduceNamed", func(left, right int) namedInteger {
			return namedInteger(left + right)
		}),
	)
	if err != nil {
		t.Fatalf("NewCompiler() error = %v", err)
	}

	key := namedInteger(21)

	variables := map[string]any{
		testFirst:  []namedInteger{20},
		"second":   []namedInteger{21},
		"low":      namedInteger(20),
		"high":     key,
		testValues: map[namedInteger]string{key: testFound},
		testPairs:  []any{[]any{key, key}},
		testKey:    key,
		"seed":     key,
	}

	for source, want := range map[string]any{
		"concat(first, second)[1].Double()":         42,
		"map([21,], toNamed)[0].Double()":           42,
		"reduce([10, 11], reduceNamed).Double()":    42,
		"min(low, high).Double()":                   40,
		"max(low, high).Double()":                   42,
		"values[map([21,], toNamed)[0]]":            testFound,
		"values[max(low, high)]":                    testFound,
		"fromPairs(pairs)[key].Double()":            42,
		"values[reduce([], reduceNamed, seed)]":     testFound,
		"reduce([], reduceNamed, seed).Double()":    42,
		"groupBy([21,], toNamed)[key][0]":           21,
		"keys(groupBy([21,], toNamed))[0].Double()": 42,
	} {
		expression, compileErr := compiler.Compile(source)
		if compileErr != nil {
			t.Fatalf("Compile(%q) error = %v", source, compileErr)
		}

		got, evalErr := expression.Eval(context.Background(), variables)
		if evalErr != nil || got != want {
			t.Fatalf("Eval(%q) = %#v, %v; want %#v, nil", source, got, evalErr, want)
		}
	}
}

func TestAccessChainsPreserveNamedValuesForMethodsAndMapKeys(t *testing.T) {
	t.Parallel()

	key := namedMapKey(1)
	holder := namedValueHolder{
		Value: namedInteger(4), Key: key, Values: map[namedMapKey]namedInteger{key: 5},
	}

	got, err := fasteval.Eval(
		context.Background(),
		"holder.Value.Double() == 8 && holder.Values[holder.Key].Double() == 10 && "+
			"type(holder.Value) == 'int' && holder.Value + 1 == 5",
		map[string]any{"holder": holder},
	)
	if err != nil || got != true {
		t.Fatalf("Eval() = %#v, %v; want true, nil", got, err)
	}
}

func TestUnexportedFieldIsRejected(t *testing.T) {
	t.Parallel()

	_, err := fasteval.Eval(context.Background(), "user.note", map[string]any{
		testUser: account{Name: "", note: "secret"},
	})
	if err == nil {
		t.Fatal("Eval() error = nil, want unexported-field error")
	}
}

func TestNilEmbeddedFieldReturnsAccessError(t *testing.T) {
	t.Parallel()

	_, err := fasteval.Eval(context.Background(), "user.Name", map[string]any{
		testUser: accountWithDetails{accountDetails: nil},
	})

	var evalErr *fasteval.EvalError
	if !errors.As(err, &evalErr) || evalErr.Code() != fasteval.ErrAccess {
		t.Fatalf("Eval() error = %v, want ErrAccess", err)
	}
}

func TestPointerInterfaceCyclesReturnErrors(t *testing.T) {
	t.Parallel()

	var cycle any

	cycle = &cycle

	for _, source := range []string{"value.Name", "value[0]", "get(value, 0)", "first(value)"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			_, err := fasteval.Eval(context.Background(), source, map[string]any{testValue: cycle})
			if err == nil {
				t.Fatalf("Eval(%q) error = nil, want cycle error", source)
			}
		})
	}
}

func TestOptionalAccessSuppressesPointerToNilMap(t *testing.T) {
	t.Parallel()

	var values map[string]any

	got, err := fasteval.Eval(context.Background(), "value?.missing", map[string]any{
		testValue: &values,
	})
	if err != nil || got != nil {
		t.Fatalf("Eval() = %#v, %v; want nil, nil", got, err)
	}
}

package fasteval

import (
	"context"
	"math"
	"testing"
	"time"
)

func TestEqualityHashKeysMatchScalarEquality(t *testing.T) {
	t.Parallel()

	type namedString string

	tests := []struct {
		name  string
		left  any
		right any
	}{
		{name: "cross-type integer", left: int8(1), right: uint64(1)},
		{name: "integer and float", left: int64(1), right: float64(1)},
		{name: "integer and complex", left: uint8(1), right: complex128(1)},
		{name: "signed zero", left: math.Copysign(0, -1), right: float64(0)},
		{name: "positive infinity", left: math.Inf(1), right: math.Inf(1)},
		{name: "duration and integer", left: time.Duration(1), right: int64(1)},
		{name: "named and plain string", left: namedString("x"), right: "x"},
		{name: "same string", left: "x", right: "x"},
		{name: nilKeyword, left: nil, right: nil},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			leftKey, leftState := makeEqualityHashKey(testCase.left)

			rightKey, rightState := makeEqualityHashKey(testCase.right)

			if leftState != equalityHashAvailable || rightState != equalityHashAvailable {
				t.Fatalf("hash states = %v, %v; want available", leftState, rightState)
			}

			want, err := equalValuesContext(context.Background(), testCase.left, testCase.right)
			if err != nil {
				t.Fatalf("equalValuesContext() error = %v", err)
			}

			if got := leftKey == rightKey; got != want {
				t.Fatalf("hash equality = %v, scalar equality = %v", got, want)
			}
		})
	}

	if _, state := makeEqualityHashKey(math.NaN()); state != equalityHashAlwaysDistinct {
		t.Fatalf("NaN hash state = %v, want always distinct", state)
	}

	if _, state := makeEqualityHashKey([]int{1}); state != equalityHashUnavailable {
		t.Fatalf("slice hash state = %v, want unavailable", state)
	}
}

func TestEqualityComponentCodesDoNotOverlapBoundaryValues(t *testing.T) {
	t.Parallel()

	values := []any{
		math.SmallestNonzeroFloat64,
		math.MaxFloat64,
		math.Inf(-1),
		math.Inf(1),
		int64(math.MinInt64),
		uint64(math.MaxUint64),
	}

	keys := make(map[equalityHashKey]any, len(values))

	for _, value := range values {
		key, state := makeEqualityHashKey(value)
		if state != equalityHashAvailable {
			t.Fatalf("makeEqualityHashKey(%v) state = %v, want available", value, state)
		}

		if previous, duplicate := keys[key]; duplicate {
			t.Fatalf("boundary values %v and %v have the same key", previous, value)
		}

		keys[key] = value
	}
}

func TestScalarMapEqualityPreservesLanguageSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  any
		right any
		want  bool
	}{
		{name: "equal integers", left: map[int]int{1: 2}, right: map[int]int{1: 2}, want: true},
		{name: "cross-type integers", left: map[int]int{1: 2}, right: map[int]float64{1: 2}, want: true},
		{name: "different integers", left: map[int]int{1: 2}, right: map[int]int{1: 3}, want: false},
		{name: "equal strings", left: map[string]string{"a": "b"}, right: map[string]string{"a": "b"}, want: true},
		{
			name: "signed zero values", left: map[int]float64{1: math.Copysign(0, -1)},
			right: map[int]float64{1: 0}, want: true,
		},
		{name: "equal complex", left: map[int]complex128{1: 2 + 3i}, right: map[int]complex128{1: 2 + 3i}, want: true},
		{name: "NaN values", left: map[int]float64{1: math.NaN()}, right: map[int]float64{1: math.NaN()}, want: false},
		{name: "NaN keys", left: map[float64]int{math.NaN(): 1}, right: map[float64]int{math.NaN(): 1}, want: false},
	}
	for _, testCase := range tests {
		got, err := equalValuesContext(context.Background(), testCase.left, testCase.right)
		if err != nil || got != testCase.want {
			t.Fatalf("%s: equalValuesContext() = %v, %v; want %v, nil", testCase.name, got, err, testCase.want)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := equalValuesContext(ctx, map[int]int{1: 2}, map[int]int{1: 2})
	if err == nil {
		t.Fatal("equalValuesContext() error = nil for canceled context")
	}
}

func TestMapEqualityUsesSemanticKeyMatching(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  any
		right any
		want  bool
	}{
		{name: "cross-type maps", left: map[int]string{1: "x"}, right: map[int64]string{1: "x"}, want: true},
		{
			name: "cross-type maps with composite values", left: map[int][]int{1: {2, 3}},
			right: map[int64][]int{1: {2, 3}}, want: true,
		},
		{
			name: "interface keys", left: map[any]string{int(1): "x"},
			right: map[any]string{int64(1): "x"}, want: true,
		},
		{
			name: "one-to-one interface keys", left: map[any]string{int(1): "a", int64(1): "b"},
			right: map[any]string{int8(1): "b", uint(1): "a"}, want: true,
		},
		{
			name: "different values", left: map[any]string{int(1): "x"},
			right: map[any]string{int64(1): "y"}, want: false,
		},
	}

	for _, testCase := range tests {
		for _, values := range [][2]any{{testCase.left, testCase.right}, {testCase.right, testCase.left}} {
			equal, err := equalValuesContext(context.Background(), values[0], values[1])
			if err != nil || equal != testCase.want {
				t.Fatalf(
					"%s: equalValuesContext(%T, %T) = %v, %v; want %v, nil",
					testCase.name, values[0], values[1], equal, err, testCase.want,
				)
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := equalValuesContext(ctx, map[int]string{1: "x"}, map[int64]string{1: "x"})
	if err == nil {
		t.Fatal("equalValuesContext() error = nil for canceled semantic map comparison")
	}
}

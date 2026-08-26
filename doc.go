// Package fasteval compiles expressions and renders raw string templates.
//
// Its expression language covers arithmetic, comparisons, logic, collections,
// access, and function calls while preserving native Go numeric types. Compiled
// expressions and templates are immutable and safe for concurrent use.
//
// Templates return raw text. Callers must encode values for HTML, SQL, shell
// commands, or any other destination before using rendered output there.
package fasteval

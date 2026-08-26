package fasteval

func makeBuiltins() map[string]*callable {
	functions := make(map[string]*callable)
	registerCollectionBuiltins(functions)
	registerStringBuiltins(functions)
	registerNumericBuiltins(functions)
	registerTimeBuiltins(functions)
	registerEncodingBuiltins(functions)

	return functions
}

func registerBuiltin(functions map[string]*callable, name string, function builtinFunc) {
	functions[name] = newBuiltin(name, function, callableArguments)
}

func registerHigherOrderBuiltin(functions map[string]*callable, name string, function builtinFunc) {
	functions[name] = newBuiltin(name, function, secondArgumentFunctionReference)
}

package fasteval

func registerCollectionBuiltins(functions map[string]*callable) {
	registerHigherOrderBuiltin(functions, "all", quantifyBuiltin(quantifyAll))
	registerHigherOrderBuiltin(functions, "any", quantifyBuiltin(quantifyAny))
	registerHigherOrderBuiltin(functions, "one", quantifyBuiltin(quantifyOne))
	registerHigherOrderBuiltin(functions, "none", quantifyBuiltin(quantifyNone))
	registerHigherOrderBuiltin(functions, "map", mapBuiltin)
	functions["filter"] = newBuiltin("filter", filterBuiltin, secondArgumentFunctionReference|firstArgumentRaw)
	functions["find"] = newBuiltin("find", findBuiltin(findFirstValue), secondArgumentFunctionReference|firstArgumentRaw)
	functions["findIndex"] = newBuiltin(
		"findIndex", findBuiltin(findFirstIndex), secondArgumentFunctionReference|firstArgumentRaw,
	)
	functions["findLast"] = newBuiltin(
		"findLast", findBuiltin(findLastValue), secondArgumentFunctionReference|firstArgumentRaw,
	)
	functions["findLastIndex"] = newBuiltin(
		"findLastIndex", findBuiltin(findLastIndex), secondArgumentFunctionReference|firstArgumentRaw,
	)
	functions["groupBy"] = newBuiltin("groupBy", groupByBuiltin, secondArgumentFunctionReference|firstArgumentRaw)
	registerHigherOrderBuiltin(functions, "count", countBuiltin)
	functions["concat"] = newBuiltin("concat", concatBuiltin, allArgumentsRaw)
	functions["flatten"] = newBuiltin("flatten", flattenBuiltin, firstArgumentRaw)
	functions["uniq"] = newBuiltin("uniq", uniqBuiltin, firstArgumentRaw)
	registerBuiltin(functions, "join", joinBuiltin)
	functions["reduce"] = newBuiltin(
		"reduce", reduceBuiltin, secondArgumentFunctionReference|allArgumentsRaw,
	)
	registerBuiltin(functions, "sum", sumBuiltin)
	registerBuiltin(functions, "mean", meanBuiltin)
	registerBuiltin(functions, "median", medianBuiltin)
	registerBuiltin(functions, "first", edgeBuiltin(false))
	registerBuiltin(functions, "last", edgeBuiltin(true))
	functions["take"] = newBuiltin("take", takeBuiltin, firstArgumentRaw)
	functions["reverse"] = newBuiltin("reverse", reverseBuiltin, firstArgumentRaw)
	functions["sort"] = newBuiltin("sort", sortBuiltin, firstArgumentRaw)
	functions["sortBy"] = newBuiltin("sortBy", sortByBuiltin, secondArgumentFunctionReference|firstArgumentRaw)
	registerBuiltin(functions, "keys", keysBuiltin)
	registerBuiltin(functions, "values", valuesBuiltin)
	registerBuiltin(functions, "toPairs", toPairsBuiltin)
	registerBuiltin(functions, "fromPairs", fromPairsBuiltin)
	registerBuiltin(functions, "len", lengthBuiltin)
	functions["get"] = newBuiltin("get", getBuiltin, allArgumentsRaw)
}

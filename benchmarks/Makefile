SHELL := /bin/bash

GO ?= go
BENCHSTAT ?= $(GO) tool benchstat
VIZB ?= $(GO) tool vizb
OUT := out
HEADER := $(OUT)/header
RESULTS := $(OUT)/results
RESULTS_HTML := $(RESULTS).html
CPU_OUT ?= 
MEM_OUT ?= 
COUNT ?= 10
BENCHTIME ?= 2s
ENGINES := fasteval govaluate expr cel gval
ENGINE_RESULTS := $(addprefix $(OUT)/,$(ENGINES))
PROFILE_FLAGS := $(if $(strip $(CPU_OUT)),-cpuprofile $(CPU_OUT)) $(if $(strip $(MEM_OUT)),-memprofile $(MEM_OUT))

.PHONY: all bench benchstat vizb clean
.DELETE_ON_ERROR:

all:
	@$(MAKE) bench
	@$(MAKE) benchstat

bench:
	@mkdir -p $(OUT)
	@rm -f $(HEADER) $(RESULTS) $(ENGINE_RESULTS)
	@set -o pipefail; \
		$(GO) test -run '^$$' -bench=. -benchmem -count=$(COUNT) -benchtime=$(BENCHTIME) $(PROFILE_FLAGS) | tee $(RESULTS)
	@grep -E '^(goos|goarch|pkg|cpu):' $(RESULTS) > $(HEADER)
	@set -eu -o pipefail; \
		for engine in $(ENGINES); do \
			{ \
				cat $(HEADER); \
				grep -E "^Benchmark[^[:space:]]*/$${engine}-[0-9]+[[:space:]]" $(RESULTS) | \
					sed "s|/$${engine}-|-|"; \
				echo PASS; \
			} > "$(OUT)/$${engine}"; \
		done

benchstat:
	@set -eu; \
		for engine in $(ENGINES); do \
			test -s "$(OUT)/$${engine}"; \
		done
	@cd $(OUT) && $(BENCHSTAT) $(ENGINES)

vizb:
	@set -eu; \
		test -s "$(RESULTS)";
	@$(VIZB) bar $(RESULTS) \
		-P go \
		-p 'n{Phase}/x{Expression}/y{Evaluator}' \
		-n 'Expression Evaluator Benchmarks' \
		-o $(RESULTS_HTML)

clean:
	@rm -f $(HEADER) $(RESULTS)* $(ENGINE_RESULTS)
	@rmdir $(OUT) 2>/dev/null || true

PROJECT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

SRC := $(PROJECT)src
TESTS := $(PROJECT)tests
ALL := $(SRC) $(TESTS)

export PYTHONPATH = $(PROJECT):$(PROJECT)/lib:$(SRC)

update-dependencies:
	uv lock -U --no-cache

generate-requirements:
	uv pip compile -q --no-cache pyproject.toml -o requirements.txt
	uv pip compile -q --no-cache --all-extras pyproject.toml -o requirements-dev.txt

lint:
	uv tool run ruff check $(ALL)
	uv tool run ruff format --check --diff $(ALL)
	shellcheck $(PROJECT)app/bin/*
	shfmt -d $(PROJECT)app/bin/*

format:
	uv tool run ruff check --fix $(ALL)
	uv tool run ruff format $(ALL)
	shfmt -l -w $(PROJECT)app/bin/*

unit:
	uv run --all-extras \
		coverage run \
		--source=$(SRC) \
		-m pytest \
		--ignore=$(TESTS)/integration \
		--ignore=$(TESTS)/functional \
		--tb native \
		-v \
		-s \
		$(ARGS)
	uv run --all-extras coverage report

integration:
	uv run --all-extras \
		pytest \
		-v \
		-x \
		-s \
		--tb native \
		--ignore=$(TESTS)/unit \
		--ignore=$(TESTS)/functional \
		--log-cli-level=INFO \
		$(ARGS)

functional:
	uv run --all-extras \
		pytest \
		-v \
		-x \
		-s \
		--tb native \
		--ignore=$(TESTS)/unit \
		--ignore=$(TESTS)/integration \
		--log-cli-level=INFO \
		$(ARGS)

clean:
	rm -rf .coverage
	rm -rf .pytest_cache
	rm -rf .ruff_cache
	rm -rf .venv
	rm -rf *.charm
	rm -rf *.rock
	rm -rf **/__pycache__
	rm -rf **/*.egg-info
	rm -rf requirements*.txt

PROJECT := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

SRC := $(PROJECT)src
TESTS := $(PROJECT)tests
ALL := $(SRC) $(TESTS)

export PYTHONPATH = $(PROJECT):$(PROJECT)/lib:$(SRC)

update-dependencies:
	uv lock -U --no-cache

lint:
	go vet ./...
	uv tool run ruff check $(ALL)
	uv tool run ruff format --check --diff $(ALL)
	uv run --all-extras ty check $(SRC) $(TESTS)

format:
	go fmt ./...
	uv tool run ruff check --fix $(ALL)
	uv tool run ruff format $(ALL)

unit:
	go test -v ./...
	uv run --all-extras \
		coverage run \
		--source=$(SRC) \
		-m pytest \
		--ignore=$(TESTS)/integration \
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
		--log-cli-level=INFO \
		$(ARGS)

clean:
	rm -rf bin
	rm -rf build
	rm -rf .coverage
	rm -rf .pytest_cache
	rm -rf .ruff_cache
	rm -rf .venv
	rm -rf *.charm
	rm -rf *.rock
	rm -rf **/__pycache__
	rm -rf **/*.egg-info
	rm -rf requirements*.txt

.PHONY: evn dev gen-client build version test-unit

VERSION_NUMBER=$(if $(VERSION),$(VERSION),$(shell ../scripts/vertagen/vertagen.sh -f pypi))

env:
	pip install -r requirements.txt

dev: env
	pip install -r requirements.dev.txt

gen-client:
	rm -rf turing/generated
	@openapi-generator generate \
		--input-spec ../api/openapi-sdk.yaml \
		--generator-name python \
		--output . \
		--config openapi-codegen.yaml

version:
	@echo VERSION = \"$(VERSION_NUMBER)\" > turing/version.py
	@echo $(VERSION_NUMBER)

build: version
	@rm -rf build dist
	@python setup.py sdist bdist_wheel

test-unit:
	@python -m pytest --cov=turing
REGISTRY_REPO=fl64

CONTAINER_NAME=hl-backend
CONTAINER_VER:=$(shell git describe --tags)
CONTAINER_VER := $(if $(CONTAINER_VER),$(CONTAINER_VER),$(shell git rev-parse --short HEAD))

CONTAINER_NAME_TAG=$(REGISTRY_REPO)/$(CONTAINER_NAME):$(CONTAINER_VER)
CONTAINER_NAME_LATEST=$(REGISTRY_REPO)/$(CONTAINER_NAME):latest

.PHONY: build latest push push_latest

NS:=default

build:
	docker build -t $(CONTAINER_NAME_TAG) .

latest: build
	docker tag $(CONTAINER_NAME_TAG) $(CONTAINER_NAME_LATEST)

push: build
	docker push $(CONTAINER_NAME_TAG)

push_latest: push latest
	docker push $(CONTAINER_NAME_LATEST)

kind_start:
	kind create cluster -n hl

kind_stop:
	kind delete cluster -n hl

deploy:
	kubectl apply -k k8s
	kubectl wait pod -n ${NS} --for=condition=ready --timeout=30s -l app=backend

undeploy:
	kubectl delete -k k8s/

curl:
	 kubectl -n ${NS} exec -it deployments/backend -- curl localhost:8000

annotate:
	kubectl -n ${NS} annotate pod -l app=backend disaster=""

remove_annotation:
	kubectl -n ${NS} annotate pod -l app=backend disaster-
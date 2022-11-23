REGISTRY_REPO=fl64

IMAGE_VER:=$(shell git describe --tags)
IMAGE_VER := $(if $(CONTAINER_VER),$(CONTAINER_VER),$(shell git rev-parse --short HEAD))

IMAGE_NAME=hl2022-app
IMAGE_NAME_TAG=$(REGISTRY_REPO)/$(IMAGE_NAME):$(IMAGE_VER)
IMAGE_NAME_LATEST=$(REGISTRY_REPO)/$(IMAGE_NAME):latest

.PHONY: build latest push push_latest kind_create kind_delete check deploy undeploy annotation_add annotation_remove

NS:=hl2022

build:
	docker build -t $(IMAGE_NAME_TAG) app/

latest: build
	docker tag $(IMAGE_NAME_TAG) $(IMAGE_NAME_LATEST)


push: build
	docker push $(IMAGE_NAME_TAG)

push_latest: push latest
	docker push $(IMAGE_NAME_LATEST)

kind_create:
	kind create cluster -n hl2022

kind_delete:
	kind delete cluster -n hl2022

deploy:
	kubectl apply -k k8s

check:
	kubectl wait pod -n ${NS} --for=condition=ready --timeout=30s -l app=backend
	kubectl wait pod -n ${NS} --for=condition=ready --timeout=30s -l app=frontend

undeploy:
	kubectl delete -k k8s/

annotation_add:
	kubectl -n ${NS} annotate pod -l app=backend disaster=""

annotation_remove:
	kubectl -n ${NS} annotate pod -l app=backend disaster-
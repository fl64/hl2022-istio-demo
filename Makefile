REGISTRY_REPO=fl64

CONTAINER_VER:=$(shell git describe --tags)
CONTAINER_VER := $(if $(CONTAINER_VER),$(CONTAINER_VER),$(shell git rev-parse --short HEAD))

BACKEND_NAME=hl2022-backend
BACKEND_NAME_TAG=$(REGISTRY_REPO)/$(BACKEND_NAME):$(CONTAINER_VER)
BACKEND_NAME_LATEST=$(REGISTRY_REPO)/$(BACKEND_NAME):latest

FRONTEND_NAME=hl2022-frontend
FRONTEND_NAME_TAG=$(REGISTRY_REPO)/$(FRONTEND_NAME):$(CONTAINER_VER)
FRONTEND_NAME_LATEST=$(REGISTRY_REPO)/$(FRONTEND_NAME):latest

#.PHONY: build latest push push_latest

NS:=hl2022

build_backend:
	docker build -t $(BACKEND_NAME_TAG) backend/

latest_backend: build_backend
	docker tag $(BACKEND_NAME_TAG) $(BACKEND_NAME_LATEST)

build_frontend:
	docker build -t $(FRONTEND_NAME_TAG) frontend/

latest_frontend: build_frontend
	docker tag $(FRONTEND_NAME_TAG) $(FRONTEND_NAME_LATEST)

build: build_backend build_frontend

latest: latest_backend latest_frontend

push_backend:
	docker push $(BACKEND_NAME_TAG)

push_backend_latest: push_backend latest_backend
	docker push $(BACKEND_NAME_LATEST)

push_frontend:
	docker push $(FRONTEND_NAME_TAG)

push_frontend_latest: push_frontend latest_frontend
	docker push $(FRONTEND_NAME_LATEST)

push: push_backend push_frontend

latest: push_backend_latest push_frontend_latest

kind_create:
	kind create cluster -n hl2022

kind_delete:
	kind delete cluster -n hl2022

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
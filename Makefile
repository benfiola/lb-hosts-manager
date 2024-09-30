ASSETS ?= $(shell pwd)/.dev
DEV ?= $(shell pwd)/dev
CLOUD_PROVIDER_KIND_VERSION ?= 0.4.0
KIND_CLUSTER_NAME ?= lb-hosts-manager
# NOTE: KIND_NODE_IMAGE, KIND_VERSION, KUBERNETES_VERSION are coupled
KIND_NODE_IMAGE ?= kindest/node:v1.30.4@sha256:976ea815844d5fa93be213437e3ff5754cd599b040946b5cca43ca45c2047114
KIND_VERSION ?= 0.24.0
KUBERNETES_VERSION ?= 1.30.4

OS = $(shell go env GOOS)
ARCH = $(shell go env GOARCH)

CLOUD_PROVIDER_KIND = $(ASSETS)/cloud-provider-kind
CLOUD_PROVIDER_KIND_CMD = $(CLOUD_PROVIDER_KIND)
CLOUD_PROVIDER_KIND_LOG = $(ASSETS)/cloud-provider-kind.log
CLOUD_PROVIDER_KIND_URL = https://github.com/kubernetes-sigs/cloud-provider-kind/releases/download/v$(CLOUD_PROVIDER_KIND_VERSION)/cloud-provider-kind_$(CLOUD_PROVIDER_KIND_VERSION)_$(OS)_$(ARCH).tar.gz
KIND = $(ASSETS)/kind
KIND_CMD = env KUBECONFIG=$(KUBECONFIG) KIND_CLUSTER_NAME=$(KIND_CLUSTER_NAME) kind
KIND_LB_HOSTS_MANAGER_MANIFEST_SRC = $(DEV)/lb-hosts-manager.yaml
KIND_URL = https://github.com/kubernetes-sigs/kind/releases/download/v$(KIND_VERSION)/kind-$(OS)-$(ARCH)
KUBECONFIG = $(ASSETS)/kube-config.yaml
KUBECTL = $(ASSETS)/kubectl
KUBECTL_CMD = env KUBECONFIG=$(KUBECONFIG) $(ASSETS)/kubectl
KUBECTL_URL = https://dl.k8s.io/release/v$(KUBERNETES_VERSION)/bin/$(OS)/$(ARCH)/kubectl
TEST_RESOURCES_MANIFEST = $(ASSETS)/test-resources.yaml
TEST_RESOURCES_MANIFEST_SRC = $(DEV)/test-resources.yaml

# MAYBE_CREATE_KIND_CLUSTER is a conditional target that is set to 'create-kind-cluster' only if a matching cluster doesn't exist
MAYBE_CREATE_KIND_CLUSTER = create-kind-cluster
ifneq (,$(wildcard $(KIND)))
	ifneq (,$(shell $(KIND_CMD) get clusters | grep $(KIND_CLUSTER_NAME)))
		MAYBE_CREATE_KIND_CLUSTER = 
	endif
endif

.PHONY: default
default: 

.PHONY: clean
clean: delete-kind-cluster
	# delete asset directory
	rm -rf $(ASSETS)

.PHONY: create-cluster
create-cluster: $(MAYBE_CREATE_KIND_CLUSTER) get-kind-cluster-kubeconfig apply-manifests start-cloud-provider-kind

.PHONY: get-kind-cluster-kubeconfig
get-kind-cluster-kubeconfig: $(KIND) | $(ASSETS)
	# delete existing kubeconfigs
	rm -rf $(KUBECONFIG) /tmp/kube-config.yaml
	# export kind cluster kubeconfig to temporary location
	# NOTE: uses a temporary kubeconfig path - kind tries to acquire a file lock on the kubeconfig file.  if $(KUBECONFIG) is on a virtiofs mount, this will fail.
	$(KIND_CMD) export kubeconfig --kubeconfig /tmp/kube-config.yaml
	# move kubeconfig to correct location
	mv /tmp/kube-config.yaml $(KUBECONFIG)

# NOTE: assumes that cluster is already created
.PHONY: apply-manifests
apply-manifests: $(KUBECTL) $(TEST_RESOURCES_MANIFEST)
	# apply test resources manifest
	$(KUBECTL_CMD) apply -f $(TEST_RESOURCES_MANIFEST)

# NOTE: assumes that cluster is already created
.PHONY: unapply-manifests
unapply-manifests: $(KUBECTL) $(TEST_RESOURCES_MANIFEST)
	# delete resources created by test resources manifest
	$(KUBECTL_CMD) delete -f $(TEST_RESOURCES_MANIFEST) --ignore-not-found=true

.PHONY: create-kind-cluster
create-kind-cluster: $(KIND)
	# create kind cluster
	# NOTE: uses a temporary kubeconfig path - kind tries to acquire a file lock on the kubeconfig file.  if $(KUBECONFIG) is on a virtiofs mount, this will fail.
	$(KIND_CMD) create cluster --kubeconfig /tmp/kube-config.yaml --image $(KIND_NODE_IMAGE)
	# remove temporary kubeconfig
	rm -f /tmp/kube-config.yaml

.PHONY: delete-kind-cluster
delete-kind-cluster: $(KIND)
	# delete kind cluster
	# NOTE: uses a temporary kubeconfig path - kind tries to acquire a file lock on the kubeconfig file.  if $(KUBECONFIG) is on a virtiofs mount, this will fail.
	$(KIND_CMD) delete cluster --kubeconfig /tmp/kube-config.yaml
	# delete kubeconfig
	rm -f $(KUBECONFIG)

.PHONY: install-tools
install-tools: $(CLOUD_PROVIDER_KIND) $(KIND) $(KUBECTL)

.PHONY: start-cloud-provider-kind
start-cloud-provider-kind: $(CLOUD_PROVIDER_KIND)
	# send SIGTERM to existing cloud-provider-kind
	pkill -x -f $(CLOUD_PROVIDER_KIND) || true
	# wait for cloud-provider-kind to exit
	while true; do pgrep -x -f $(CLOUD_PROVIDER_KIND) || break; sleep 1; done
	# launch cloud-provider-kind
	nohup $(CLOUD_PROVIDER_KIND_CMD) > $(CLOUD_PROVIDER_KIND_LOG) 2>&1 &

$(ASSETS):
	# create .dev directory
	mkdir -p $(ASSETS)

$(CLOUD_PROVIDER_KIND): | $(ASSETS)
	# install cloud-provider-kind
	# create extract directory
	mkdir -p $(ASSETS)/.tmp
	# download archive
	curl -o $(ASSETS)/.tmp/archive.tar.gz -fsSL $(CLOUD_PROVIDER_KIND_URL)
	# extract archive
	tar xzf $(ASSETS)/.tmp/archive.tar.gz -C $(ASSETS)/.tmp
	# copy executable
	cp $(ASSETS)/.tmp/cloud-provider-kind $(ASSETS)/cloud-provider-kind
	# delete extract directory
	rm -rf $(ASSETS)/.tmp

$(KIND): | $(ASSETS)
	# install kind
	# download
	curl -o $(KIND) -fsSL $(KIND_URL)
	# make executable
	chmod +x $(KIND)

$(KUBECTL): | $(ASSETS)
	# install kubectl
	# download
	curl -o $(KUBECTL) -fsSL $(KUBECTL_URL)
	# make kubectl executable
	chmod +x $(KUBECTL)

$(TEST_RESOURCES_MANIFEST): | $(ASSETS)
	# copy test resources manifest
	cp $(TEST_RESOURCES_MANIFEST_SRC) $(TEST_RESOURCES_MANIFEST)

GOPATH?=${HOME}/go
CONTAINER_REPO?=
GET_NAMESPACE_SH=kubectl config view --minify --output 'jsonpath={..namespace}'
ifeq (, $(shell ${GET_NAMESPACE_SH}))
	NAMESPACE?=default
else
	NAMESPACE?=$(shell ${GET_NAMESPACE_SH})
endif
REVISION?=1
TAG?=${NAMESPACE}-${REVISION}
VIMAGE_NAME=vertica-k8s
INT_TEST_TIMEOUT=30m
# Setting the communal path here is for the integration tests.  It must be an
# s3 endpoint and will trigger a new tenant in the minIO operator.
COMMUNAL_PATH?="s3://nimbusdb/${HOSTNAME}-${USER}/db"
INT_TEST_OUTPUT_DIR?=$(PWD)/int-tests-output
TMPDIR?=$(PWD)
HELM_UNITTEST_PLUGIN_INSTALLED=$(shell helm plugin list | grep -c '^unittest')
ifeq ($(VERBOSE), FALSE)
WAIT_FOR_INT_TESTS_ARGS?=-q
endif

CERT_DIR=cert/
REQ_CONF=$(CERT_DIR)openssl_req.conf
ROOT_KEY=$(CERT_DIR)root.key
ROOT_CRT=$(CERT_DIR)root.crt
SERVER_KEY=$(CERT_DIR)server.key
SERVER_CSR=$(CERT_DIR)server.csr
SERVER_CRT=$(CERT_DIR)server.crt
CLIENT_KEY=$(CERT_DIR)client.key
CLIENT_CSR=$(CERT_DIR)client_cert.csr
CLIENT_CRT=$(CERT_DIR)client.crt
CLIENT_TLS_SECRET=vertica-client-tls
SERVER_TLS_SECRET=vertica-server-tls

.PHONY: lint
lint:
	helm lint helm-charts/vertica helm-charts/vertica-int-tests

.PHONY: install-unittest-plugin
install-unittest-plugin:
ifeq ($(HELM_UNITTEST_PLUGIN_INSTALLED), 0)
	helm plugin install https://github.com/quintush/helm-unittest
endif

.PHONY: run-unit-tests
run-unit-tests: install-unittest-plugin
	helm unittest --helm3 --output-type JUnit --output-file $(TMPDIR)/unit-tests.xml helm-charts/vertica

.PHONY: stop-stern
stop-stern:
ifneq (,$(wildcard stern.pid))
	kill -INT $(shell cat stern.pid) || :
	rm stern.pid 2> /dev/null || :
endif

.PHONY: clean-int-tests
clean-int-tests: stop-stern
	helm uninstall tests || :

.PHONY: run-int-tests
run-int-tests: clean-int-tests
	mkdir -p $(INT_TEST_OUTPUT_DIR)
	daemonize -c $(PWD) -p stern.pid -l stern.pid -v \
		-e $(INT_TEST_OUTPUT_DIR)/int-tests.stderr \
		-o $(INT_TEST_OUTPUT_DIR)/int-tests.stdout \
		$(shell which stern) --timestamps oct-.\*
	helm install tests \
		--set communalStorage.path=${COMMUNAL_PATH} \
		--set pythonToolsTag=${TAG} \
		--set tls.serverSecret=${SERVER_TLS_SECRET} \
		--set tls.clientSecret=${CLIENT_TLS_SECRET} \
		--set pythonToolsRepo=${CONTAINER_REPO}python-tools \
		${EXTRA_HELM_ARGS} \
		helm-charts/vertica-int-tests
	timeout --foreground ${INT_TEST_TIMEOUT} scripts/wait-for-int-tests.sh $(WAIT_FOR_INT_TESTS_ARGS)
	$(MAKE) stop-stern

.PHONY: clean-deploy
clean-deploy: clean-tls-secrets
	helm uninstall cluster 2> /dev/null || :
	scripts/blastdb.sh 2> /dev/null || :

.PHONY: deploy-kind
deploy-kind: clean-deploy create-tls-secrets
	helm install cluster \
		-f helm-charts/vertica/kind-overrides.yaml \
		--set image.server.tag=${TAG} \
		--set db.storage.communal.path=${COMMUNAL_PATH} \
		helm-charts/vertica
	timeout --foreground 20m scripts/wait-for-deploy.sh

docker-build: docker-build-vertica docker-build-python-tools

.PHONY: docker-build-vertica
docker-build-vertica: docker-vertica/Dockerfile
	cd docker-vertica \
	&& make CONTAINER_REPO=${CONTAINER_REPO} TAG=${TAG}

.PHONY: docker-build-python-tools
docker-build-python-tools:
	cd docker-python-tools \
	&& docker build -t ${CONTAINER_REPO}python-tools:${TAG} .

.PHONY: docker-push
docker-push:
	docker push ${CONTAINER_REPO}${VIMAGE_NAME}:${TAG}
	docker push ${CONTAINER_REPO}python-tools:${TAG}

.PHONY: tls_config
tls_config:
	@echo "[req] "                                   > $(REQ_CONF)
	@echo "prompt                  = no"            >> $(REQ_CONF)
	@echo "distinguished_name      = CStore4Ever"   >> $(REQ_CONF)
	@echo "[CStore4Ever]"                           >> $(REQ_CONF)
	@echo "C                       = US"            >> $(REQ_CONF)
	@echo "ST                      = Massacussetts" >> $(REQ_CONF)
	@echo "O                       = $(ONAME)"      >> $(REQ_CONF)
	@echo "CN                      = INVALIDHOST"   >> $(REQ_CONF)
	@echo "emailAddress            = foo@bar.com"   >> $(REQ_CONF)

.PHONY: create-tls-keys
create-tls-keys:
	mkdir -p $(CERT_DIR)
	@echo "Generating SSL certificates"
	@# Generate CA files (so we can sign server and client keys)
	@$(MAKE)    ONAME="Certificate Authority"   tls_config
	@openssl genrsa                                                        -out $(ROOT_KEY)
	@openssl req -config $(REQ_CONF) -new -x509 -key $(ROOT_KEY)         -out $(ROOT_CRT)
	@# Make server private and public keys
	@$(MAKE)    ONAME="Vertica Server"          tls_config
	@openssl genrsa                                                        -out $(SERVER_KEY)
	@openssl req -config $(REQ_CONF) -new       -key $(SERVER_KEY) -out $(SERVER_CSR)
	@openssl x509 -req -in $(SERVER_CSR) \
		-days 3650 -sha1 -CAcreateserial -CA $(ROOT_CRT) -CAkey $(ROOT_KEY) \
		-out $(SERVER_CRT)
	@# Make client private and public keys
	@$(MAKE)    ONAME="Vertica Client"          tls_config
	@openssl genrsa                                                        -out $(CLIENT_KEY)
	@openssl req -config $(REQ_CONF) -new       -key $(CLIENT_KEY) -out $(CLIENT_CSR)
	@openssl x509 -req -in $(CLIENT_CSR) \
		-days 3650 -sha1 -CAcreateserial -CA $(ROOT_CRT) -CAkey $(ROOT_KEY) \
		-out $(CLIENT_CRT)

.PHONY: clean-tls-secrets
clean-tls-secrets:
	kubectl delete secret $(CLIENT_TLS_SECRET) || :
	kubectl delete secret $(SERVER_TLS_SECRET) || :

.PHONY: create-tls-secrets
create-tls-secrets: clean-tls-secrets create-tls-keys
	kubectl create secret tls $(CLIENT_TLS_SECRET) --cert=$(CLIENT_CRT) --key=$(CLIENT_KEY)
	kubectl create secret generic $(SERVER_TLS_SECRET) \
		--from-file=tls.crt=$(SERVER_CRT) \
		--from-file=tls.key=$(SERVER_KEY) \
		--from-file=tls.rootca=$(ROOT_CRT)

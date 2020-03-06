#!/bin/bash

set -e

: ${IMAGE:?Need to set metrics-agent IMAGE variable to test}
: ${KUBERNETES_VERSION:?Need to set KUBERNETES_VERSION to test}

export WORKINGDIR=$(PWD)/testdata/e2e/e2e-${KUBERNETES_VERSION}

cleanup() {
  # export WORKINGDIR=$(PWD)/testdata/e2e/e2e-${KUBERNETES_VERSION}
  kind delete cluster --name=e2e-${KUBERNETES_VERSION} &> /dev/null || true
  if [ -d $WORKINGDIR ]; then
    echo "Cleaning up Temp directory : ${WORKINGDIR}"
    rm -rf $WORKINGDIR
  fi
}

setup_kind() {

  export PATH=$(go env GOPATH)/bin:$PATH

  cleanup

  if ! (kind create cluster --name=e2e-${KUBERNETES_VERSION} --image=kindest/node:${KUBERNETES_VERSION}) ; then
    echo "Could not create KinD cluster"
    exit 1
  fi

  sleep 2
  
  i=0
  until [ $i -ge 5 ]
  do
   kind load docker-image ${IMAGE} --name e2e-${KUBERNETES_VERSION} && echo "${IMAGE} image added to cluster" && break
   n=$[$i+1]
   sleep 15
done

}

deploy(){
  mkdir -p -m 0777 ${WORKINGDIR}
  # export WORKINGDIR=$(PWD)/testdata/e2e/e2e-${KUBERNETES_VERSION}
  # Bail out if the temp directory wasn't created successfully.
  if [ ! -d $WORKINGDIR ]; then
    >&2 echo "Failed to create temp directory ${WORKINGDIR}"
    exit 1
  fi
  
  echo ${WORKINGDIR}


  kubectl apply -f deploy/kubernetes/
  export CONTAINER="\"name\": \"metrics-agent\", \"image\": \"${IMAGE}\",\"imagePullPolicy\": \"Never\""
  export ENVS="\"env\": [{\"name\": \"CLOUDABILITY_CLUSTER_NAME\", \"value\": \"e2e\"}, {\"name\": \"CLOUDABILITY_POLL_INTERVAL\", \"value\": \"20\"} ]"


  # {\"apiVersion\": \"v1\",\"kind\": \"Pod\", \"metadata\": {\"name\": \"stress\"},\"spec\": {\"containers\": [{\"name\": \"stress\",\"image\": \"jfusterm/stress\",\"args\": [\"--vm\",\"1\",\"--vm-bytes\",\"127M\"],\"resources\": {\"limits\": {\"cpu\": \"200m\",\"memory\": \"128Mi\"},\"requests\": {\"cpu\": \"200m\",\"memory\": \"128Mi\"}}}]}}

    kubectl -n cloudability patch deployment metrics-agent --patch \
  "{\"spec\": {\"template\": {\"spec\": {\"containers\": [{${CONTAINER}, ${ENVS} }]}}}}"
    kubectl create ns stress
    kubectl -n stress run stress --labels=app=stress --image=jfusterm/stress -- --cpu 50 --vm 1 --vm-bytes 127m
}

wait_for_metrics() {
  # Wait for metrics-agent pod ready
  while [[ $(kubectl get pods -n cloudability -l app=metrics-agent -o 'jsonpath={..status.conditions[?(@.type=="Ready")].status}') != "True" ]]; do
    echo "waiting for pod ready" && sleep 5;
  done
}

get_sample_data(){
  echo "Waiting for agent data collection"
  sleep 30
  POD=$(kubectl get pod -n cloudability -l app=metrics-agent -o jsonpath="{.items[0].metadata.name}")
  kubectl cp cloudability/$POD:/tmp ${WORKINGDIR} 

}

run_tests() {
  echo tests
  WORKING_DIR=${WORKINGDIR} go test test/e2e_test.go -v
  sleep 90000
}

trap cleanup EXIT
setup_kind
deploy
wait_for_metrics
get_sample_data
run_tests

#!/bin/bash
# Uninstalls the helm chart to easily upgrade to the event broker software operator.
#   It ensures the PVC is not uninstalled during the process
#   It migrates secrets from the helm chart deployment to event broker software operator
# Params:
#   $1: the chart name
#   $2: namespace of deployment
# Assumes being run on a Kubernetes environment with enough resources for HA dev deployment
#   - kubectl configured


kubectl create secret generic broker-secret --from-literal=username_admin_password=admin
kubectl annotate pvc --all "helm.sh/resource-policy=keep" -n $2
kubectl annotate pv --all "helm.sh/resource-policy=keep" -n $2
helm uninstall $1
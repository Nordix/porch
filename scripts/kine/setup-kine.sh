#!/usr/bin/env bash
# Copyright 2026 The kpt Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Move Porch's CRD-backed resources on a kind cluster (Repositories, v1alpha2
# PackageRevisions, PackageVariants, ...) from the cluster's etcd to Kine +
# Postgres, using an aggregated apiserver:
#
#   main kube-apiserver --APIService--> kine-apiserver --etcd API--> Kine --SQL--> Postgres
#
# Usage: setup-kine.sh [install|status|uninstall] [options]
#   install     (default) deploy Kine and redirect the Porch CRD groups to it
#   status      show where the Porch groups are served from and the Kine key count
#   uninstall   remove the redirect, restore the CRDs in the main cluster, delete kine-system
#
# Uses the current kubectl context, which must be a kind cluster (kind-*).
# WARNING: install and uninstall delete all Porch objects stored under the CRDs.
#
# Options:
#   --yes, -y        don't ask before the destructive step
#   --allow-non-kind run against a context that isn't kind-* (use with care)
#   --port N         local port for the kine-apiserver port-forward (default: 16443)
#   -h, --help       show this help

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
porch_dir="$(cd "$script_dir/../.." && pwd)"
manifests_dir="$porch_dir/deployments/kine"
crds_dir="$porch_dir/api/generated/crds"
state_dir="$porch_dir/.build/kine"
crd_backup="$state_dir/porch-crds-backup.json"

ns="kine-system"
backend_svc="kine-apiserver"
porch_ns="porch-system"
porch_groups_re='^(porch\.kpt\.dev|config\.porch\.kpt\.dev)$'
webhook_configs=(packagerevision-validating-webhook-configuration repository-validating-webhook-configuration)
managed_by_label="app.kubernetes.io/managed-by=porch-setup-kine"

cmd="install"
assume_yes="false"
allow_non_kind="false"
pf_port="16443"

usage() { sed -n '16,34p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; }

while [[ $# -gt 0 ]]; do
    case "$1" in
        install|status|uninstall) cmd="$1" ;;
        --port) pf_port="$2"; shift ;;
        --port=*) pf_port="${1#*=}" ;;
        -y|--yes) assume_yes="true" ;;
        --allow-non-kind) allow_non_kind="true" ;;
        -h|--help) usage; exit 0 ;;
        *) echo "Unknown argument: $1" >&2; usage >&2; exit 1 ;;
    esac
    shift
done

tmp_dir="$(mktemp -d)"
pf_pid=""
cleanup() {
    if [[ -n "$pf_pid" ]]; then kill "$pf_pid" 2>/dev/null || true; fi
    rm -rf "$tmp_dir"
}
trap cleanup EXIT

step() { echo; echo "=== $* ==="; }
info() { echo "  $*"; }
die()  { echo "ERROR: $*" >&2; exit 1; }

for bin in kubectl jq openssl; do
    command -v "$bin" >/dev/null || die "$bin not found on PATH"
done

cluster="$(command kubectl config current-context 2>/dev/null)" || die "no current kubectl context"
# This script replaces the cluster's Porch APIServices and deletes its Porch
# CRDs, so only run it against kind clusters. A stray KUBECONFIG pointing at a
# shared cluster would otherwise be silently modified.
if [[ "$cluster" != kind-* && "$allow_non_kind" != "true" ]]; then
    die "current context '$cluster' (KUBECONFIG=${KUBECONFIG:-~/.kube/config}) is not a kind cluster; switch context or pass --allow-non-kind"
fi
# Pin every kubectl call to the context checked above, even if KUBECONFIG or
# the current context change while the script runs.
kubectl() { command kubectl --context "$cluster" "$@"; }
kubectl get --raw /readyz >/dev/null || die "context '$cluster' is not reachable"

backend_kubeconfig="$tmp_dir/kine-apiserver.kubeconfig"
# The backend has its own kubeconfig, so bypass the pinned-context wrapper.
kb() { command kubectl --kubeconfig "$backend_kubeconfig" "$@"; }

# APIServices in the main cluster that point at kine-apiserver, one name per line.
redirected_apiservices() {
    kubectl get apiservice -o json | jq -r --arg ns "$ns" --arg svc "$backend_svc" \
        '.items[] | select(.spec.service.namespace == $ns and .spec.service.name == $svc) | .metadata.name'
}

is_redirected() { [[ -n "$(redirected_apiservices)" ]]; }

porch_crd_names() {
    kubectl get crd -o json | jq -r --arg re "$porch_groups_re" '.items[] | select(.spec.group | test($re)) | .metadata.name'
}

# Read the admin token from the secret, creating it on first run.
admin_token() {
    if ! kubectl -n "$ns" get secret kine-apiserver-auth >/dev/null 2>&1; then
        local token
        token="$(openssl rand -hex 32)"
        printf '%s,kine-admin,kine-admin,system:masters\n' "$token" > "$tmp_dir/tokens.csv"
        kubectl -n "$ns" create secret generic kine-apiserver-auth --from-file=tokens.csv="$tmp_dir/tokens.csv" >/dev/null
        rm -f "$tmp_dir/tokens.csv"
    fi
    kubectl -n "$ns" get secret kine-apiserver-auth -o jsonpath='{.data.tokens\.csv}' | base64 -d | head -1 | cut -d, -f1
}

# Port-forward to kine-apiserver and write an admin kubeconfig for it.
connect_backend() {
    local token
    token="$(admin_token)"
    kubectl -n "$ns" port-forward "svc/$backend_svc" "$pf_port:443" >"$tmp_dir/pf.log" 2>&1 &
    pf_pid=$!
    cat > "$backend_kubeconfig" <<EOF
apiVersion: v1
kind: Config
clusters:
- name: kine-apiserver
  cluster:
    server: https://127.0.0.1:$pf_port
    insecure-skip-tls-verify: true
users:
- name: kine-admin
  user:
    token: $token
contexts:
- name: kine-apiserver
  context: {cluster: kine-apiserver, user: kine-admin}
current-context: kine-apiserver
EOF
    for _ in $(seq 1 30); do
        kb get --raw /readyz >/dev/null 2>&1 && return 0
        kill -0 "$pf_pid" 2>/dev/null || die "port-forward exited: $(cat "$tmp_dir/pf.log")"
        sleep 1
    done
    die "kine-apiserver not reachable on 127.0.0.1:$pf_port"
}

# Porch group/versions served by kine-apiserver, as APIService names
# (<version>.<group>), one per line. Retries while discovery catches up.
backend_group_versions() {
    local gvs=""
    for _ in $(seq 1 30); do
        gvs="$(kb get --raw /apis 2>/dev/null | jq -r --arg re "$porch_groups_re" \
            '.groups[] | select(.name | test($re)) | .name as $g | .versions[] | "\(.version).\($g)"' || true)"
        [[ -n "$gvs" ]] && break
        sleep 2
    done
    printf '%s\n' "$gvs" | sed '/^$/d'
}

kine_key_count() {
    kubectl -n "$ns" exec deploy/kine-postgres -- psql -U kine -d kine -tAc \
        "select count(*) from kine where name like '/registry/porch.kpt.dev/%' or name like '/registry/config.porch.kpt.dev/%'" 2>/dev/null | tr -d '[:space:]'
}

restart_porch() {
    if kubectl get ns "$porch_ns" >/dev/null 2>&1; then
        kubectl -n "$porch_ns" rollout restart deploy >/dev/null
        kubectl -n "$porch_ns" rollout status deploy --timeout=300s
    else
        info "namespace $porch_ns not found, skipping Porch restart"
    fi
}

do_status() {
    step "APIServices for the Porch groups"
    kubectl get apiservice | grep -E 'NAME|porch' || true
    if kubectl get ns "$ns" >/dev/null 2>&1; then
        step "$ns"
        kubectl -n "$ns" get deploy,svc
        echo "  Porch keys stored in Kine: $(kine_key_count || echo n/a)"
    else
        info "namespace $ns not present"
    fi
    echo
    if is_redirected; then echo "Porch storage: Kine"; else echo "Porch storage: cluster etcd (not redirected)"; fi
}

do_install() {
    local server_version
    server_version="$(kubectl get --raw /version | jq -r .gitVersion)"
    echo "Cluster: $cluster (Kubernetes $server_version)"
    mkdir -p "$state_dir"

    step "1/6 Postgres"
    kubectl apply -f "$manifests_dir/00-kine.yaml"
    kubectl -n "$ns" rollout status deploy/kine-postgres --timeout=300s
    # Earlier versions ran Kine as its own Deployment; Kine is now a sidecar.
    kubectl -n "$ns" delete deploy/kine svc/kine --ignore-not-found >/dev/null

    step "2/6 kine-apiserver (kube-apiserver + Kine sidecar)"
    if ! kubectl -n "$ns" get secret kine-apiserver-sa >/dev/null 2>&1; then
        openssl genrsa -out "$tmp_dir/sa.key" 2048 2>/dev/null
        openssl rsa -in "$tmp_dir/sa.key" -pubout -out "$tmp_dir/sa.pub" 2>/dev/null
        kubectl -n "$ns" create secret generic kine-apiserver-sa \
            --from-file=sa.key="$tmp_dir/sa.key" --from-file=sa.pub="$tmp_dir/sa.pub" >/dev/null
        rm -f "$tmp_dir/sa.key" "$tmp_dir/sa.pub"
        info "created secret kine-apiserver-sa"
    fi
    admin_token >/dev/null
    # CA the main apiserver uses for its front-proxy client cert when it forwards requests.
    kubectl -n kube-system get cm extension-apiserver-authentication \
        -o jsonpath='{.data.requestheader-client-ca-file}' > "$tmp_dir/front-proxy-ca.crt"
    [[ -s "$tmp_dir/front-proxy-ca.crt" ]] || die "requestheader-client-ca-file not found in kube-system/extension-apiserver-authentication"
    kubectl -n "$ns" create configmap kine-apiserver-front-proxy \
        --from-file=requestheader-client-ca-file="$tmp_dir/front-proxy-ca.crt" \
        --dry-run=client -o yaml | kubectl apply -f - >/dev/null
    # Run the same kube-apiserver version as the kind node.
    sed "s|registry.k8s.io/kube-apiserver:v[0-9][0-9.]*|registry.k8s.io/kube-apiserver:${server_version}|" \
        "$manifests_dir/10-apiserver.yaml" | kubectl apply -f -
    kubectl -n "$ns" rollout status "deploy/$backend_svc" --timeout=300s
    connect_backend

    step "3/6 Porch CRDs + webhooks into kine-apiserver"
    # Prefer the CRDs currently installed in the main cluster so both sides match.
    # Keep a backup so uninstall can restore them.
    if [[ -n "$(porch_crd_names)" ]]; then
        kubectl get crd -o json | jq --arg re "$porch_groups_re" '{
            apiVersion: "v1", kind: "List",
            items: [.items[] | select(.spec.group | test($re)) | {
                apiVersion, kind,
                metadata: {name: .metadata.name, labels: (.metadata.labels // {}),
                           annotations: ((.metadata.annotations // {}) | del(.["kubectl.kubernetes.io/last-applied-configuration"]))},
                spec}]}' > "$crd_backup"
        info "saved $(jq '.items | length' "$crd_backup") CRDs to $crd_backup"
        kb apply --server-side --force-conflicts -f "$crd_backup" >/dev/null
    elif [[ -s "$crd_backup" ]]; then
        info "no Porch CRDs in the main cluster (already redirected?), using $crd_backup"
        kb apply --server-side --force-conflicts -f "$crd_backup" >/dev/null
    else
        info "no Porch CRDs found anywhere, using $crds_dir"
        kb apply --server-side --force-conflicts -f "$crds_dir/" >/dev/null
    fi
    kb wait --for=condition=Established crd --all --timeout=120s >/dev/null
    kb get crd

    # Admission for these resources now runs in kine-apiserver, so it needs the
    # Porch webhooks. kine-apiserver can't resolve Service references itself,
    # so point them at the in-cluster service URL instead.
    local w
    for w in "${webhook_configs[@]}"; do
        if ! kubectl get validatingwebhookconfiguration "$w" >/dev/null 2>&1; then
            info "webhook $w not in the main cluster, skipping (is porch-controllers deployed?)"
            continue
        fi
        kubectl get validatingwebhookconfiguration "$w" -o json | jq '
            {apiVersion, kind, metadata: {name: .metadata.name},
             webhooks: [.webhooks[] | .clientConfig |= (
                 if .service then
                   {caBundle, url: "https://\(.service.name).\(.service.namespace).svc:\(.service.port // 443)\(.service.path // "")"}
                 else . end)]}' | kb apply -f - >/dev/null
        info "webhook $w copied"
    done

    # Redirect every Porch group/version kine-apiserver serves, except ones the
    # main cluster already routes to another server (e.g. v1alpha1.porch.kpt.dev
    # -> porch-server).
    local gv owner redirect=()
    while read -r gv; do
        owner="$(kubectl get apiservice "$gv" -o jsonpath='{.spec.service.namespace}/{.spec.service.name}' 2>/dev/null || true)"
        if [[ -n "$owner" && "$owner" != "/" && "$owner" != "$ns/$backend_svc" ]]; then
            info "skipping $gv: already served by $owner"
            continue
        fi
        redirect+=("$gv")
    done < <(backend_group_versions)
    [[ ${#redirect[@]} -gt 0 ]] || die "kine-apiserver serves no Porch group/versions to redirect"
    info "will redirect: ${redirect[*]}"

    step "4/6 Remove Porch CRDs from the main cluster"
    local crds
    crds="$(porch_crd_names)"
    if [[ -z "$crds" ]]; then
        info "none left, skipping"
    else
        echo "This deletes these CRDs from '$cluster' and EVERY object stored under them"
        echo "(Repositories, PackageRevisions, PackageVariants, ...). Finalizers are stripped,"
        echo "so Porch won't clean up git for them. This can't be undone."
        # shellcheck disable=SC2086
        printf '    %s\n' $crds
        if [[ "$assume_yes" != "true" ]]; then
            local answer
            read -r -p "Type 'yes' to continue: " answer
            [[ "$answer" == "yes" ]] || die "aborted (Kine is deployed, nothing deleted)"
        fi
        local crd obj_ns obj_name
        for crd in $crds; do
            kubectl delete "$crd" --all -A --wait=false >/dev/null 2>&1 || true
        done
        sleep 2
        for crd in $crds; do
            kubectl get "$crd" -A -o json 2>/dev/null \
                | jq -r '.items[] | select(.metadata.finalizers | length > 0) | "\(.metadata.namespace) \(.metadata.name)"' \
                | while read -r obj_ns obj_name; do
                    kubectl -n "$obj_ns" patch "$crd" "$obj_name" --type=merge -p '{"metadata":{"finalizers":null}}' >/dev/null 2>&1 || true
                done
        done
        # shellcheck disable=SC2086
        kubectl delete crd $crds --timeout=180s
    fi
    # Deleting a CRD makes the main apiserver remove its "Local" APIService.
    # Wait for that before creating ours, so the two don't fight.
    for gv in "${redirect[@]}"; do
        for _ in $(seq 1 60); do
            kubectl get apiservice "$gv" >/dev/null 2>&1 || break
            [[ -n "$(kubectl get apiservice "$gv" -o jsonpath='{.spec.service.name}' 2>/dev/null)" ]] && break
            sleep 2
        done
    done

    step "5/6 Redirect the Porch groups to kine-apiserver"
    # Same priorities the main apiserver gives CRD-backed groups, so preferred
    # versions don't change compared to plain CRDs.
    for gv in "${redirect[@]}"; do
        jq -n --arg name "$gv" --arg ns "$ns" --arg svc "$backend_svc" --arg label "$managed_by_label" '{
            apiVersion: "apiregistration.k8s.io/v1", kind: "APIService",
            metadata: {name: $name, labels: {($label | split("=")[0]): ($label | split("=")[1])}},
            spec: {
                group: ($name | split(".")[1:] | join(".")),
                version: ($name | split(".")[0]),
                groupPriorityMinimum: 1000,
                versionPriority: 100,
                insecureSkipTLSVerify: true,
                service: {namespace: $ns, name: $svc, port: 443}}}'
    done | kubectl apply -f -
    for gv in "${redirect[@]}"; do
        kubectl wait --for=condition=Available "apiservice/$gv" --timeout=120s
    done
    restart_porch

    step "6/6 Verify"
    kubectl get apiservice | grep -E 'NAME|porch'
    kubectl api-resources --api-group=config.porch.kpt.dev
    kubectl api-resources --api-group=porch.kpt.dev
    kubectl get repositories -A >/dev/null && info "list repositories through the main cluster: OK"
    info "Porch keys stored in Kine: $(kine_key_count)"
    echo
    echo "Done. Porch's CRD-backed resources are now stored in Kine."
    echo "Re-run this script after any 'make run-in-kind-*', which reinstalls the CRDs in the main cluster."
}

do_uninstall() {
    echo "This removes the redirect and deletes namespace $ns, including every Porch object stored in Kine."
    if [[ "$assume_yes" != "true" ]]; then
        local answer
        read -r -p "Type 'yes' to continue: " answer
        [[ "$answer" == "yes" ]] || die "aborted"
    fi

    step "1/3 Remove redirect APIServices"
    local gv
    for gv in $(redirected_apiservices); do
        kubectl delete apiservice "$gv"
    done

    step "2/3 Restore Porch CRDs in the main cluster"
    if [[ -s "$crd_backup" ]]; then
        kubectl apply --server-side --force-conflicts -f "$crd_backup" >/dev/null
    else
        info "no backup found, using $crds_dir"
        kubectl apply --server-side --force-conflicts -f "$crds_dir/" >/dev/null
    fi
    # shellcheck disable=SC2046
    kubectl wait --for=condition=Established $(porch_crd_names | sed 's|^|crd/|') --timeout=120s >/dev/null
    restart_porch

    step "3/3 Delete $ns"
    kubectl delete ns "$ns" --ignore-not-found --timeout=180s
    echo
    echo "Done. Porch storage is back on the cluster etcd."
}

case "$cmd" in
    install) do_install ;;
    status) do_status ;;
    uninstall) do_uninstall ;;
esac

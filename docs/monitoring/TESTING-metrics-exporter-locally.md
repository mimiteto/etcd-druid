# Testing the etcd-metrics-exporter against a local etcd-druid

This is a manual test plan for the `etcd-metrics-exporter` sidecar on a local
KIND-based etcd-druid setup. It covers building and loading the image, wiring the
image vector override, verifying the sidecar in both non-TLS (http) and TLS (mTLS)
modes, and validating the example PodMonitor + Grafana dashboard.

> Scope: local KIND cluster (not a full Gardener landscape). etcd-druid's local dev
> flow is KIND + a local registry + skaffold/helm. See
> `docs/deployment/getting-started-locally/getting-started-locally.md`.

## Why this needs care (read first)

1. **The image vector gap.** The etcd pod's containers — `etcd-wrapper`,
   `backup-restore`, and now `metrics-exporter` — take their images from the
   embedded image vector (`internal/images/images.yaml`), **not** from the druid
   Deployment image. Skaffold builds `local-skaffold/etcd-metrics-exporter` but
   **nothing consumes it**: the pods would try to pull
   `europe-docker.pkg.dev/.../etcd-metrics-exporter:v0.1.0`, which does not exist
   yet, and land in `ImagePullBackOff`. We must override the image vector via the
   `IMAGEVECTOR_OVERWRITE` env var, which is **not** wired into local tooling —
   Phase 1 does it manually.
2. **TLS mode.** The default example Etcd has client TLS **off**, so the exporter
   runs in **http** mode. The **mTLS** path is only exercised when
   `spec.etcd.clientUrlTls` is set (Phase 3).
3. **No monitoring stack locally.** Prometheus/Grafana are absent from the local
   setup; Phase 4 brings up our own.

## Prerequisites

- `docker`, `kind`, `yq`, `kubectl`, `helm`, `skaffold`, Go toolchain.
- Repo checked out on the feature branch; `make` targets available.

---

## Phase 0 — Cluster + build & load the exporter image

```bash
# From the repo root
make kind-up
export KUBECONFIG=$PWD/hack/kind/kubeconfig
kubectl get nodes   # sanity: one control-plane node Ready

# kind-up also starts a local registry container at localhost:5001.
# Build the exporter image (its Dockerfile stage is `etcd-metrics-exporter`)
# and push it to the local registry so the kind node can pull it.
docker build --target etcd-metrics-exporter -t localhost:5001/etcd-metrics-exporter:dev .
docker push localhost:5001/etcd-metrics-exporter:dev
```

**Verify:**
```bash
curl -s http://localhost:5001/v2/etcd-metrics-exporter/tags/list
# expect: {"name":"etcd-metrics-exporter","tags":["dev"]}
```

> Note: `make docker-build` builds only the default (`druid`) stage, so we build the
> exporter stage explicitly here. The `--target etcd-metrics-exporter` stage was
> added in the build wiring commit.

---

## Phase 1 — Wire the image vector override

Deploy druid, then point the etcd-metrics-exporter image at the locally built one.

```bash
make deploy                 # skaffold builds+loads druid, helm-installs the chart
kubectl -n default rollout status deploy/etcd-druid
```

Create the overwrite ConfigMap (override **only** the exporter entry):
```bash
cat <<'EOF' > /tmp/images_overwrite.yaml
images:
- name: etcd-metrics-exporter
  repository: localhost:5001/etcd-metrics-exporter
  tag: "dev"
EOF

kubectl -n default create configmap etcd-druid-images-overwrite \
  --from-file=images_overwrite.yaml=/tmp/images_overwrite.yaml
```

Mount it into the druid Deployment and set the env var (the chart has no hook for
this, so patch it directly):
```bash
kubectl -n default patch deploy/etcd-druid --type=strategic -p '
spec:
  template:
    spec:
      containers:
        - name: etcd-druid
          env:
            - name: IMAGEVECTOR_OVERWRITE
              value: /imagevector-overwrite/images_overwrite.yaml
          volumeMounts:
            - name: imagevector-overwrite
              mountPath: /imagevector-overwrite
              readOnly: true
      volumes:
        - name: imagevector-overwrite
          configMap:
            name: etcd-druid-images-overwrite
'
kubectl -n default rollout status deploy/etcd-druid
```
> If the druid container name differs, adjust `containers[].name`. Confirm with
> `kubectl -n default get deploy/etcd-druid -o jsonpath='{.spec.template.spec.containers[*].name}'`.

**Verify:** druid restarted cleanly and picked up the override:
```bash
kubectl -n default logs deploy/etcd-druid | grep -i "image vector\|IMAGEVECTOR" || true
# (The definitive check is in Phase 2 once an etcd pod exists.)
```

---

## Phase 2 — Non-TLS functional test (http mode)

```bash
kubectl apply -f examples/etcd/druid_v1alpha1_etcd.yaml   # client TLS OFF by default
kubectl get etcd,sts,pods -w                              # wait for pods Running
```

**Verify — container present and using the local image:**
```bash
POD=$(kubectl get pod -l app.kubernetes.io/managed-by=etcd-druid -o name | head -1)
# 3 containers, including metrics-exporter
kubectl get "$POD" -o jsonpath='{.spec.containers[*].name}{"\n"}'
# metrics-exporter image is the local one
kubectl get "$POD" -o jsonpath='{range .spec.containers[?(@.name=="metrics-exporter")]}{.image}{"\n"}{end}'
# expect: localhost:5001/etcd-metrics-exporter:dev
# args show http endpoint + insecure flags
kubectl get "$POD" -o jsonpath='{range .spec.containers[?(@.name=="metrics-exporter")]}{.args}{"\n"}{end}'
# expect: --endpoint=http://<name>-local:2379, --insecure-transport=true, --config-file=/var/etcd/config/etcd.conf.yaml
```

**Verify — metrics served:**
```bash
kubectl port-forward "$POD" 9096:9096 &
sleep 2
curl -s localhost:9096/metrics | grep -E 'etcddruid_exporter_(configured_quota_backend_bytes|resource_events_total)'
```
- `etcddruid_exporter_configured_quota_backend_bytes` should equal the
  `quota-backend-bytes` in the etcd config file:
  ```bash
  kubectl exec "$POD" -c backup-restore -- cat /var/etcd/config/etcd.conf.yaml | grep quota-backend-bytes
  ```

**Verify — resource-event churn grows** (a fresh standalone etcd has little traffic,
so generate writes inside the etcd container):
```bash
# exec into the etcd container and put keys under a few resource prefixes
kubectl exec "$POD" -c etcd -- sh -c '
  for i in $(seq 1 50); do
    etcdctl --endpoints=http://127.0.0.1:2379 put /registry/leases/x$i v >/dev/null
    etcdctl --endpoints=http://127.0.0.1:2379 put /registry/configmaps/y$i v >/dev/null
  done'
curl -s localhost:9096/metrics | grep resource_events_total
# expect non-zero counters, e.g. resource="leases", resource="configmaps"
```
> Adjust the `etcdctl` invocation to the flags the wrapper image expects; if etcdctl
> isn't on PATH in the etcd container, use a debug/ephemeral container.

---

## Phase 3 — TLS functional test (mTLS mode)

Create an Etcd **with** client TLS so the exporter uses https/mTLS.

- Easiest: reuse the e2e PKI path. `test/e2e/utils/setup.go` generates the CA +
  server/client secrets; `test/utils/etcd.go`'s `WithClientTLS()` sets the spec.
  Alternatively generate PKI with etcd-wrapper's `hack/local-dev/generate_pki.sh`
  and create the secrets by hand, then craft an Etcd manifest that references them
  in `spec.etcd.clientUrlTls` (`tlsCASecretRef`, `serverTLSSecretRef`,
  `clientTLSSecretRef`).

**Verify:**
```bash
POD=$(kubectl get pod -l app.kubernetes.io/managed-by=etcd-druid -o name | head -1)
# args now use https + cert paths
kubectl get "$POD" -o jsonpath='{range .spec.containers[?(@.name=="metrics-exporter")]}{.args}{"\n"}{end}'
# expect: --endpoint=https://<name>-local:2379, --cacert=/var/etcd/ssl/ca/..., --cert/--key under /var/etcd/ssl/client, --insecure-transport=false
# CA + client TLS volumes mounted
kubectl describe "$POD" | grep -A2 -iE 'etcd-ca|etcd-client-tls'
# no TLS handshake errors in the sidecar
kubectl logs "$POD" -c metrics-exporter | grep -iE 'tls|x509|handshake|error' || echo "no TLS errors"
# metrics still populate
kubectl port-forward "$POD" 9096:9096 & sleep 2
curl -s localhost:9096/metrics | grep etcddruid_exporter_
```

---

## Phase 4 — Scrape + dashboard

Bring up a monitoring stack (none exists locally) and validate the example
PodMonitor and dashboard.

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kube-prom prometheus-community/kube-prometheus-stack -n monitoring --create-namespace
kubectl -n monitoring rollout status deploy/kube-prom-grafana
```

Apply the example PodMonitor (in the etcd namespace, and make sure the
kube-prometheus-stack's PodMonitor selector picks it up — you may need to add the
release label, e.g. `release: kube-prom`, to the PodMonitor's metadata.labels):
```bash
# For the TLS etcd endpoint, replace REPLACE_WITH_ETCD_CA_SECRET in the file with
# the actual etcd CA secret name before applying.
kubectl apply -f docs/monitoring/etcd-metrics-exporter-podmonitor.yaml
```

**Verify — targets UP:**
```bash
kubectl -n monitoring port-forward svc/kube-prom-kube-prometheus-prometheus 9090:9090 &
# Prometheus UI > Status > Targets: both the sidecar (9096) and etcd-native (2379)
# pod targets should be UP. Or query the API:
curl -s 'http://localhost:9090/api/v1/query?query=etcddruid_exporter_resource_events_total' | jq '.data.result | length'
```

**Verify — dashboard renders:**
```bash
kubectl -n monitoring port-forward svc/kube-prom-grafana 3000:80 &
# Grafana (admin / `kubectl -n monitoring get secret kube-prom-grafana -o jsonpath='{.data.admin-password}' | base64 -d`)
# Import docs/monitoring/etcd-metrics-exporter-dashboard.json (Import > paste JSON),
# select the Prometheus datasource, pick the namespace/pod.
```
Confirm each panel:
- **Defragmentable Space %** timeseries + gauge — non-negative, sane values.
- **Backend Quota (configured vs runtime)** — both series present on one axis.
- **Quota Drift** — shows a numeric value (0 when config matches runtime). This is
  the panel whose `on(namespace, pod)` fix we validate here — it must **not** read
  "No data".
- **Resource Event Churn** timeseries + **Top Resources** bar gauge — populated
  after the Phase 2/3 write load.

---

## Teardown

```bash
kubectl delete -f examples/etcd/druid_v1alpha1_etcd.yaml
helm -n monitoring uninstall kube-prom || true
make undeploy
make kind-down
```

## Known limitations / notes

- `IMAGEVECTOR_OVERWRITE` is injected manually (Phase 1); the chart has no first-class
  value for it. Making it first-class (chart env + configmap volume) is a possible
  follow-up.
- The churn metric reflects write **activity** (watch events), not the current key
  count — matching the original `etcdctl watch | uniq -c` workflow. A quiet etcd
  shows low rates until you generate writes.
- Phase 4 requires bringing your own prometheus-operator + Grafana; the PodMonitor's
  TLS block for the etcd-native (2379) endpoint needs the real CA secret name.

# Testing the etcd-metrics-exporter locally

Manual test plan for the `etcd-metrics-exporter` sidecar on a local KIND-based
etcd-druid (KIND + local registry + skaffold/helm; not a full Gardener landscape —
see `docs/deployment/getting-started-locally/getting-started-locally.md`).

**Three gotchas:**
1. **Image vector gap** — etcd pod containers (incl. `metrics-exporter`) take images
   from `internal/images/images.yaml`, not the druid Deployment. The published
   `.../etcd-metrics-exporter:v0.1.0` doesn't exist yet → without an override the pod
   hits `ImagePullBackOff`. Phase 1 overrides it via `IMAGEVECTOR_OVERWRITE` (not
   wired into local tooling, so done by hand).
2. **TLS mode** — the default example Etcd has client TLS off → exporter runs **http**.
   mTLS is only exercised with `spec.etcd.clientUrlTls` set (Phase 3).
3. **No local monitoring** — Prometheus/Grafana are absent; Phase 4 brings its own.

**Prereqs:** `docker`, `kind`, `yq`, `kubectl`, `helm`, `skaffold`, Go; on the feature branch.

## Phase 0 — Cluster + image
```bash
make kind-up
export KUBECONFIG=$PWD/hack/kind/kubeconfig
kubectl get nodes                                                    # one node Ready
docker build --target etcd-metrics-exporter -t localhost:5001/etcd-metrics-exporter:dev .
docker push localhost:5001/etcd-metrics-exporter:dev
curl -s http://localhost:5001/v2/etcd-metrics-exporter/tags/list     # {"name":...,"tags":["dev"]}
```
(`make docker-build` builds only the `druid` stage, hence the explicit `--target`.)

## Phase 1 — Image vector override
```bash
make deploy
kubectl -n default rollout status deploy/etcd-druid

printf 'images:\n- name: etcd-metrics-exporter\n  repository: localhost:5001/etcd-metrics-exporter\n  tag: "dev"\n' > /tmp/images_overwrite.yaml
kubectl -n default create configmap etcd-druid-images-overwrite \
  --from-file=images_overwrite.yaml=/tmp/images_overwrite.yaml

kubectl -n default patch deploy/etcd-druid --type=strategic -p '
spec:
  template:
    spec:
      containers:
        - name: etcd-druid
          env: [{name: IMAGEVECTOR_OVERWRITE, value: /imagevector-overwrite/images_overwrite.yaml}]
          volumeMounts: [{name: imagevector-overwrite, mountPath: /imagevector-overwrite, readOnly: true}]
      volumes:
        - name: imagevector-overwrite
          configMap: {name: etcd-druid-images-overwrite}
'
kubectl -n default rollout status deploy/etcd-druid
```

## Phase 2 — Non-TLS (http)
```bash
kubectl apply -f examples/etcd/druid_v1alpha1_etcd.yaml     # client TLS OFF
kubectl get etcd,sts,pods -w                                # wait Running, then Ctrl-C
POD=$(kubectl get pod -l app.kubernetes.io/managed-by=etcd-druid -o name | head -1)

# 3 containers incl. metrics-exporter; local image; http endpoint + insecure flags
kubectl get "$POD" -o jsonpath='{.spec.containers[*].name}{"\n"}'
kubectl get "$POD" -o jsonpath='{range .spec.containers[?(@.name=="metrics-exporter")]}{.image}{"\n"}{.args}{"\n"}{end}'

kubectl port-forward "$POD" 9096:9096 & sleep 2
curl -s localhost:9096/metrics | grep -E 'etcddruid_exporter_(configured_quota_backend_bytes|resource_events_total)'
kubectl exec "$POD" -c backup-restore -- grep quota-backend-bytes /var/etcd/config/etcd.conf.yaml   # matches gauge

# generate writes so the churn counter grows
kubectl exec "$POD" -c etcd -- sh -c 'for i in $(seq 1 50); do
  etcdctl --endpoints=http://127.0.0.1:2379 put /registry/leases/x$i v >/dev/null
  etcdctl --endpoints=http://127.0.0.1:2379 put /registry/configmaps/y$i v >/dev/null; done'
curl -s localhost:9096/metrics | grep resource_events_total   # non-zero, per resource
```

## Phase 3 — TLS (mTLS)
Create an Etcd with `spec.etcd.clientUrlTls` + PKI secrets (reuse the e2e path:
`test/e2e/utils/setup.go` PKI + `test/utils/etcd.go` `WithClientTLS()`, or
etcd-wrapper's `hack/local-dev/generate_pki.sh`, then reference the secrets in the spec).
```bash
POD=$(kubectl get pod -l app.kubernetes.io/managed-by=etcd-druid -o name | head -1)
# args now https + cert paths; CA + client-tls mounted; no TLS errors
kubectl get "$POD" -o jsonpath='{range .spec.containers[?(@.name=="metrics-exporter")]}{.args}{"\n"}{end}'
kubectl describe "$POD" | grep -iE 'etcd-ca|etcd-client-tls'
kubectl logs "$POD" -c metrics-exporter | grep -iE 'tls|x509|handshake|error' || echo "no TLS errors"
kubectl port-forward "$POD" 9096:9096 & sleep 2; curl -s localhost:9096/metrics | grep etcddruid_exporter_
```

## Phase 4 — Scrape + dashboard
```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts && helm repo update
helm install kube-prom prometheus-community/kube-prometheus-stack -n monitoring --create-namespace
kubectl -n monitoring rollout status deploy/kube-prom-grafana

# add `release: kube-prom` to the PodMonitor's metadata.labels so the stack selects it;
# replace REPLACE_WITH_ETCD_CA_SECRET for the TLS etcd (2379) endpoint before applying.
kubectl apply -f docs/monitoring/etcd-metrics-exporter-podmonitor.yaml

kubectl -n monitoring port-forward svc/kube-prom-kube-prometheus-prometheus 9090:9090 & sleep 2
curl -s 'http://localhost:9090/api/v1/query?query=etcddruid_exporter_resource_events_total' | jq '.data.result|length'
```
Import `docs/monitoring/etcd-metrics-exporter-dashboard.json` into Grafana
(`kubectl -n monitoring port-forward svc/kube-prom-grafana 3000:80`; admin pw:
`kubectl -n monitoring get secret kube-prom-grafana -o jsonpath='{.data.admin-password}'|base64 -d`).
Confirm: defrag % (gauge+timeseries), quota configured-vs-runtime, **Quota Drift shows
a value not "No data"** (validates the `on(namespace,pod)` fix), churn timeseries + top-N.

## Teardown
```bash
kubectl delete -f examples/etcd/druid_v1alpha1_etcd.yaml
helm -n monitoring uninstall kube-prom || true
make undeploy && make kind-down
```

## Notes
- `IMAGEVECTOR_OVERWRITE` is patched in by hand (no chart value yet) — possible follow-up.
- Churn = write **activity** (watch events), not current key count; a quiet etcd shows
  low rates until you generate writes.
- Phase 4 needs a bring-your-own prometheus-operator + Grafana.

# Monitoring

etcd-druid uses [Prometheus](https://prometheus.io/) for metrics reporting. The metrics can be used for real-time monitoring and debugging of compaction jobs.

The simplest way to see the available metrics is to cURL the metrics endpoint `/metrics`. The format is described [here](http://prometheus.io/docs/instrumenting/exposition_formats/).

Follow the [Prometheus getting started doc](https://prometheus.io/docs/prometheus/latest/getting_started/) to spin up a Prometheus server to collect etcd metrics.

The naming of metrics follows the suggested [Prometheus best practices](https://prometheus.io/docs/practices/naming/). All compaction related metrics are put under namespace `etcddruid` and the respective subsystems.

## Snapshot Compaction

These metrics provide information about the compaction jobs that run after some interval in shoot control planes. Studying the metrics, we can deduce how many compaction job ran successfully, how many failed, how many delta events compacted etc.

| Name                                      | Description                                                         | Type      |
| ----------------------------------------- | ------------------------------------------------------------------- | --------- |
| etcddruid_compaction_jobs_total           | Total number of compaction jobs initiated by compaction controller. | Counter   |
| etcddruid_compaction_jobs_current         | Number of currently running compaction job.                         | Gauge     |
| etcddruid_compaction_job_duration_seconds | Total time taken in seconds to finish a running compaction job.     | Histogram |
| etcddruid_compaction_num_delta_events     | Total number of etcd events to be compacted by a compaction job.    | Gauge     |

There are two labels for `etcddruid_compaction_jobs_total` metrics. The label `succeeded` shows how many of the compaction jobs are succeeded and label `failed` shows how many of compaction jobs are failed.

There are two labels for `etcddruid_compaction_job_duration_seconds` metrics. The label `succeeded` shows how much time taken by a successful job to complete and label `failed` shows how much time taken by a failed compaction job.

`etcddruid_compaction_jobs_current` metric comes with label `etcd_namespace` that indicates the namespace of the Etcd running in the control plane of a shoot cluster..


## Etcd

These metrics are exposed by the [etcd](https://etcd.io/) process that runs in each etcd pod.

The following list metrics is applicable to clustering of a multi-node etcd cluster. The full list of metrics exposed by `etcd` is available [here](https://etcd.io/docs/v3.4/metrics).

| No. | Metrics Name                                  | Description                                                                                      | Comments                                                                                                                       |
| --- | --------------------------------------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| 1   | etcd_disk_wal_fsync_duration_seconds          | latency distributions of fsync called by WAL.                                                    | High disk operation latencies indicate disk issues.                                                                            |
| 2   | etcd_disk_backend_commit_duration_seconds     | latency distributions of commit called by backend.                                               | High disk operation latencies indicate disk issues.                                                                            |
| 3   | etcd_server_has_leader                        | whether or not a leader exists. 1: leader exists, 0: leader not exists.                          | To capture quorum loss or to check the availability of etcd cluster.                                                           |
| 4   | etcd_server_is_leader                         | whether or not this member is a leader. 1 if it is, 0 otherwise.                                 |                                                                                                                                |
| 5   | etcd_server_leader_changes_seen_total         | number of leader changes seen.                                                                   | Helpful in fine tuning the zonal cluster like etcd-heartbeat time etc, it can also indicates the etcd load and network issues. |
| 6   | etcd_server_is_learner                        | whether or not this member is a learner. 1 if it is, 0 otherwise.                                |                                                                                                                                |
| 7   | etcd_server_learner_promote_successes         | total number of successful learner promotions while this member is leader.                       | Might be helpful in checking the success of API calls called by backup-restore.                                                |
| 8   | etcd_network_client_grpc_received_bytes_total | total number of bytes received from grpc clients.                                                | Client Traffic In.                                                                                                             |
| 9   | etcd_network_client_grpc_sent_bytes_total     | total number of bytes sent to grpc clients.                                                      | Client Traffic Out.                                                                                                            |
| 10  | etcd_network_peer_sent_bytes_total            | total number of bytes sent to peers.                                                             | Useful for network usage.                                                                                                      |
| 11  | etcd_network_peer_received_bytes_total        | total number of bytes received from peers.                                                       | Useful for network usage.                                                                                                      |
| 12  | etcd_network_active_peers                     | current number of active peer connections.                                                       | Might be useful in detecting issues like network partition.                                                                    |
| 13  | etcd_server_proposals_committed_total         | total number of consensus proposals committed.                                                   | A consistently large lag between a single member and its leader indicates that member is slow or unhealthy.                    |
| 14  | etcd_server_proposals_pending                 | current number of pending proposals to commit.                                                   | Pending proposals suggests there is a high client load or the member cannot commit proposals.                                  |
| 15  | etcd_server_proposals_failed_total            | total number of failed proposals seen.                                                           | Might indicates downtime caused by a loss of quorum.                                                                           |
| 16  | etcd_server_proposals_applied_total           | total number of consensus proposals applied.                                                     | Difference between etcd_server_proposals_committed_total and etcd_server_proposals_applied_total should usually be small.      |
| 17  | etcd_mvcc_db_total_size_in_bytes              | total size of the underlying database physically allocated in bytes.                             |                                                                                                                                |
| 18  | etcd_server_heartbeat_send_failures_total     | total number of leader heartbeat send failures.                                                  | Might be helpful in fine-tuning the cluster or detecting slow disk or any network issues.                                      |
| 19  | etcd_network_peer_round_trip_time_seconds     | round-trip-time histogram between peers.                                                         | Might be helpful in fine-tuning network usage specially for zonal etcd cluster.                                                |
| 20  | etcd_server_slow_apply_total                  | total number of slow apply requests.                                                             | Might indicate overloaded from slow disk.                                                                                      |
| 21  | etcd_server_slow_read_indexes_total           | total number of pending read indexes not in sync with leader's or timed out read index requests. |                                                                                                                                |

The full list of metrics is available [here](https://etcd.io/docs/v3.4/metrics/).

## Etcd-Backup-Restore

These metrics are exposed by the [etcd-backup-restore](https://github.com/gardener/etcd-backup-restore) container in each etcd pod.

The following list metrics is applicable to clustering of a multi-node etcd cluster. The full list of metrics exposed by `etcd-backup-restore` is available [here](https://github.com/gardener/etcd-backup-restore/blob/master/docs/operations/metrics.md).

| No. | Metrics Name                            | Description                                                                       |
| --- | --------------------------------------- | --------------------------------------------------------------------------------- |
| 1.  | etcdbr_cluster_size                     | to capture the scale-up/scale-down scenarios.                                     |
| 2.  | etcdbr_is_learner                       | whether or not this member is a learner. 1 if it is, 0 otherwise.                 |
| 3.  | etcdbr_is_learner_count_total           | total number times member added as the learner.                                   |
| 4.  | etcdbr_restoration_duration_seconds     | total latency distribution required to restore the etcd member.                   |
| 5.  | etcdbr_add_learner_duration_seconds     | total latency distribution of adding the etcd member as a learner to the cluster. |
| 6.  | etcdbr_member_remove_duration_seconds   | total latency distribution removing the etcd member from the cluster.             |
| 7.  | etcdbr_member_promote_duration_seconds  | total latency distribution of promoting the learner to the voting member.         |
| 8.  | etcdbr_defragmentation_duration_seconds | total latency distribution of defragmentation of each etcd cluster member.        |

## Etcd Metrics Exporter

These metrics are exposed by the [etcd-metrics-exporter](https://github.com/gardener/etcd-druid) sidecar container that runs in each etcd pod. The sidecar exists so that operators do not have to manually run `etcdctl endpoint status` / `etcdctl watch` against a live member to debug database size, configured quota, or event churn — it surfaces this information as scrapeable Prometheus metrics instead.

The sidecar listens on port `9096` and serves its metrics at `/metrics` over plain HTTP (no TLS).

| No. | Metrics Name                                      | Description                                                                                       |
| --- | ------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| 1.  | etcddruid_exporter_resource_events_total          | Total number of watch events observed, counted per resource and event type.                       |
| 2.  | etcddruid_exporter_configured_quota_backend_bytes | The quota-backend-bytes value configured in the etcd config file (the configured DB size limit).  |

`etcddruid_exporter_resource_events_total` is a counter and carries two labels: `resource` indicates the resource being watched and `event_type` indicates the kind of watch event observed. `etcddruid_exporter_configured_quota_backend_bytes` is a gauge and has no labels.

### Configured-vs-runtime quota and defragmentable space

`etcddruid_exporter_configured_quota_backend_bytes` reports the **configured** quota, i.e. the value read from the etcd configuration file. This is intentionally distinct from the **runtime** view, which comes from etcd's OWN native metrics on the client port (`2379`):

- `etcd_server_quota_backend_bytes` — the quota the running etcd process is actually enforcing.
- `etcd_mvcc_db_total_size_in_bytes` — total size physically allocated by the backend database.
- `etcd_mvcc_db_total_size_in_use_in_bytes` — size actually in use.

The **defragmentable space** is derived from etcd's native metrics as `etcd_mvcc_db_total_size_in_bytes - etcd_mvcc_db_total_size_in_use_in_bytes`. Exposing the configured quota alongside the runtime quota lets a dashboard highlight configured-vs-runtime drift (for example when the config file and the running process disagree).

### Scraping the metrics

Both the sidecar metrics (port `9096`, HTTP) and etcd's native metrics (port `2379`, HTTPS when client TLS is enabled) are reachable via the etcd client `Service`, which exposes a `metrics` port for the sidecar. etcd-druid does not ship or reconcile any monitoring custom resources — scraping is left to the monitoring platform. An example `PodMonitor` (prometheus-operator) that scrapes both endpoints is provided at [`etcd-metrics-exporter-podmonitor.yaml`](./etcd-metrics-exporter-podmonitor.yaml); it is an example only and is not applied by druid.

## Prometheus supplied metrics

The Prometheus client library provides a number of metrics under the `go` and `process` namespaces.

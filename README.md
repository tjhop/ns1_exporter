# NS1 Prometheus Exporter and HTTP Service Discovery Provider

Prometheus Exporter for NS1 DNS query statistics, exposed by the [NS1 API](https://ns1.com/api). In addition to Prometheus metrics, the exporter can also be configured to serve a [Prometheus HTTP Service Discovery Compatible](https://prometheus.io/docs/prometheus/latest/http_sd/#requirements-of-http-sd-endpoints) list of targets based on the DNS records associated with the zones on the account.

## Project Status and Functionality

_DISCLAIMER_: While I am currently employed by [NS1 (an IBM company)](https://ns1.com/), this project is *NOT* an official NS1 product, and is maintained on a best-effort basis.

Contributions are welcome!

Below is a list of various project milestones, of no particular organization:

| Feature/Thing | Status |
| --- | --- |
| Exporter | `Beta` (functional, stability unknown, subject to change) |
| HTTP SD | `$future` |
| `Makefile` | `$future` |
| Github actions for builds/release publishing | `$future` |
| Documentation for exporter | `$future` |
| Documentation for http sd | `$future` |
| Config examples for exporter | `$future` |
| Config examples for http sd | `$future` |

## Exporter

### Metrics

| Metric Name | Labels | Metric Type | Metric Help |
| --- | --- | --- | --- |
| `ns1_build_info` | [`build_date`, `commit`, `version`] | Gauge | "ns1_build_info NS1 exporter build information" |
| `ns1_stats_queries_per_second` | [`record_name`, `record_type`, `zone_name`] | Gauge | "ns1_stats_queries_per_second DNS queries per second for the labeled NS1 resource." |

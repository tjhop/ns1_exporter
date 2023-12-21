# NS1 Prometheus Exporter and HTTP Service Discovery Provider

[![license](https://img.shields.io/github/license/tjhop/ns1_exporter)](https://github.com/tjhop/ns1_exporter/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/tjhop/ns1_exporter)](https://goreportcard.com/report/github.com/tjhop/ns1_exporter)
[![golangci-lint](https://github.com/tjhop/ns1_exporter/actions/workflows/golangci-lint.yaml/badge.svg)](https://github.com/tjhop/ns1_exporter/actions/workflows/golangci-lint.yaml)
[![Latest Release](https://img.shields.io/github/v/release/tjhop/ns1_exporter)](https://github.com/tjhop/ns1_exporter/releases/latest)

Prometheus Exporter for NS1 DNS query statistics, exposed by the [NS1 API](https://ns1.com/api). In addition to Prometheus metrics, the exporter can also be configured to serve a [Prometheus HTTP Service Discovery Compatible](https://prometheus.io/docs/prometheus/latest/http_sd/#requirements-of-http-sd-endpoints) list of targets based on the DNS records associated with the zones on the account.

## Project Status and Functionality

_DISCLAIMER_: While I am currently employed by [NS1 (an IBM company)](https://ns1.com/), this project is *NOT* an official NS1 product, and is maintained on a best-effort basis.

Contributions are welcome! Commits should follow [conventional commits](https://www.conventionalcommits.org/en/v1.0.0/) syntax.

## Installation

### Docker

```shell
docker run -d -p 8080:8080 -e NS1_APIKEY="${NS1_APIKEY}" ghcr.io/tjhop/ns1_exporter <flags>
```

### Go
With a working `go` environemnt, the `ns1_exporter` can be installed like so:

```shell
go install github.com/tjhop/ns1_exporter@latest
NS1_APIKEY="<api-token>" /path/to/ns1_exporter <flags>
```

### Binary
Download a release appropriate for your system from the [Releases](https://github.com/tjhop/ns1_exporter/releases) page.

```shell
NS1_APIKEY="<api-token>" /path/to/ns1_exporter <flags>
```

### System Packages
Download a release appropriate for your system from the [Releases](https://github.com/tjhop/ns1_exporter/releases) page. A Systemd service file is included in the system packages that are built.

```shell
# install system package (example assuming Debian based)
apt install /path/to/package
# create unit override, add NS1_APIKEY environment variable and add any needed flags
systemctl edit ns1_exporter.service
systemctl enable ns1_exporter.service ; systemctl start ns1_exporter.service
```

_Note_: While packages are built for several systems, there are currently no plans to attempt to submit packages to upstream package repositories.

## Exporter

The primary purpose of the exporter is to expose NS1 DNS queries-per-second stats from the NS1 API.

An example Prometheus configuration file demonstrating how to scrape metrics can be found in [docs/examples/prometheus_ns1_metrics.yml](./docs/examples/prometheus_ns1_metrics.yml)

### Metrics

| Metric Name | Labels | Metric Type | Metric Help |
| --- | --- | --- | --- |
| `ns1_build_info` | [`build_date`, `commit`, `version`] | Gauge | "ns1_build_info NS1 exporter build information" |
| `ns1_api_failures` | [] | Counter | "Number of failed NS1 API calls." |
| `ns1_stats_queries_per_second` | [`record_name`, `record_type`, `zone_name`] | Gauge | "ns1_stats_queries_per_second DNS queries per second for the labeled NS1 resource." |

## HTTP Service Discovery

When enabled via the `--ns1.enable-service-discovery` flag, the exporter will also expose an HTTP endpoint `/sd` that can be used to output NS1 DNS records in a format that is compatible with [Prometheus's HTTP service discovery](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_sd_config). In order to be kind to NS1 API rate limits, the SD mechanism will update every 5 minutes and cache scrape targets. To override the default SD refresh interval, use the `--ns1.sd-refresh-interval` flag.

> :warning: _NOTE_: The NS1 API has an [account activity](https://ns1.com/api?docId=2285) endpoint that can be used to retrieve recent account activity (such as creating/modifying/deleting DNS records). However, the [ns1-go SDK](https://github.com/ns1/ns1-go) being used by this exporter does not currently support the account activity endpoint. This means that at each HTTP SD refresh interval, the exporter will do a full refresh of all DNS records available to the API token. If/when the go SDK adds support for the account activity endpoint, the HTTP SD mechanism will be updated to use a more intelligent refresh algorithm that polls the activity log on each refresh, only updating the scrape target cache when recent changes are detected to the account's DNS records.

Example HTTP SD entry for an `A` record pointing to a testing instance on Hetzner Cloud:

```shell
~/go/src/github.com/tjhop/ns1_exporter (main [ ]) -> curl -s localhost:8080/sd | jq '.[] | select(.labels.__meta_ns1_record_type=="A")'
{
  "targets": [
    "ns1_exporter.ns1.work.tjhop.io-A"
  ],
  "labels": {
    "__meta_ns1_record_answers": ",;id=657e72c79ac50c0001632390;rdata[|5.161.56.54|];meta[||];region_name=;,",
    "__meta_ns1_record_domain": "ns1_exporter.ns1.work.tjhop.io",
    "__meta_ns1_record_filters": ",,",
    "__meta_ns1_record_id": "657e72c76ec4b20001d2fbb9",
    "__meta_ns1_record_link": "",
    "__meta_ns1_record_meta": ",meta[;;],",
    "__meta_ns1_record_override_address_records_enabled": "false",
    "__meta_ns1_record_override_ttl_enabled": "false",
    "__meta_ns1_record_regions": "",
    "__meta_ns1_record_ttl": "3600",
    "__meta_ns1_record_type": "A",
    "__meta_ns1_record_use_client_subnet_enabled": "true",
    "__meta_ns1_record_zone": "ns1.work.tjhop.io"
  }
}
```

An example Prometheus configuration file demonstrating HTTP SD can be found in [docs/examples/prometheus_ns1_http_sd.yml](./docs/examples/prometheus_ns1_http_sd.yml)

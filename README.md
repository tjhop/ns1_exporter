# NS1 Prometheus Exporter and HTTP Service Discovery Provider

[![license](https://img.shields.io/github/license/tjhop/ns1_exporter)](https://github.com/tjhop/ns1_exporter/blob/master/LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/tjhop/ns1_exporter)](https://goreportcard.com/report/github.com/tjhop/ns1_exporter)
[![golangci-lint](https://github.com/tjhop/ns1_exporter/actions/workflows/golangci-lint.yaml/badge.svg)](https://github.com/tjhop/ns1_exporter/actions/workflows/golangci-lint.yaml)
[![Latest Release](https://img.shields.io/github/v/release/tjhop/ns1_exporter)](https://github.com/tjhop/ns1_exporter/releases/latest)
[![GitHub Downloads (all assets, all releases)](https://img.shields.io/github/downloads/tjhop/ns1_exporter/total)](https://github.com/tjhop/ns1_exporter/releases/latest)

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

_NOTE_: Installing via this method will result in a build without embedded metadata for version/build info. If you wish to fully recreate a release build as this project does, you will need to clone the project and use [goreleaser](https://goreleaser.com/) to make a build:

```shell
git clone https://github.com/tjhop/ns1_exporter.git
cd ns1_exporter
make build
NS1_APIKEY="<api-token>" ./dist/ns1_exporter_linux_amd64_v1/ns1_exporter <flags>
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

> :warning: _NOTE_: The queries-per-second statistics available from the NS1 API are not real-time, but are time delayed. This means that the metric values that are exposed to Prometheus are not real-time. This exporter makes no attempt to adjust metric timestamp to try and align the corresponding timestamp with the qps values.

An example Prometheus configuration file demonstrating how to scrape metrics can be found in [docs/examples/prometheus_ns1_metrics.yml](./docs/examples/prometheus_ns1_metrics.yml)

### Metrics

| Metric Name | Labels | Metric Type | Metric Help |
| --- | --- | --- | --- |
| `ns1_build_info` | [`build_date`, `commit`, `version`] | Gauge | "ns1_build_info NS1 exporter build information" |
| `ns1_api_failures_total` | [] | Counter | "Total number of failed NS1 API calls." |
| `ns1_stats_queries_per_second` | [`record_name`, `record_type`, `zone_name`] | Gauge | "ns1_stats_queries_per_second DNS queries per second for the labeled NS1 resource." |

## HTTP Service Discovery

When enabled via the `--ns1.enable-service-discovery` flag, the exporter will also expose an HTTP endpoint `/sd` that can be used to output NS1 DNS records in a format that is compatible with [Prometheus's HTTP service discovery](https://prometheus.io/docs/prometheus/latest/configuration/configuration/#http_sd_config). In order to be kind to NS1 API rate limits, the SD mechanism will poll the `/account/activity` endpoint every 1 minute and check to see if any recent API actions have been performed that would affect the SD's cache; if recent account actions are detected, the SD mechanism will assume it's cache is invalid and refresh data to serve for targets. As a failsafe, the SD mechanism will allow a maximum of 10 "empty" responses from the account activity endpoint (meaning no activity since the last poll), at which point it will refresh it's cache regardless to ensure it's fresh. To override the default SD refresh interval, use the `--ns1.sd-refresh-interval` flag.

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

## Command Line Flags

The available command line flags are documented in the help flag:

```shell
~ -> ./dist/ns1_exporter_linux_amd64_v1/ns1_exporter -h
usage: ns1_exporter [<flags>]


Flags:
  -h, --[no-]help                Show context-sensitive help (also try
                                 --help-long and --help-man).
      --web.telemetry-path="/metrics"  
                                 Path under which to expose metrics.
      --web.service-discovery-path="/sd"  
                                 Path under which to expose targets for
                                 Prometheus HTTP service discovery.
      --web.max-requests=40      Maximum number of parallel scrape requests.
                                 Use 0 to disable.
      --ns1.concurrency=0        NS1 API request concurrency. Default
                                 (0) uses NS1 Go SDK sleep strategry.
                                 60 may be good balance between performance
                                 and reduced risk of HTTP 429, see
                                 https://pkg.go.dev/gopkg.in/ns1/ns1-go.v2/rest
                                 and exporter documentation for more
                                 information.
      --[no-]ns1.exporter-enable-record-qps  
                                 Whether or not to enable retrieving
                                 record-level QPS stats from the NS1 API
      --[no-]ns1.exporter-enable-zone-qps  
                                 Whether or not to enable retrieving zone-level
                                 QPS stats from the NS1 API (overridden by
                                 `--ns1.enable-record-qps`)
      --ns1.exporter-zone-blacklist=  
                                 A regular expression of zone(s) the exporter
                                 is not allowed to query qps stats for (takes
                                 precedence over --ns1.exporter-zone-whitelist)
      --ns1.exporter-zone-whitelist=  
                                 A regular expression of zone(s) the exporter is
                                 allowed to query qps stats for
      --[no-]ns1.enable-service-discovery  
                                 Whether or not to enable an HTTP endpoint
                                 to expose NS1 DNS records as HTTP service
                                 discovery targets
      --ns1.sd-refresh-interval=5m  
                                 The interval at which targets for Prometheus
                                 HTTP service discovery will be refreshed from
                                 the NS1 API
      --ns1.sd-zone-blacklist=   A regular expression of zone(s) that the
                                 service discovery mechanism will not
                                 provide targets for (takes precedence over
                                 --ns1.sd-zone-whitelist)
      --ns1.sd-zone-whitelist=   A regular expression of zone(s) that the
                                 service discovery mechanism will provide
                                 targets for
      --ns1.sd-record-type=      A regular expression of record types that
                                 the service discovery mechanism will provide
                                 targets for
      --runtime.gomaxprocs=1     The target number of CPUs Go will run on
                                 (GOMAXPROCS) ($GOMAXPROCS)
      --[no-]web.systemd-socket  Use systemd socket activation listeners instead
                                 of port listeners (Linux only).
      --web.listen-address=:8080 ...  
                                 Addresses on which to expose metrics and web
                                 interface. Repeatable for multiple addresses.
      --web.config.file=""       [EXPERIMENTAL] Path to configuration file
                                 that can enable TLS or authentication. See:
                                 https://github.com/prometheus/exporter-toolkit/blob/master/docs/web-configuration.md
      --log.level=info           Only log messages with the given severity or
                                 above. One of: [debug, info, warn, error]
      --log.format=logfmt        Output format of log messages. One of: [logfmt,
                                 json]
      --[no-]version             Show application version.
```

// Copyright 2023 TJ Hoplock
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package exporter

import (
	"fmt"
	"regexp"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	api "gopkg.in/ns1/ns1-go.v2/rest"

	"github.com/tjhop/ns1_exporter/internal/version"
	"github.com/tjhop/ns1_exporter/pkg/metrics"
	ns1_internal "github.com/tjhop/ns1_exporter/pkg/ns1"
)

// Worker is a struct containing configs needed to retrieve stats from NS1 API
// to expose as prometheus metrics. It implements the prometheus.Collector interface
type Worker struct {
	EnableZoneQPS   bool
	EnableRecordQPS bool
	ZoneBlacklist   *regexp.Regexp
	ZoneWhitelist   *regexp.Regexp

	logger    log.Logger
	client    *api.Client
	zoneCache map[string]*ns1_internal.Zone
	qpsCache  []*ns1_internal.QPS
}

// NewWorker creates a new Worker struct to collect data from the NS1 API
func NewWorker(logger log.Logger, client *api.Client, zoneEnabled, recordEnabled bool, blacklist, whitelist *regexp.Regexp) *Worker {
	worker := &Worker{
		EnableZoneQPS:   zoneEnabled,
		EnableRecordQPS: recordEnabled,
		ZoneBlacklist:   blacklist,
		ZoneWhitelist:   whitelist,
		client:          client,
		logger:          log.With(logger, "worker", "exporter"),
	}

	// register exporter worker for metrics collection
	metrics.Registry.MustRegister(worker)

	return worker
}

// Describe implements the prometheus.Collector interface
func (w *Worker) Describe(ch chan<- *prometheus.Desc) {
	ch <- metrics.MetricBuildInfoDesc
	ch <- metrics.MetricQPSDesc
}

// Collect implements the prometheus.Collector interface
func (w *Worker) Collect(ch chan<- prometheus.Metric) {
	// write build info
	ch <- prometheus.MustNewConstMetric(
		metrics.MetricBuildInfoDesc, prometheus.GaugeValue, 1, version.Version, version.BuildDate, version.Commit,
	)

	// qps metrics
	for _, qps := range w.qpsCache {
		ch <- prometheus.MustNewConstMetric(
			metrics.MetricQPSDesc, prometheus.GaugeValue, float64(qps.Value), qps.ZoneName, qps.RecordName, qps.RecordType,
		)
	}
}

// RefreshZoneData updates the data for each of the zones in the worker's zone list by querying the NS1 API, parses the data to structs that serve as internal counterparts to the NS1 API's dns.Record and dns.Zone, and then updating the worker's internal map of zones. This internal map is used as a cache to respond to respond to HTTP requests.
func (w *Worker) RefreshZoneData() {
	getRecords := w.EnableRecordQPS || w.EnableZoneQPS
	w.zoneCache = ns1_internal.RefreshZoneData(w.logger, w.client, getRecords, w.ZoneBlacklist, w.ZoneWhitelist)
	level.Debug(w.logger).Log("msg", "Worker zone cache updated", "num_zones", len(w.zoneCache))

	if getRecords {
		for k, v := range w.zoneCache {
			level.Debug(w.logger).Log("msg", "Worker zone record count", "zone", k, "num_records", len(v.Records))
		}
	}
}

// RefreshQPSData refreshes the worker's `[]*ns1_internal.QPS` cache array by using the zone/record information present in the worker's `map[string]*ns1_internal.Zone` cache map. This function dispatches the work of making the API calls/updating the cache to either `Worker.RefreshQPSRecordData()`, `Worker.RefreshQPSZoneData()`, or `Worker.RefreshQPSAccountData()` as needed, depending on the flags provided to the service.
func (w *Worker) RefreshQPSData() {
	// if enabled at record level monitoring, only make record-level qps
	// calls. zone/account level stats can be calculated at query time, and
	// it'll save API calls.
	if w.EnableRecordQPS {
		w.RefreshQPSRecordData()
		return
	}

	// similar reasoning if enabled at zone level monitoring
	if w.EnableZoneQPS {
		w.RefreshQPSZoneData()
		return
	}

	// otherwise, just grab account level stats
	w.RefreshQPSAccountData()
}

// RefreshQPSAccountData refreshes the worker's `[]*ns1_internal.QPS` cache array by requesting account-level QPS stats from the NS1 API.
func (w *Worker) RefreshQPSAccountData() {
	cache := make([]*ns1_internal.QPS, 1)

	level.Debug(w.logger).Log("msg", "Refreshing account-level qps data from NS1 API")
	qpsRaw, _, err := w.client.Stats.GetQPS()
	if err != nil {
		level.Error(w.logger).Log("msg", "Failed to get account-level qps data from NS1 API", "err", err.Error())
		metrics.MetricExporterNS1APIFailures.Inc()
	}

	cache[0] = &ns1_internal.QPS{
		Value: qpsRaw,
	}
	w.qpsCache = cache
	level.Debug(w.logger).Log("msg", "Worker QPS cache updated", "qps_level", "account")
}

// RefreshQPSZoneData refreshes the worker's `[]*ns1_internal.QPS` cache array by using the zone/record information present in the worker's `map[string]*ns1_internal.Zone` cache map.
func (w *Worker) RefreshQPSZoneData() {
	var cache []*ns1_internal.QPS

	for zName := range w.zoneCache {
		level.Debug(w.logger).Log("msg", "Refreshing zone-level qps data from NS1 API", "zone_name", zName)
		zoneQPSRaw, _, err := w.client.Stats.GetZoneQPS(zName)
		if err != nil {
			level.Error(w.logger).Log("msg", "Failed to get zone-level qps data from NS1 API", "err", err.Error(), "zone_name", zName)
			metrics.MetricExporterNS1APIFailures.Inc()
		}

		cache = append(cache, &ns1_internal.QPS{
			Value:    zoneQPSRaw,
			ZoneName: zName,
		})
		level.Debug(w.logger).Log("msg", "Worker QPS cache updated", "qps_level", "zone", "zone", zName)
	}
	w.qpsCache = cache
}

// RefreshQPSRecordData refreshes the worker's `[]*ns1_internal.QPS` cache array by using the zone/record information present in the worker's `map[string]*ns1_internal.Zone` cache map.
func (w *Worker) RefreshQPSRecordData() {
	// check total number of records in zone cache prior to launching requests
	var numRecords int
	for _, z := range w.zoneCache {
		numRecords += len(z.Records)
	}
	level.Debug(w.logger).Log("msg", "updating worker qps cache", "zone_count", len(w.zoneCache), "record_count", fmt.Sprintf("%d", numRecords))

	var cache []*ns1_internal.QPS

	for zName, zData := range w.zoneCache {
		for _, r := range zData.Records {
			level.Debug(w.logger).Log("msg", "Refreshing record-level qps data from NS1 API", "zone_name", zName, "record_domain", r.Domain, "record_type", r.Type)
			recordQPSRaw, _, err := w.client.Stats.GetRecordQPS(zName, r.Domain, r.Type)
			if err != nil {
				level.Error(w.logger).Log("msg", "Failed to get record-level qps data for from NS1 API", "err", err.Error(), "zone_name", zName, "record_name", r.Domain, "record_type", r.Type)
				metrics.MetricExporterNS1APIFailures.Inc()
			}

			cache = append(cache, &ns1_internal.QPS{
				Value:      recordQPSRaw,
				ZoneName:   zName,
				RecordName: r.Domain,
				RecordType: r.Type,
			})
		}
		level.Debug(w.logger).Log("msg", "Worker QPS cache updated", "qps_level", "zone", "zone", zName, "num_records", len(zData.Records))
	}
	w.qpsCache = cache
}

// Refresh calls the other Refresh* functions as needed to update the worker's data from the NS1 API.
func (w *Worker) Refresh() {
	level.Info(w.logger).Log("msg", "Updating zone data from NS1 API")
	w.RefreshZoneData()
	level.Info(w.logger).Log("msg", "Updating QPS data from NS1 API")
	w.RefreshQPSData()
}

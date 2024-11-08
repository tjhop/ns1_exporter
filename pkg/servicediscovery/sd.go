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

package servicediscovery

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	promModel "github.com/prometheus/common/model"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/account"
	"gopkg.in/ns1/ns1-go.v2/rest/model/data"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
	"gopkg.in/ns1/ns1-go.v2/rest/model/filter"

	"github.com/tjhop/ns1_exporter/pkg/metrics"
	ns1_internal "github.com/tjhop/ns1_exporter/pkg/ns1"
)

const (
	ns1Label                             = promModel.MetaLabelPrefix + "ns1_"
	ns1RecordLabelAnswers                = ns1Label + "record_answers"
	ns1RecordLabelDomain                 = ns1Label + "record_domain"
	ns1RecordLabelFilters                = ns1Label + "record_filters"
	ns1RecordLabelID                     = ns1Label + "record_id"
	ns1RecordLabelLink                   = ns1Label + "record_link"
	ns1RecordLabelMeta                   = ns1Label + "record_meta"
	ns1RecordLabelOverrideAddressRecords = ns1Label + "record_override_address_records_enabled"
	ns1RecordLabelOverrideTTL            = ns1Label + "record_override_ttl_enabled"
	ns1RecordLabelRegions                = ns1Label + "record_regions"
	ns1RecordLabelTTL                    = ns1Label + "record_ttl"
	ns1RecordLabelType                   = ns1Label + "record_type"
	ns1RecordLabelUseClientSubnet        = ns1Label + "record_use_client_subnet_enabled"
	ns1RecordLabelZone                   = ns1Label + "record_zone"
)

type HTTPSDTarget struct {
	Targets []string           `json:"targets"`
	Labels  promModel.LabelSet `json:"labels"`
}

// Worker contains an API client to interact with the NS1 api, as well as a
// cache of DNS records and the Prometheus targets created from those records.
// Worker gets registered on a different handler for the `/sd` path and run via
// the same HTTP server as the metrics exporter.
type Worker struct {
	ZoneBlacklist       *regexp.Regexp
	ZoneWhitelist       *regexp.Regexp
	RecordTypeWhitelist *regexp.Regexp

	logger               *slog.Logger
	client               *api.Client
	zoneCache            map[string]*ns1_internal.Zone
	recordCache          []*dns.Record
	targetCache          []*HTTPSDTarget
	lastRefreshTimestamp time.Time
	pollCount            int
}

func NewWorker(logger *slog.Logger, client *api.Client, blacklist, whitelist, recordType *regexp.Regexp) *Worker {
	worker := Worker{
		client:              client,
		ZoneBlacklist:       blacklist,
		ZoneWhitelist:       whitelist,
		RecordTypeWhitelist: recordType,
		logger:              logger.With("worker", "http_sd"),
	}

	return &worker
}

func metaAsPrometheusMetaLabel(meta *data.Meta, innerDelim, outerDelim string) string {
	if meta == nil {
		return fmt.Sprintf("meta[%s%s]%s", innerDelim, innerDelim, outerDelim)
	}

	metaMap := meta.StringMap()
	if len(metaMap) == 0 {
		return fmt.Sprintf("meta[%s%s]%s", innerDelim, innerDelim, outerDelim)
	}

	var builder strings.Builder

	fmt.Fprintf(&builder, "meta[%s", innerDelim)

	metaMapKeys := getMapKeys(metaMap)
	sort.Strings(metaMapKeys)
	for _, key := range metaMapKeys {
		fmt.Fprintf(&builder, "%s=%v%s", key, metaMap[key], innerDelim)
	}

	fmt.Fprintf(&builder, "]%s", outerDelim)

	return builder.String()
}

func answerRdataAsPrometheuaMetaLabel(rdata []string, innerDelim, outerDelim string) string {
	if len(rdata) == 0 {
		return fmt.Sprintf("rdata[%s%s]%s", innerDelim, innerDelim, outerDelim)
	}

	var builder strings.Builder

	fmt.Fprintf(&builder, "rdata[%s", innerDelim)
	for _, data := range rdata {
		fmt.Fprintf(&builder, "%s%s", data, innerDelim)
	}
	fmt.Fprintf(&builder, "]%s", outerDelim)

	return builder.String()
}

func recordFiltersAsPrometheusMetaLabel(filters []*filter.Filter) string {
	if len(filters) == 0 {
		return ",,"
	}

	var builder strings.Builder
	builder.WriteString(",")
	for _, filter := range filters {
		fmt.Fprintf(&builder, ";type=%s;", filter.Type)
		fmt.Fprintf(&builder, "disabled=%t;", filter.Disabled)

		if filter.Config == nil {
			builder.WriteString("config[||];,")
			continue
		}

		if len(filter.Config) == 0 {
			builder.WriteString("config[||];,")
			continue
		}

		builder.WriteString("config[|")
		configKeys := getMapKeys(filter.Config)
		sort.Strings(configKeys)
		for _, key := range configKeys {
			fmt.Fprintf(&builder, "%s=%v|", key, filter.Config[key])
		}
		builder.WriteString("];,")
	}

	return builder.String()
}

func recordAsPrometheusTarget(record *dns.Record) *HTTPSDTarget {
	var answers, regions strings.Builder

	// format answers and associated metadata as meta label
	answers.WriteString(",")
	for _, answer := range record.Answers {
		fmt.Fprintf(&answers, ";id=%s;", answer.ID)
		answers.WriteString(answerRdataAsPrometheuaMetaLabel(answer.Rdata, "|", ";"))
		answers.WriteString(metaAsPrometheusMetaLabel(answer.Meta, "|", ";"))
		fmt.Fprintf(&answers, "region_name=%s;", answer.RegionName)
		answers.WriteString(",")
	}

	// format regions as meta label
	var recordRegionsMetaLabel string
	switch record.Regions {
	case nil:
		recordRegionsMetaLabel = ",,"
	default:
		regions.WriteString(",")
		for k, v := range record.Regions {
			fmt.Fprintf(&regions, "%s=%s", k, metaAsPrometheusMetaLabel(&v.Meta, ";", ","))
		}
	}

	overrideAddr := "false"
	if record.OverrideAddressRecords != nil {
		overrideAddr = strconv.FormatBool(*record.OverrideAddressRecords)
	}
	overrideTTL := "false"
	if record.OverrideTTL != nil {
		overrideTTL = strconv.FormatBool(*record.OverrideTTL)
	}
	useClientSubnet := "false"
	if record.UseClientSubnet != nil {
		useClientSubnet = strconv.FormatBool(*record.UseClientSubnet)
	}

	labels := promModel.LabelSet{
		ns1RecordLabelAnswers:                promModel.LabelValue(answers.String()),
		ns1RecordLabelDomain:                 promModel.LabelValue(record.Domain),
		ns1RecordLabelFilters:                promModel.LabelValue(recordFiltersAsPrometheusMetaLabel(record.Filters)),
		ns1RecordLabelID:                     promModel.LabelValue(record.ID),
		ns1RecordLabelLink:                   promModel.LabelValue(record.Link),
		ns1RecordLabelMeta:                   promModel.LabelValue("," + metaAsPrometheusMetaLabel(record.Meta, ";", ",")),
		ns1RecordLabelOverrideAddressRecords: promModel.LabelValue(overrideAddr),
		ns1RecordLabelOverrideTTL:            promModel.LabelValue(overrideTTL),
		ns1RecordLabelRegions:                promModel.LabelValue(recordRegionsMetaLabel),
		ns1RecordLabelTTL:                    promModel.LabelValue(fmt.Sprintf("%d", record.TTL)),
		ns1RecordLabelType:                   promModel.LabelValue(record.Type),
		ns1RecordLabelUseClientSubnet:        promModel.LabelValue(useClientSubnet),
		ns1RecordLabelZone:                   promModel.LabelValue(record.Zone),
	}

	target := HTTPSDTarget{
		Targets: []string{fmt.Sprintf("%s-%s", record.Domain, record.Type)},
		Labels:  labels,
	}

	return &target
}

func (w *Worker) RefreshPrometheusTargetData() {
	var data []*HTTPSDTarget

	for _, record := range w.recordCache {
		data = append(data, recordAsPrometheusTarget(record))
	}

	w.targetCache = data
	w.logger.Debug("Worker Prometheus target group updated", "num_targets", len(w.targetCache))
}

func (w *Worker) RefreshZoneData() {
	w.zoneCache = ns1_internal.RefreshZoneData(w.logger, w.client, true, w.ZoneBlacklist, w.ZoneWhitelist)
}

func (w *Worker) RefreshRecordData() {
	var records []*dns.Record

	for zName, zData := range w.zoneCache {
		// if record type regex is provided, filter records
		if w.RecordTypeWhitelist != nil && w.RecordTypeWhitelist.String() != "" {
			var filteredRecords []*ns1_internal.ZoneRecord
			for _, r := range zData.Records {
				if !w.RecordTypeWhitelist.MatchString(r.Type) {
					// if record type not in whitelist, log it and skip it
					w.logger.Debug("skipping record because it doesn't match whitelist regex", "record", r.Domain, "record_type_regex", w.RecordTypeWhitelist.String())
					continue
				}
				filteredRecords = append(filteredRecords, r)
			}

			zData.Records = filteredRecords
		}

		for _, r := range zData.Records {
			w.logger.Debug("Refreshing record data from NS1 API", "zone_name", zName, "record_domain", r.Domain, "record_type", r.Type)
			record, _, err := w.client.Records.Get(zData.Zone, r.Domain, r.Type)
			if err != nil {
				w.logger.Error("Failed to get record data from NS1 API", "err", err, "zone_name", zName, "record_domain", r.Domain, "record_type", r.Type)
				metrics.MetricExporterNS1APIFailures.Inc()
				continue
			}
			records = append(records, record)
		}
	}

	w.recordCache = records
	w.logger.Debug("Worker record cache updated", "num_records", len(w.recordCache))
}

func (w *Worker) RefreshData() {
	w.logger.Info("Updating record data from NS1 API")
	w.RefreshZoneData()
	w.RefreshRecordData()
	w.logger.Info("Updating prometheus target data from cached record data")
	w.RefreshPrometheusTargetData()
}

func (w *Worker) Refresh() {
	needsRefresh := true
	ts := time.Now().UTC()

	// if we already have data, we need to poll for activity and see if we should still refresh our data set or skip
	if w.recordCache != nil {
		params := []api.Param{
			{Key: "start", Value: strconv.FormatInt(w.lastRefreshTimestamp.Unix(), 10)},
			{Key: "limit", Value: "1000"},
		}
		w.logger.Debug("Refreshing account activity from NS1 API")
		activity, _, err := w.client.Activity.List(params...)
		if err != nil {
			w.logger.Error("Failed to get account activity from NS1 API", "err", err)
			metrics.MetricExporterNS1APIFailures.Inc()
		}
		w.pollCount++

		switch len(activity) {
		case 0:
			if w.pollCount < 10 {
				needsRefresh = false
			}
		default:
			// activity detected, filter to only care about activity that can affect zones/records, that's what we care about
			var filteredActivity []*account.Activity
			for _, a := range activity {
				switch a.ResourceType {
				case "dns_zone", "record", "notify_list", "datasource", "datafeed", "job":
					filteredActivity = append(filteredActivity, a)
				default:
				}
			}

			if len(filteredActivity) == 0 && w.pollCount < 10 {
				needsRefresh = false
			}
		}
	}

	if needsRefresh {
		w.RefreshData()
		w.pollCount = 0
	}

	w.lastRefreshTimestamp = ts
}

func (w *Worker) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	buf, err := json.MarshalIndent(w.targetCache, "", "    ")
	if err != nil {
		w.logger.Error("Failed to convert DNS records from NS1 API into Prometheus Targets", "err", err)
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if bytesWritten, err := writer.Write(buf); err != nil {
		w.logger.Error("Failed to write full HTTP response", "err", err, "bytes", bytesWritten)
	}
}

func getMapKeys(m map[string]any) []string {
	keys := make([]string, len(m))

	i := 0
	for key := range m {
		keys[i] = key
		i++
	}

	return keys
}

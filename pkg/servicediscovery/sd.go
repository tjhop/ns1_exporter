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
	"net/http"
	"strconv"
	"strings"

	"github.com/go-kit/log/level"
	promModel "github.com/prometheus/common/model"
	"github.com/prometheus/common/promlog"
	"gopkg.in/ns1/ns1-go.v2/rest/model/data"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
	"gopkg.in/ns1/ns1-go.v2/rest/model/filter"

	"github.com/tjhop/ns1_exporter/pkg/metrics"
	ns1_internal "github.com/tjhop/ns1_exporter/pkg/ns1"
)

var (
	logger = promlog.New(&promlog.Config{})
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
	client *ns1_internal.Client

	recordCache []*dns.Record
	targetCache []*HTTPSDTarget
}

func NewWorker(client *ns1_internal.Client) *Worker {
	worker := Worker{
		client: client,
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

	for metaField, val := range metaMap {
		fmt.Fprintf(&builder, "%s=%s%s", metaField, val, innerDelim)
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

func recordFiltersAsPrometheusMetaLabel(filters []*filter.Filter, delimiter string) string {
	if len(filters) == 0 {
		return ",,"
	}

	var builder strings.Builder
	builder.WriteString(",")
	for _, filter := range filters {
		fmt.Fprintf(&builder, ";type=%s;", filter.Type)
		fmt.Fprintf(&builder, "disabled=%t;", filter.Disabled)

		switch filter.Config {
		case nil:
			builder.WriteString("config[||]")
		default:
			builder.WriteString("config[|")
			for k, v := range filter.Config {
				fmt.Fprintf(&builder, "%s=%v;", k, v)
			}
			builder.WriteString("];")

		}
		builder.WriteString(",")
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
		ns1RecordLabelFilters:                promModel.LabelValue(recordFiltersAsPrometheusMetaLabel(record.Filters, ",")),
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
	level.Debug(logger).Log("msg", "Worker record cache updated", "worker", "http_sd", "num_records", len(w.targetCache))
}

func (w *Worker) RefreshRecordData() {
	var records []*dns.Record

	zoneData := w.client.RefreshZoneData(true)
	for zName, zData := range zoneData {
		for _, zRecord := range zData.Records {
			record, _, err := w.client.Records.Get(zData.Zone, zRecord.Domain, zRecord.Type)
			if err != nil {
				level.Error(logger).Log("msg", "Failed to get record data from NS1 API", "err", err.Error(), "worker", "http_sd", "zone_name", zName, "record_domain", zRecord.Domain, "record_type", zRecord.Type)
				metrics.MetricExporterNS1APIFailures.Inc()
				continue
			}
			records = append(records, record)
		}
	}

	w.recordCache = records
	level.Debug(logger).Log("msg", "Worker record cache updated", "worker", "http_sd", "num_records", len(w.recordCache))
}

func (w *Worker) Refresh() {
	level.Info(logger).Log("msg", "Updating record data from NS1 API", "worker", "http_sd")
	w.RefreshRecordData()
	level.Info(logger).Log("msg", "Updating prometheus target data from cached record data", "worker", "http_sd")
	w.RefreshPrometheusTargetData()
}

func (w *Worker) ServeHTTP(writer http.ResponseWriter, req *http.Request) {
	buf, err := json.MarshalIndent(w.targetCache, "", "    ")
	if err != nil {
		level.Error(logger).Log("msg", "Failed to convert DNS records from NS1 API into Prometheus Targets", "err", err.Error(), "worker", "http_sd")
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}

	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)
	if bytesWritten, err := writer.Write(buf); err != nil {
		level.Error(logger).Log("msg", "Failed to write full HTTP response", "err", err.Error(), "worker", "http_sd", "bytes", bytesWritten)
	}
}

// Copyright 2024 TJ Hoplock
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
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	promModel "github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"gopkg.in/ns1/ns1-go.v2/mockns1"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"github.com/prometheus/common/promlog"
	"gopkg.in/ns1/ns1-go.v2/rest/model/data"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
	"gopkg.in/ns1/ns1-go.v2/rest/model/filter"

	ns1_internal "github.com/tjhop/ns1_exporter/pkg/ns1"
)

var (
	mockLogger = promlog.New(&promlog.Config{})

	mockZoneCache = map[string]*ns1_internal.Zone{
		"foo.bar": {Zone: "foo.bar", Records: []*ns1_internal.ZoneRecord{
			{Domain: "test.foo.bar", ShortAns: []string{"1.2.3.4", "5.6.7.8", "127.0.0.1"}, Type: "A"},
			{Domain: "test.foo.bar", ShortAns: []string{"dead::beef"}, Type: "AAAA"},
		}},
	}
	mockDnsRecordCache = []*dns.Record{
		{Meta: &data.Meta{Up: true},
			ID:      "mockARecordID",
			Zone:    "foo.bar",
			Domain:  "test.foo.bar",
			Type:    "A",
			Link:    "",
			TTL:     3600,
			Answers: []*dns.Answer{{ID: "mockARecordAnswerID", Meta: &data.Meta{Up: true}, Rdata: []string{"1.2.3.4", "5.6.7.8", "127.0.0.1"}}},
			Filters: []*filter.Filter{filter.NewUp()},
		},
		{ID: "mockAAAARecordID",
			Zone:    "foo.bar",
			Domain:  "test.foo.bar",
			Type:    "AAAA",
			Link:    "",
			TTL:     3600,
			Answers: []*dns.Answer{{ID: "mockAAAARecordAnswerID", Rdata: []string{"dead::beef"}}},
			Filters: []*filter.Filter{},
		},
	}
	mockSDTargetCache = []*HTTPSDTarget{
		{Targets: []string{"test.foo.bar-A"}, Labels: promModel.LabelSet{
			ns1RecordLabelAnswers:                promModel.LabelValue(",;id=mockARecordAnswerID;rdata[|1.2.3.4|5.6.7.8|127.0.0.1|];meta[|up=1|];region_name=;,"),
			ns1RecordLabelDomain:                 promModel.LabelValue("test.foo.bar"),
			ns1RecordLabelFilters:                promModel.LabelValue(",;type=up;disabled=false;config[||];,"),
			ns1RecordLabelID:                     promModel.LabelValue("mockARecordID"),
			ns1RecordLabelLink:                   promModel.LabelValue(""),
			ns1RecordLabelMeta:                   promModel.LabelValue(",meta[;up=1;],"),
			ns1RecordLabelOverrideAddressRecords: promModel.LabelValue("false"),
			ns1RecordLabelOverrideTTL:            promModel.LabelValue("false"),
			ns1RecordLabelRegions:                promModel.LabelValue(",,"),
			ns1RecordLabelTTL:                    promModel.LabelValue("3600"),
			ns1RecordLabelType:                   promModel.LabelValue("A"),
			ns1RecordLabelUseClientSubnet:        promModel.LabelValue("false"),
			ns1RecordLabelZone:                   promModel.LabelValue("foo.bar"),
		}},
		{Targets: []string{"test.foo.bar-AAAA"}, Labels: promModel.LabelSet{
			ns1RecordLabelAnswers:                promModel.LabelValue(",;id=mockAAAARecordAnswerID;rdata[|dead::beef|];meta[||];region_name=;,"),
			ns1RecordLabelDomain:                 promModel.LabelValue("test.foo.bar"),
			ns1RecordLabelFilters:                promModel.LabelValue(",,"),
			ns1RecordLabelID:                     promModel.LabelValue("mockAAAARecordID"),
			ns1RecordLabelLink:                   promModel.LabelValue(""),
			ns1RecordLabelMeta:                   promModel.LabelValue(",meta[;;],"),
			ns1RecordLabelOverrideAddressRecords: promModel.LabelValue("false"),
			ns1RecordLabelOverrideTTL:            promModel.LabelValue("false"),
			ns1RecordLabelRegions:                promModel.LabelValue(",,"),
			ns1RecordLabelTTL:                    promModel.LabelValue("3600"),
			ns1RecordLabelType:                   promModel.LabelValue("AAAA"),
			ns1RecordLabelUseClientSubnet:        promModel.LabelValue("false"),
			ns1RecordLabelZone:                   promModel.LabelValue("foo.bar"),
		}},
	}
	mockTargetJSON = []byte(`[
    {
        "targets": [
            "test.foo.bar-A"
        ],
        "labels": {
            "__meta_ns1_record_answers": ",;id=mockARecordAnswerID;rdata[|1.2.3.4|5.6.7.8|127.0.0.1|];meta[|up=1|];region_name=;,",
            "__meta_ns1_record_domain": "test.foo.bar",
            "__meta_ns1_record_filters": ",;type=up;disabled=false;config[||];,",
            "__meta_ns1_record_id": "mockARecordID",
            "__meta_ns1_record_link": "",
            "__meta_ns1_record_meta": ",meta[;up=1;],",
            "__meta_ns1_record_override_address_records_enabled": "false",
            "__meta_ns1_record_override_ttl_enabled": "false",
            "__meta_ns1_record_regions": ",,",
            "__meta_ns1_record_ttl": "3600",
            "__meta_ns1_record_type": "A",
            "__meta_ns1_record_use_client_subnet_enabled": "false",
            "__meta_ns1_record_zone": "foo.bar"
        }
    },
    {
        "targets": [
            "test.foo.bar-AAAA"
        ],
        "labels": {
            "__meta_ns1_record_answers": ",;id=mockAAAARecordAnswerID;rdata[|dead::beef|];meta[||];region_name=;,",
            "__meta_ns1_record_domain": "test.foo.bar",
            "__meta_ns1_record_filters": ",,",
            "__meta_ns1_record_id": "mockAAAARecordID",
            "__meta_ns1_record_link": "",
            "__meta_ns1_record_meta": ",meta[;;],",
            "__meta_ns1_record_override_address_records_enabled": "false",
            "__meta_ns1_record_override_ttl_enabled": "false",
            "__meta_ns1_record_regions": ",,",
            "__meta_ns1_record_ttl": "3600",
            "__meta_ns1_record_type": "AAAA",
            "__meta_ns1_record_use_client_subnet_enabled": "false",
            "__meta_ns1_record_zone": "foo.bar"
        }
    }
]`)
)

func TestMetaAsPrometheusMetaLabel(t *testing.T) {
	tests := map[string]struct {
		meta       *data.Meta
		innerDelim string
		outerDelim string
		want       string
	}{
		"nil":              {meta: nil, innerDelim: "|", outerDelim: ";", want: "meta[||];"},
		"empty_string_map": {meta: &data.Meta{}, innerDelim: ";", outerDelim: ",", want: "meta[;;],"},
		// NOTE: the ns1-go (data.Meta).StringMap() method coerces boolean types to integer values:
		// https://github.com/ns1/ns1-go/blob/3520e92b5de3394dd10694e00c530c29bd51b37d/rest/model/data/meta.go#L173
		// https://github.com/ns1/ns1-go/blob/3520e92b5de3394dd10694e00c530c29bd51b37d/rest/model/data/meta.go#L183-L187
		"some_meta": {meta: &data.Meta{Weight: 50, Priority: 1, Up: true}, innerDelim: "|", outerDelim: ";", want: "meta[|priority=1|up=1|weight=50|];"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := metaAsPrometheusMetaLabel(tc.meta, tc.innerDelim, tc.outerDelim)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestAnswerRdataAsPrometheuaMetaLabel(t *testing.T) {
	tests := map[string]struct {
		rdata      []string
		innerDelim string
		outerDelim string
		want       string
	}{
		"empty":            {rdata: []string{}, innerDelim: "|", outerDelim: ";", want: "rdata[||];"},
		"one_answer":       {rdata: []string{"1.2.3.4"}, innerDelim: "|", outerDelim: ";", want: "rdata[|1.2.3.4|];"},
		"multiple_answers": {rdata: []string{"1.2.3.4", "5.6.7.8", "127.0.0.1"}, innerDelim: "|", outerDelim: ";", want: "rdata[|1.2.3.4|5.6.7.8|127.0.0.1|];"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := answerRdataAsPrometheuaMetaLabel(tc.rdata, tc.innerDelim, tc.outerDelim)

			require.Equal(t, tc.want, got)
		})
	}
}

func TestRecordFiltersAsPrometheusMetaLabel(t *testing.T) {
	tests := map[string]struct {
		filters []*filter.Filter
		want    string
	}{
		"empty": {filters: []*filter.Filter{}, want: ",,"},
		"two_filters_empty_config": {filters: []*filter.Filter{
			{Type: "mock_one", Disabled: true, Config: filter.Config{}},
			{Type: "mock_two", Disabled: true, Config: filter.Config{}},
		}, want: ",;type=mock_one;disabled=true;config[||];,;type=mock_two;disabled=true;config[||];,"},
		"one_filter_with_config": {filters: []*filter.Filter{
			{Type: "mock_one", Disabled: true, Config: filter.Config{"MockString": "asdf", "MockInt": 5, "MockBool": false}},
		}, want: ",;type=mock_one;disabled=true;config[|MockBool=false|MockInt=5|MockString=asdf|];,"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := recordFiltersAsPrometheusMetaLabel(tc.filters)

			require.Equal(t, tc.want, got)
		})
	}
}

// func recordAsPrometheusTarget(record *dns.Record) *HTTPSDTarget {
func TestRecordAsPrometheusTarget(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.Nil(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	worker := NewWorker(mockLogger, mockClient, nil, nil, nil)

	tests := map[string]struct {
		recordCache []*dns.Record
		want        []*HTTPSDTarget
	}{
		"empty_record_cache": {recordCache: []*dns.Record{}, want: nil},
		"some_record_cache":  {recordCache: mockDnsRecordCache, want: mockSDTargetCache},
	}

	for name, tc := range tests {
		worker.recordCache = tc.recordCache

		t.Run(name, func(t *testing.T) {
			var got []*HTTPSDTarget
			for _, record := range tc.recordCache {
				got = append(got, recordAsPrometheusTarget(record))
			}

			require.Equal(t, tc.want, got)

			// clear test cases for next iteration
			mock.ClearTestCases()
		})
	}
}

func TestRefreshRecordData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.Nil(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	tests := map[string]struct {
		zoneCache           map[string]*ns1_internal.Zone
		recordTypeWhitelist *regexp.Regexp
		want                []*dns.Record
	}{
		"empty_zone_cache":   {zoneCache: map[string]*ns1_internal.Zone{}, recordTypeWhitelist: nil, want: nil},
		"some_zone_cache":    {zoneCache: mockZoneCache, recordTypeWhitelist: nil, want: mockDnsRecordCache},
		"record_type_filter": {zoneCache: mockZoneCache, recordTypeWhitelist: regexp.MustCompile("SRV|AAAA"), want: mockDnsRecordCache[1:]},
	}

	for name, tc := range tests {
		worker := NewWorker(mockLogger, mockClient, nil, nil, tc.recordTypeWhitelist)
		worker.zoneCache = tc.zoneCache

		t.Run(name, func(t *testing.T) {
			for _, record := range tc.want {
				require.Nil(t, mock.AddTestCase(http.MethodGet, fmt.Sprintf("zones/%s/%s/%s", record.Zone, record.Domain, record.Type),
					http.StatusOK, nil, nil, "", record),
				)
			}

			worker.RefreshRecordData()

			require.Equal(t, tc.want, worker.recordCache)

			// clear test cases for next iteration
			mock.ClearTestCases()
		})
	}
}

func TestRefreshPrometheusTargetData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.Nil(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	worker := NewWorker(mockLogger, mockClient, nil, nil, nil)

	tests := map[string]struct {
		recordCache []*dns.Record
		want        []*HTTPSDTarget
	}{
		"empty_record_cache": {recordCache: []*dns.Record{}, want: nil},
		"some_record_cache":  {recordCache: mockDnsRecordCache, want: mockSDTargetCache},
	}

	for name, tc := range tests {
		worker.recordCache = tc.recordCache

		t.Run(name, func(t *testing.T) {
			worker.RefreshPrometheusTargetData()

			require.Equal(t, tc.want, worker.targetCache)

			// clear test cases for next iteration
			mock.ClearTestCases()
		})
	}
}

func TestServeHTTP(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.Nil(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	ts := httptest.NewServer(http.DefaultServeMux)
	t.Cleanup(ts.Close)

	worker := NewWorker(mockLogger, mockClient, nil, nil, nil)
	http.Handle("/sd", worker)
	httpClient := http.Client{
		Timeout: 30 * time.Second,
	}

	tests := map[string]struct {
		targetCache []*HTTPSDTarget
		want        []byte
	}{
		"empty_target_cache": {targetCache: []*HTTPSDTarget{}, want: []byte("[]")},
		"some_target_cache":  {targetCache: mockSDTargetCache, want: mockTargetJSON},
	}

	for name, tc := range tests {
		worker.targetCache = tc.targetCache

		t.Run(name, func(t *testing.T) {
			url, err := url.JoinPath(ts.URL, "sd")
			require.NoError(t, err)
			req, err := http.NewRequest("GET", url, nil)
			require.NoError(t, err)

			req.Header.Set("Accept", "application/json")

			resp, err := httpClient.Do(req)
			require.NoError(t, err)

			defer func() {
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}()

			require.Equal(t, resp.StatusCode, http.StatusOK)

			got, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.True(t, bytes.Equal(tc.want, got))
			require.Equal(t, string(tc.want), string(got))
		})
	}
}

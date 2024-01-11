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
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/ns1/ns1-go.v2/mockns1"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/data"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
	"gopkg.in/ns1/ns1-go.v2/rest/model/filter"

	ns1_internal "github.com/tjhop/ns1_exporter/pkg/ns1"
)

var (
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
func TestRecordAsPrometheusTarget(t *testing.T) {}

// func (w *Worker) RefreshRecordData() {
func TestRefreshRecordData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.Nil(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	tests := map[string]struct {
		zoneCache map[string]*ns1_internal.Zone
		want      []*dns.Record
	}{
		"empty_zone_cache": {zoneCache: map[string]*ns1_internal.Zone{}, want: []*dns.Record{}},
		"some_zone_cache":  {zoneCache: mockZoneCache, want: mockDnsRecordCache},
	}

	for name, tc := range tests {
		worker := NewWorker(mockClient, nil, nil)
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

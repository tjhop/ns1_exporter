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
	"net/http"
	"net/url"
	"strings"
	"testing"

	prom_testutil "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/require"
	"gopkg.in/ns1/ns1-go.v2/mockns1"
	api "gopkg.in/ns1/ns1-go.v2/rest"

	"github.com/tjhop/ns1_exporter/pkg/metrics"
	ns1_internal "github.com/tjhop/ns1_exporter/pkg/ns1"
)

var (
	mockLogger = promslog.New(&promslog.Config{})

	mockZoneCache = map[string]*ns1_internal.Zone{
		"foo.bar": {Zone: "foo.bar", Records: []*ns1_internal.ZoneRecord{
			{Domain: "foo.bar", ShortAns: []string{"dns1.p01.nsone.net."}, Type: "NS"},
			{Domain: "test.foo.bar", ShortAns: []string{"1.2.3.4"}, Type: "A"},
			{Domain: "test.foo.bar", ShortAns: []string{"dead::beef"}, Type: "AAAA"},
		}},
		"keep.me": {Zone: "keep.me", Records: []*ns1_internal.ZoneRecord{
			{Domain: "keep.me", ShortAns: []string{"dns1.p01.nsone.net."}, Type: "NS"},
			{Domain: "test.keep.me", ShortAns: []string{"1.2.3.4"}, Type: "A"},
		}},
	}
)

func TestRefreshQPSAccountData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.NoError(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	tests := map[string]struct {
		want []*ns1_internal.QPS
	}{
		"account": {want: []*ns1_internal.QPS{
			{Value: float32(10000)},
		}},
	}

	accountQPSMetricsExpected := `
# HELP ns1_build_info NS1 exporter build information
# TYPE ns1_build_info gauge
ns1_build_info{build_date="",commit="",version=""} 1
# HELP ns1_stats_queries_per_second DNS queries per second for the labeled NS1 resource. Note that NS1 QPS metrics are time delayed, not real-time.
# TYPE ns1_stats_queries_per_second gauge
ns1_stats_queries_per_second{record_name="",record_type="",zone_name=""} 10000
`

	for name, tc := range tests {
		worker := NewWorker(mockLogger, mockClient, false, false, nil, nil)
		worker.zoneCache = mockZoneCache

		t.Run(name, func(t *testing.T) {
			for _, qps := range tc.want {
				require.NoError(t, mock.AddTestCase(http.MethodGet, "stats/qps", http.StatusOK, nil, nil, "",
					struct{ QPS float32 }{QPS: qps.Value}),
				)
			}

			worker.RefreshQPSAccountData()

			require.Len(t, tc.want, len(worker.qpsCache))
			require.NoError(t, prom_testutil.CollectAndCompare(worker, strings.NewReader(accountQPSMetricsExpected)))

			// clear test cases for next iteration
			mock.ClearTestCases()
		})

		// unregister worker to prevent duplicate metric collection issues in further test runs
		metrics.Registry.Unregister(worker)
	}
}

func TestRefreshQPSZoneData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.NoError(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	tests := map[string]struct {
		want []*ns1_internal.QPS
	}{
		"zone": {want: []*ns1_internal.QPS{
			{Value: float32(5000), ZoneName: "foo.bar"},
			{Value: float32(5000), ZoneName: "keep.me"},
		}},
	}

	zoneQPSMetricsExpected := `
# HELP ns1_build_info NS1 exporter build information
# TYPE ns1_build_info gauge
ns1_build_info{build_date="",commit="",version=""} 1
# HELP ns1_stats_queries_per_second DNS queries per second for the labeled NS1 resource. Note that NS1 QPS metrics are time delayed, not real-time.
# TYPE ns1_stats_queries_per_second gauge
ns1_stats_queries_per_second{record_name="",record_type="",zone_name="foo.bar"} 5000
ns1_stats_queries_per_second{record_name="",record_type="",zone_name="keep.me"} 5000
`

	for name, tc := range tests {
		worker := NewWorker(mockLogger, mockClient, true, false, nil, nil)
		worker.zoneCache = mockZoneCache

		t.Run(name, func(t *testing.T) {
			for _, qps := range tc.want {
				require.NoError(t, mock.AddTestCase(http.MethodGet, "stats/qps/"+qps.ZoneName, http.StatusOK, nil, nil, "",
					struct{ QPS float32 }{QPS: qps.Value}),
				)
			}

			worker.RefreshQPSZoneData()

			require.Len(t, tc.want, len(worker.qpsCache))
			require.NoError(t, prom_testutil.CollectAndCompare(worker, strings.NewReader(zoneQPSMetricsExpected)))

			// clear test cases for next iteration
			mock.ClearTestCases()
		})

		// unregister worker to prevent duplicate metric collection issues in further test runs
		metrics.Registry.Unregister(worker)
	}
}

func TestRefreshQPSRecordData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.NoError(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	tests := map[string]struct {
		want []*ns1_internal.QPS
	}{
		"record": {want: []*ns1_internal.QPS{
			{Value: float32(1000), ZoneName: "foo.bar", RecordName: "foo.bar", RecordType: "NS"},
			{Value: float32(2500), ZoneName: "foo.bar", RecordName: "test.foo.bar", RecordType: "A"},
			{Value: float32(2500), ZoneName: "foo.bar", RecordName: "test.foo.bar", RecordType: "AAAA"},
			{Value: float32(1000), ZoneName: "keep.me", RecordName: "keep.me", RecordType: "NS"},
			{Value: float32(2500), ZoneName: "keep.me", RecordName: "test.keep.me", RecordType: "A"},
		}},
	}

	recordQPSMetricsExpected := `
# HELP ns1_build_info NS1 exporter build information
# TYPE ns1_build_info gauge
ns1_build_info{build_date="",commit="",version=""} 1
# HELP ns1_stats_queries_per_second DNS queries per second for the labeled NS1 resource. Note that NS1 QPS metrics are time delayed, not real-time.
# TYPE ns1_stats_queries_per_second gauge
ns1_stats_queries_per_second{record_name="foo.bar",record_type="NS",zone_name="foo.bar"} 1000
ns1_stats_queries_per_second{record_name="test.foo.bar",record_type="A",zone_name="foo.bar"} 2500
ns1_stats_queries_per_second{record_name="test.foo.bar",record_type="AAAA",zone_name="foo.bar"} 2500
ns1_stats_queries_per_second{record_name="keep.me",record_type="NS",zone_name="keep.me"} 1000
ns1_stats_queries_per_second{record_name="test.keep.me",record_type="A",zone_name="keep.me"} 2500
`

	for name, tc := range tests {
		worker := NewWorker(mockLogger, mockClient, true, true, nil, nil)
		worker.zoneCache = mockZoneCache

		t.Run(name, func(t *testing.T) {
			for _, qps := range tc.want {
				require.NoError(t, mock.AddTestCase(http.MethodGet, fmt.Sprintf("stats/qps/%s/%s/%s", qps.ZoneName, qps.RecordName, qps.RecordType),
					http.StatusOK, nil, nil, "", struct{ QPS float32 }{QPS: qps.Value}),
				)
			}

			worker.RefreshQPSRecordData()

			require.Len(t, tc.want, len(worker.qpsCache))
			require.NoError(t, prom_testutil.CollectAndCompare(worker, strings.NewReader(recordQPSMetricsExpected)))

			// clear test cases for next iteration
			mock.ClearTestCases()
		})

		// unregister worker to prevent duplicate metric collection issues in further test runs
		metrics.Registry.Unregister(worker)
	}
}

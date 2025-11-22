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

package ns1

import (
	"fmt"
	"net/url"
	"regexp"
	"testing"

	"github.com/prometheus/common/promslog"
	"github.com/stretchr/testify/require"
	"gopkg.in/ns1/ns1-go.v2/mockns1"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
)

var (
	mockLogger = promslog.New(&promslog.Config{})
)

func TestRefreshZoneData(t *testing.T) {
	mock, doer, err := mockns1.New(t)
	require.NoError(t, err)
	defer mock.Shutdown()

	mockClient := api.NewClient(doer, api.SetAPIKey("mockAPIKey"))
	mockClient.Endpoint, err = url.Parse(fmt.Sprintf("https://%s/v1/", mock.Address))
	require.NoError(t, err)

	tests := map[string]struct {
		zoneEnabled   bool
		recordEnabled bool
		zoneBlacklist *regexp.Regexp
		zoneWhitelist *regexp.Regexp
		want          map[string]*Zone
		expectedLen   int
	}{
		"recordsDisabled": {recordEnabled: false, zoneEnabled: false, zoneBlacklist: nil, zoneWhitelist: nil, want: map[string]*Zone{
			"foo.bar": {},
			"keep.me": {},
			"drop.me": {},
		}, expectedLen: 3},
		"recordsEnabled": {recordEnabled: true, zoneEnabled: false, zoneBlacklist: nil, zoneWhitelist: nil, want: map[string]*Zone{
			"foo.bar": {Zone: "foo.bar", Records: []*ZoneRecord{{Domain: "test.foo.bar", ShortAns: []string{"dead::beef"}, Type: "AAAA"}}},
			"keep.me": {Zone: "keep.me", Records: []*ZoneRecord{{Domain: "test.keep.me", ShortAns: []string{"1.2.3.4"}, Type: "A"}}},
			"drop.me": {Zone: "drop.me", Records: []*ZoneRecord{{Domain: "test.drop.me", ShortAns: []string{"5.6.7.8"}, Type: "A"}}},
		}, expectedLen: 3},
		"blacklist": {recordEnabled: true, zoneEnabled: false, zoneBlacklist: regexp.MustCompile("drop.+"), zoneWhitelist: nil, want: map[string]*Zone{
			"foo.bar": {Zone: "foo.bar", Records: []*ZoneRecord{{Domain: "test.foo.bar", ShortAns: []string{"dead::beef"}, Type: "AAAA"}}},
			"keep.me": {Zone: "keep.me", Records: []*ZoneRecord{{Domain: "test.keep.me", ShortAns: []string{"1.2.3.4"}, Type: "A"}}},
		}, expectedLen: 2},
		"whitelist": {recordEnabled: true, zoneEnabled: false, zoneBlacklist: nil, zoneWhitelist: regexp.MustCompile("keep.+"), want: map[string]*Zone{
			"keep.me": {Zone: "keep.me", Records: []*ZoneRecord{{Domain: "test.keep.me", ShortAns: []string{"1.2.3.4"}, Type: "A"}}},
		}, expectedLen: 1},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, mock.AddZoneListTestCase(nil, nil,
				[]*dns.Zone{
					{Zone: "foo.bar", Records: []*dns.ZoneRecord{{Domain: "test.foo.bar", ShortAns: []string{"dead::beef"}, Type: "AAAA"}}},
					{Zone: "keep.me", Records: []*dns.ZoneRecord{{Domain: "test.keep.me", ShortAns: []string{"1.2.3.4"}, Type: "A"}}},
					{Zone: "drop.me", Records: []*dns.ZoneRecord{{Domain: "test.drop.me", ShortAns: []string{"5.6.7.8"}, Type: "A"}}},
				},
			))

			getRecords := tc.recordEnabled || tc.zoneEnabled

			require.NoError(t, mock.AddZoneGetTestCase("foo.bar", nil, nil,
				&dns.Zone{Zone: "foo.bar", Records: []*dns.ZoneRecord{{Domain: "test.foo.bar", ShortAns: []string{"dead::beef"}, Type: "AAAA"}}},
				getRecords,
			))

			require.NoError(t, mock.AddZoneGetTestCase("keep.me", nil, nil,
				&dns.Zone{Zone: "keep.me", Records: []*dns.ZoneRecord{{Domain: "test.keep.me", ShortAns: []string{"1.2.3.4"}, Type: "A"}}},
				getRecords,
			))

			require.NoError(t, mock.AddZoneGetTestCase("drop.me", nil, nil,
				&dns.Zone{Zone: "drop.me", Records: []*dns.ZoneRecord{{Domain: "test.drop.me", ShortAns: []string{"5.6.7.8"}, Type: "A"}}},
				getRecords,
			))

			got := RefreshZoneData(mockLogger, mockClient, getRecords, tc.zoneBlacklist, tc.zoneWhitelist)
			require.Equal(t, tc.want, got)
			require.Len(t, got, tc.expectedLen)
			for _, zone := range got {
				switch getRecords {
				case true:
					require.Len(t, zone.Records, 1)
				default:
					require.Empty(t, zone.Records)
				}
			}

			// clear test cases for next iteration
			mock.ClearTestCases()
		})
	}
}

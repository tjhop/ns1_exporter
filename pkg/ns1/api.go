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
	"crypto/tls"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/promlog"
	api "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"

	"github.com/tjhop/ns1_exporter/pkg/metrics"
)

var (
	logger = promlog.New(&promlog.Config{})
)

type APIConfig struct {
	Concurrency   int
	Endpoint      string
	TLSSkipVerify bool
	UserAgent     string
	EnableDDI     bool
}

// ZoneRecord is an internal struct that is essentially the same thing as a
// `model/dns.ZoneRecord`, just trimmed down to removee a bunch of fields we
// don't care about
type ZoneRecord struct {
	Domain   string
	ShortAns []string
	Type     string
	// TODO: add support for tags/local tags for DDI?
}

// Zone is an internal struct that is essentially the same thing as
// `model/dns.Zone`, mostly trimmed down to drop a bunch of fields we don't
// care about right now
type Zone struct {
	Zone       string
	NetworkIDs []int // not used yet
	Records    []*ZoneRecord
}

// QPS holds values related to QPS info from the NS1 API
type QPS struct {
	Value      float32
	ZoneName   string
	RecordName string
	RecordType string
}

// NewClient creates a new NS1 API client based on the provided config
func NewClient(config APIConfig) *api.Client {
	token := os.Getenv("NS1_APIKEY")
	if token == "" {
		level.Error(logger).Log("err", "NS1_APIKEY environment variable is not set")
		os.Exit(1)
	}

	httpClient := &http.Client{Timeout: time.Second * 15}
	clientOpts := []func(*api.Client){api.SetAPIKey(token), api.SetFollowPagination(true), api.SetUserAgent(config.UserAgent)}

	if config.Endpoint != "" {
		clientOpts = append(clientOpts, api.SetEndpoint(config.Endpoint))
	}

	if config.TLSSkipVerify {
		tr := &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		}
		httpClient.Transport = tr
	}

	if config.EnableDDI {
		clientOpts = append(clientOpts, api.SetDDIAPI())
	}

	c := api.NewClient(httpClient, clientOpts...)

	if config.Concurrency > 0 {
		c.RateLimitStrategyConcurrent(config.Concurrency)
	} else {
		c.RateLimitStrategySleep()
	}

	return c
}

func RefreshZoneData(c *api.Client, getRecords bool, zoneBlacklist, zoneWhitelist *regexp.Regexp) map[string]*Zone {
	zMap := make(map[string]*Zone)

	zones, _, err := c.Zones.List()
	if err != nil {
		level.Error(logger).Log("msg", "Failed to list zones from NS1 API", "err", err.Error(), "worker", "exporter")
		metrics.MetricExporterNS1APIFailures.Inc()
		return zMap
	}

	// check listed zones against any provided blacklist and remove ones that we don't care about
	if zoneBlacklist != nil && zoneBlacklist.String() != "" {
		var filteredZones []*dns.Zone
		for _, z := range zones {
			if zoneBlacklist.MatchString(z.Zone) {
				// if zone in blacklist, log it and skip it
				level.Debug(logger).Log("msg", "skipping zone because it matches blacklist regex", "zone", z.Zone, "blacklist_regex", zoneBlacklist.String())
				continue
			}

			filteredZones = append(filteredZones, z)
		}

		zones = filteredZones
	}

	// check listed zones against any provided whitelist and keep only ones that we care about
	if zoneWhitelist != nil && zoneWhitelist.String() != "" {
		var filteredZones []*dns.Zone
		for _, z := range zones {
			if !zoneWhitelist.MatchString(z.Zone) {
				// if zone not in whitelist, log it and skip it
				level.Debug(logger).Log("msg", "skipping zone because it doesn't match whitelist regex", "zone", z.Zone, "whitelist_regex", zoneWhitelist.String())
				continue
			}
			filteredZones = append(filteredZones, z)
		}

		zones = filteredZones
	}

	// iterate over listed zones and get details for each
	switch {
	case getRecords:
		for _, z := range zones {
			zoneDataRaw, _, err := c.Zones.Get(z.Zone, true)
			if err != nil {
				level.Error(logger).Log("msg", "Failed to get zone data from NS1 API", "err", err.Error(), "worker", "exporter", "zone_name", z.Zone)
				continue
			}

			// extract data we care about into internal counterpart structs
			var recordData []*ZoneRecord
			for _, r := range zoneDataRaw.Records {
				record := &ZoneRecord{
					Domain:   r.Domain,
					ShortAns: r.ShortAns,
					Type:     r.Type,
				}

				recordData = append(recordData, record)
			}

			zoneData := &Zone{
				Zone:       z.Zone,
				NetworkIDs: zoneDataRaw.NetworkIDs,
				Records:    recordData,
			}

			// insert zone into new worker "cache" map
			zMap[z.Zone] = zoneData
		}
	default:
		// if we're only getting account level qps data, insert empty
		// zone structs into map so we can at least maintain a "list"
		// of zones
		for _, z := range zones {
			zMap[z.Zone] = &Zone{}
		}
	}

	return zMap
}

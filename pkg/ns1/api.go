package ns1

import (
	"crypto/tls"
	"net/http"
	"os"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/promlog"
	api "gopkg.in/ns1/ns1-go.v2/rest"

	"github.com/tjhop/ns1_exporter/pkg/metrics"
)

var (
	logger = promlog.New(&promlog.Config{})
)

// TODO: make flags for all these
type APIConfig struct {
	Concurrency   int
	Endpoint      string
	TLSSkipVerify bool
	UserAgent     string
	EnableDDI     bool
}

// ZoneRecord is an internal package that is essentially the same thing as a
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

// Client is a type alias for the NS1 API Client, for use with defining custom methods.
type Client api.Client

// NewClient creates a new NS1 API client based on the provided config
func NewClient(config APIConfig) *Client {
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

	client := Client(*c)

	return &client
}

func (c *Client) RefreshZoneData(getRecords bool) map[string]*Zone {
	zMap := make(map[string]*Zone)

	zones, _, err := c.Zones.List()
	if err != nil {
		level.Error(logger).Log("msg", "Failed to list zones from NS1 API", "err", err.Error(), "worker", "exporter")
		metrics.MetricExporterNS1APIFailures.Inc()
		return zMap
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

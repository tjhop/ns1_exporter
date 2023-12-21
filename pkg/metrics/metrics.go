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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"sync"
)

const (
	metricNamespace = "ns1"
)

var (
	once     sync.Once
	Registry *prometheus.Registry

	// metric descriptions for ns1 stats as prometheus metrics
	MetricBuildInfoDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, "build", "info"),
		"NS1 exporter build information",
		[]string{"version", "build_date", "commit"}, nil,
	)
	MetricQPSDesc = prometheus.NewDesc(
		prometheus.BuildFQName(metricNamespace, "stats", "queries_per_second"),
		"DNS queries per second for the labeled NS1 resource. Note that NS1 QPS metrics are time delayed, not real-time.",
		[]string{"zone_name", "record_name", "record_type"}, nil,
	)

	// metrics for operations of the exporter itself
	MetricExporterNS1APIFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: metricNamespace,
		Name:      "api_failures",
		Help:      "Number of failed NS1 API calls.",
	})
)

func init() {
	once.Do(func() {
		Registry = prometheus.NewRegistry()

		Registry.MustRegister(
			// add standard process/go metrics to registry
			collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
			collectors.NewGoCollector(),
			// register raw metrics -- let exporter worker register
			// itself for collection of metrics from ns1 api
			MetricExporterNS1APIFailures,
		)
	})
}

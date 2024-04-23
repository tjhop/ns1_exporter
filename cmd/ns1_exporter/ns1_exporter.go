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

package main

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/exporter-toolkit/web/kingpinflag"

	"github.com/tjhop/ns1_exporter/internal/version"
	"github.com/tjhop/ns1_exporter/pkg/exporter"
	"github.com/tjhop/ns1_exporter/pkg/metrics"
	"github.com/tjhop/ns1_exporter/pkg/ns1"
	sd "github.com/tjhop/ns1_exporter/pkg/servicediscovery"
)

const (
	programName = "ns1_exporter"
	defaultPort = 8080
)

var (
	// so. many. flags.
	flagWebTelemetryPath = kingpin.Flag(
		"web.telemetry-path",
		"Path under which to expose metrics.",
	).Default("/metrics").String()

	flagWebSDPath = kingpin.Flag(
		"web.service-discovery-path",
		"Path under which to expose targets for Prometheus HTTP service discovery.",
	).Default("/sd").String()

	flagWebMaxRequests = kingpin.Flag(
		"web.max-requests",
		"Maximum number of parallel scrape requests. Use 0 to disable.",
	).Default("40").Int()

	// From NS1 terraform provider docs, with relation to concurrency, risk
	// of 429 from rate limiting, etc:
	//
	// Setting this to a value of 60 represents a good balance between
	// optimising for performance and reducing the risk of a 429 response.
	// If you still encounter issues then you can increase this value: we
	// would recommend you do so in increments of 20.
	flagNS1Concurrency = kingpin.Flag(
		"ns1.concurrency",
		"NS1 API request concurrency. Default (0) uses NS1 Go SDK sleep strategry. 60 may be good balance between performance and reduced risk of HTTP 429, see https://pkg.go.dev/gopkg.in/ns1/ns1-go.v2/rest and exporter documentation for more information.",
	).Default("0").Int()

	// TODO: allow enabling DDI at some poitn? do we need it for anything this project does?
	// flagNS1EnableDDI = kingpin.Flag(
	// "ns1.enable-ddi",
	// "Whether or not to enable DDI in the NS1 API client",
	// ).Bool()

	flagNS1ExporterEnableRecordQPS = kingpin.Flag(
		"ns1.exporter-enable-record-qps",
		"Whether or not to enable retrieving record-level QPS stats from the NS1 API. Default is enabled.",
	).Default("true").Bool()

	flagNS1ExporterEnableZoneQPS = kingpin.Flag(
		"ns1.exporter-enable-zone-qps",
		"Whether or not to enable retrieving zone-level QPS stats from the NS1 API (overridden by `--ns1.enable-record-qps`). Default is enabled.",
	).Default("true").Bool()

	flagNS1ExporterZoneBlacklistRegex = kingpin.Flag(
		"ns1.exporter-zone-blacklist",
		"A regular expression of zone(s) the exporter is not allowed to query qps stats for (takes precedence over --ns1.exporter-zone-whitelist).",
	).Default("").Regexp()

	flagNS1ExporterZoneWhitelistRegex = kingpin.Flag(
		"ns1.exporter-zone-whitelist",
		"A regular expression of zone(s) the exporter is allowed to query qps stats for.",
	).Default("").Regexp()

	flagNS1EnableSD = kingpin.Flag(
		"ns1.enable-service-discovery",
		"Whether or not to enable an HTTP endpoint to expose NS1 DNS records as HTTP service discovery targets. Default is disabled.",
	).Default("false").Bool()

	flagNS1SDRefreshInterval = kingpin.Flag(
		"ns1.sd-refresh-interval",
		"The interval at which targets for Prometheus HTTP service discovery will be refreshed from the NS1 API.",
	).Default("1m").Duration()

	flagNS1SDZoneBlacklistRegex = kingpin.Flag(
		"ns1.sd-zone-blacklist",
		"A regular expression of zone(s) that the service discovery mechanism will not provide targets for (takes precedence over --ns1.sd-zone-whitelist).",
	).Default("").Regexp()

	flagNS1SDZoneWhitelistRegex = kingpin.Flag(
		"ns1.sd-zone-whitelist",
		"A regular expression of zone(s) that the service discovery mechanism will provide targets for.",
	).Default("").Regexp()

	flagNS1SDRecordTypeRegex = kingpin.Flag(
		"ns1.sd-record-type",
		"A regular expression of record types that the service discovery mechanism will provide targets for.",
	).Default("").Regexp()

	flagRuntimeGOMAXPROCS = kingpin.Flag(
		"runtime.gomaxprocs", "The target number of CPUs Go will run on (GOMAXPROCS).",
	).Envar("GOMAXPROCS").Default("1").Int()

	toolkitFlags = kingpinflag.AddFlags(kingpin.CommandLine, fmt.Sprintf(":%d", defaultPort))
)

func main() {
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print(programName))
	kingpin.CommandLine.UsageWriter(os.Stdout)
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	level.Info(logger).Log("msg", "Starting "+programName, "version", version.Version, "build_date", version.BuildDate, "commit", version.Commit, "go_version", runtime.Version())

	// nicely yell at people needlessly running as root
	if os.Geteuid() == 0 {
		level.Warn(logger).Log("msg", programName+"is running as root user. This exporter is designed to run as unprivileged user, root is not required.")
	}

	runtime.GOMAXPROCS(*flagRuntimeGOMAXPROCS)
	level.Debug(logger).Log("msg", "Go MAXPROCS", "procs", runtime.GOMAXPROCS(0))

	Run(logger)
}

func Run(logger log.Logger) {
	token := os.Getenv("NS1_APIKEY")
	if token == "" {
		level.Error(logger).Log("err", "NS1_APIKEY environment variable is not set")
		os.Exit(1)
	}

	apiClient := ns1.NewClient(ns1.APIConfig{
		Token:       token,
		Concurrency: *flagNS1Concurrency,
		UserAgent:   fmt.Sprintf("ns1_exporter/%s", version.Version),
		// EnableDDI:   *flagNS1EnableDDI,
	})
	exporterWorker := exporter.NewWorker(logger, apiClient, *flagNS1ExporterEnableZoneQPS, *flagNS1ExporterEnableRecordQPS, *flagNS1ExporterZoneBlacklistRegex, *flagNS1ExporterZoneWhitelistRegex)
	sdWorker := sd.NewWorker(logger, apiClient, *flagNS1SDZoneBlacklistRegex, *flagNS1SDZoneWhitelistRegex, *flagNS1SDRecordTypeRegex)

	var g run.Group
	{
		// termination and cleanup
		term := make(chan os.Signal, 1)
		signal.Notify(term, os.Interrupt, syscall.SIGTERM)
		cancel := make(chan struct{})
		g.Add(
			func() error {
				select {
				case sig := <-term:
					level.Warn(logger).Log("msg", "Caught signal, exiting gracefully.", "signal", sig.String())
				case <-cancel:
				}

				return nil
			},
			func(err error) {
				close(cancel)
			},
		)
	}
	{
		// ticker routine to refresh metrics from NS1 api to serve with exporter
		cancel := make(chan struct{})
		g.Add(
			func() error {
				ticker := time.NewTicker(1 * time.Minute)
				defer ticker.Stop()

				for {
					// work around the fact that tickers
					// don't immediately trigger:
					//
					// refresh immediately in each loop
					// iteration to get the effect of an
					// immediate refresh on first call, and
					// then have the ticker's `select` case
					// simply continue the loop to
					// retrigger a new wait/refresh on each
					// ticker interval
					exporterWorker.Refresh()

					select {
					case <-ticker.C:
						continue
					case <-cancel:
						return nil
					}
				}
			},
			func(error) {
				close(cancel)
			},
		)
	}
	{
		// ticker routine to refresh targets from NS1 api to serve for HTTP SD
		cancel := make(chan struct{})
		g.Add(
			func() error {
				switch *flagNS1EnableSD {
				case true:
					ticker := time.NewTicker(*flagNS1SDRefreshInterval)
					defer ticker.Stop()

					for {
						// work around the fact that tickers
						// don't immediately trigger:
						//
						// refresh immediately in each loop
						// iteration to get the effect of an
						// immediate refresh on first call, and
						// then have the ticker's `select` case
						// simply continue the loop to
						// retrigger a new wait/refresh on each
						// ticker interval
						sdWorker.Refresh()

						select {
						case <-ticker.C:
							continue
						case <-cancel:
							return nil
						}
					}
				default:
					<-cancel
					return nil
				}
			},
			func(error) {
				close(cancel)
			},
		)
	}
	{
		// web server
		cancel := make(chan struct{})
		server := setupServer(logger, sdWorker)

		g.Add(
			func() error {
				if err := web.ListenAndServe(server, toolkitFlags, logger); err != http.ErrServerClosed {
					level.Error(logger).Log("err", err)
					return err
				}

				<-cancel

				return nil
			},
			func(error) {
				if err := server.Shutdown(context.Background()); err != nil {
					// Error from closing listeners, or context timeout:
					level.Error(logger).Log("err", err)
				}
				close(cancel)
			},
		)
	}

	if err := g.Run(); err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}
	level.Info(logger).Log("msg", programName+" finished. See you next time!")
}

func setupServer(logger log.Logger, sdWorker *sd.Worker) *http.Server {
	server := &http.Server{
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	metricsHandler := promhttp.HandlerFor(
		prometheus.Gatherers{metrics.Registry},
		promhttp.HandlerOpts{
			ErrorLog:            stdlog.New(log.NewStdlibAdapter(level.Error(logger)), "", 0),
			ErrorHandling:       promhttp.ContinueOnError,
			MaxRequestsInFlight: *flagWebMaxRequests,
			Registry:            metrics.Registry,
		},
	)
	metricsHandler = promhttp.InstrumentMetricHandler(
		metrics.Registry, metricsHandler,
	)
	http.Handle("/metrics", metricsHandler)

	landingPageLinks := []web.LandingLinks{
		{
			Address: *flagWebTelemetryPath,
			Text:    "Metrics",
		},
	}

	if *flagNS1EnableSD {
		landingPageLinks = append(landingPageLinks,
			web.LandingLinks{
				Address: *flagWebSDPath,
				Text:    "Service Discovery",
			},
		)

		http.Handle("/sd", sdWorker)
	}

	if *flagWebTelemetryPath != "/" {
		landingConfig := web.LandingConfig{
			Name:        "NS1 Exporter",
			Description: "Prometheus NS1 Exporter",
			Version:     version.Info(),
			Links:       landingPageLinks,
		}
		landingPage, err := web.NewLandingPage(landingConfig)
		if err != nil {
			level.Error(logger).Log("err", err)
			os.Exit(1)
		}
		http.Handle("/", landingPage)
	}

	return server
}

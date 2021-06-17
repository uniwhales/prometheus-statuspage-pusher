package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/prometheus/client_golang/api"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type queryConfig map[string]string

var (
	prometheusURL    = flag.String("prom", "http://localhost:9090", "URL of Prometheus server")
	statusPageAPIKey = flag.String("apikey", "", "Statuspage API key")
	statusPageID     = flag.String("pageid", "", "Statuspage page ID")
	queryConfigFile  = flag.String("config", "queries.yaml", "Query config file")
	metricInterval   = flag.Duration("interval", 30*time.Second, "Metric push interval")

	httpClient = &http.Client{
		Timeout: 5 * time.Second,
	}
)

func main() {
	flag.Parse()
	qConfig := queryConfig{}
	qcd, err := ioutil.ReadFile(*queryConfigFile)
	if err != nil {
		log.Fatalf("Couldn't read config file: %w", err)
	}
	if err := yaml.Unmarshal(qcd, &qConfig); err != nil {
		log.Fatalf("Couldn't parse config file: %w", err)
	}

	queryPrometheus(qConfig)
	ticker := time.NewTicker(*metricInterval)

	go func() {
		for {
			select {
			case <-ticker.C:
				queryPrometheus(qConfig)
			}
		}
	}()
}

func queryPrometheus(qConfig queryConfig) {
	client, err := api.NewClient(api.Config{Address: *prometheusURL})
	if err != nil {
		log.Fatalf("Couldn't create Prometheus client: %w", err)
	}
	api := prometheus.NewAPI(client)

	for metricID, query := range qConfig {
		ctxlog := log.WithField("metric_id", metricID)

		ts := time.Now()
		resp, warnings, err := api.Query(context.Background(), query, ts)
		if err != nil {
			ctxlog.Errorf("Couldn't query Prometheus: %w", err)
			continue
		}

		if len(warnings) > 0 {
			for _, warning := range warnings {
				ctxlog.Warnf("Prometheus query warning: %s", warning)
			}
		}

		vec := resp.(model.Vector)
		if l := vec.Len(); l != 1 {
			ctxlog.Errorf("Expected query to return single value, actual %d samples", l)
			continue
		}

		value := vec[0].Value
		if "NaN" == value.String() {
			ctxlog.Error("Query returns NaN")
			continue
		}

		log.Info("Query result: %v", value)

		if err := sendStatusPage(ts, metricID, float64(value)); err != nil {
			ctxlog.Error("Couldn't send metric to Statuspage: %w", err)
			continue
		}
	}
}

func sendStatusPage(ts time.Time, metricID string, value float64) error {
	values := url.Values{
		"data[timestamp]": []string{strconv.FormatInt(ts.Unix(), 10)},
		"data[value]":     []string{strconv.FormatFloat(value, 'f', -1, 64)},
	}
	url := "https://api.statuspage.io" + path.Join("/v1", "pages", *statusPageID, "metrics", metricID, "data.json")
	req, err := http.NewRequest("POST", url, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+*statusPageAPIKey)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respStr, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("Empty API Error")
		}
		return fmt.Errorf("API Error: %s", string(respStr))
	}
	return nil
}

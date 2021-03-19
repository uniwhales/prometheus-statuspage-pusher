package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/api"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
)

type queryConfig map[string]string

var (
	prometheusURL   = flag.String("pu", "http://localhost:9090", "URL of Prometheus server")
	statusPageURL   = flag.String("su", "https://api.statuspage.io", "URL of Statuspage API")
	statusPageToken = flag.String("st", os.Getenv("STATUSPAGE_TOKEN"), "Statuspage Oauth token")
	statusPageID    = flag.String("si", "", "Statuspage page ID")
	queryConfigFile = flag.String("c", "queries.yaml", "Query config file")
	metricInterval  = flag.Duration("i", 30*time.Second, "Metric push interval")
	prometheusPort  = flag.Int("prometheusPort", 9095, "Port to serve Prometheus metrics from")

	httpClient = &http.Client{}
)

func fatal(fields ...interface{}) {
	log.Error(fields...)
	os.Exit(1)
}
func main() {
	flag.Parse()
	qConfig := queryConfig{}
	qcd, err := ioutil.ReadFile(*queryConfigFile)
	if err != nil {
		fatal("Couldn't read config file ", err.Error())
	}
	if err := yaml.Unmarshal(qcd, &qConfig); err != nil {
		fatal("Couldn't parse config file ", err.Error())
	}

	client, err := api.NewClient(api.Config{Address: *prometheusURL})
	if err != nil {
		fatal("Couldn't create Prometheus client ", err.Error())
	}
	api := prometheus.NewAPI(client)

	go metricsServer(*prometheusPort)

	for {
		for metricID, query := range qConfig {
			ts := time.Now()
			resp, warnings, err := api.Query(context.Background(), query, ts)
			if err != nil {
				log.Error("Couldn't query Prometheus ", err.Error())
				continue
			}
			if len(warnings) > 0 {
				for _, warning := range warnings {
					log.Warn("Prometheus query warning ", warning)
				}
			}
			vec := resp.(model.Vector)
			if l := vec.Len(); l != 1 {
				log.Error("Expected query to return single value: ", "samples ", l)
				continue
			}

			log.Info("metricID: ", metricID, "resp: ", vec[0].Value)
			if err := sendStatusPage(ts, metricID, float64(vec[0].Value)); err != nil {
				log.Error("Couldn't send metric to Statuspage ", err.Error())
				continue
			}
		}
		time.Sleep(*metricInterval)
	}
}

func sendStatusPage(ts time.Time, metricID string, value float64) error {
	values := url.Values{
		"data[timestamp]": []string{strconv.FormatInt(ts.Unix(), 10)},
		"data[value]":     []string{strconv.FormatFloat(value, 'f', -1, 64)},
	}
	url := *statusPageURL + path.Join("/v1", "pages", *statusPageID, "metrics", metricID, "data.json")
	req, err := http.NewRequest("POST", url, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "OAuth "+*statusPageToken)
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
		return errors.New("API Error: " + string(respStr))
	}
	return nil
}

func metricsServer(port int) {
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(":"+strconv.Itoa(port), nil)
	if err != nil {
		log.Error("Error serving Metrics server")
	}
}

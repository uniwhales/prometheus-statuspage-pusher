package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"

	"github.com/prometheus/common/model"
)

func init() {
	pflag.String("promethueus_url", "http://localhost:9090", "URL of Prometheus server")
	pflag.String("statuspage_api_key", "", "Statuspage API key")
	pflag.String("statuspage_page_id", "", "Statuspage page ID")
	pflag.String("config", "queries.yaml", "Query config file")
	pflag.Duration("interval", 30*time.Second, "Metric push interval")
	pflag.Uint("rounding", 6, "Round metric values to specific decimal places")
	pflag.String("backfill", "", "Backfill the data points in, for example, 5d")
	pflag.String("log_level", "info", "Log level accepted by Logrus, for example, \"error\", \"warn\", \"info\", \"debug\", ...")

	pflag.Parse()
	_ = viper.BindPFlags(pflag.CommandLine)
	viper.AutomaticEnv()
}

var (
	httpClient = &http.Client{
		Timeout: 30 * time.Second,
	}

	queryConfig map[string]string
)

func main() {
	flag.Parse()
	if lvl, err := log.ParseLevel(viper.GetString("log_level")); err != nil {
		log.Fatal(err)
	} else {
		log.SetLevel(lvl)
	}

	qcd, err := os.ReadFile(viper.GetString("config"))
	if err != nil {
		log.Fatalf("Couldn't read config file: %s", err)
	}
	if err := yaml.Unmarshal(qcd, &queryConfig); err != nil {
		log.Fatalf("Couldn't parse config file: %s", err)
	}

	if backfillDuration := viper.GetString("backfill"); backfillDuration != "" {
		md, err := model.ParseDuration(backfillDuration)
		if err != nil {
			log.Fatalf("Incorrect duration format: %s", backfillDuration)
		}
		d := time.Duration(md)
		queryAndPush(&d)
	} else {
		queryAndPush(nil)
	}

	ticker := time.NewTicker(viper.GetDuration("interval"))
	for {
		select {
		case <-ticker.C:
			go queryAndPush(nil)
		}
	}
}

func queryAndPush(backfill *time.Duration) {
	log.Infof("Started to query and pushing metrics")

	metrics := queryPrometheus(backfill, viper.GetUint("rounding"))
	chunkedMetrics := chunkMetrics(metrics)

	for _, m := range chunkedMetrics {
		err := pushStatuspage(m)
		if err != nil {
			log.Error(err)
		}
	}

	log.Infof("Finished querying and pushing metrics")
}

func pushStatuspage(metrics statuspageMetrics) error {
	const apiBase = "https://api.statuspage.io/v1"
	apiKey := viper.GetString("statuspage_api_key")
	pageId := viper.GetString("statuspage_page_id")

	jsonContents, err := json.Marshal(statuspageMetricsPayload{Data: metrics})
	if err != nil {
		return err
	}

	log.Debugf("Metrics payload pushing to Statuspage: %s", jsonContents)

	metricIDs := make([]string, 0, len(metrics))
	for id := range metrics {
		metricIDs = append(metricIDs, id)
	}

	log.Infof("Pushing metrics: %s", strings.Join(metricIDs, ", "))

	url := fmt.Sprintf("%s/pages/%s/metrics/data", apiBase, pageId)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonContents))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("OAuth %s", apiKey))

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respStr, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("HTTP status %d, Empty API response", resp.StatusCode)
		}
		return fmt.Errorf("HTTP status %d, API error: %s", resp.StatusCode, string(respStr))
	}

	return nil
}

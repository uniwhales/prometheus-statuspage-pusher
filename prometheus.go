package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/prometheus/client_golang/api"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

func queryPrometheus(backfill *time.Duration, decimal uint) statuspageMetrics {
	client, err := api.NewClient(api.Config{Address: *prometheusURL})
	if err != nil {
		log.Fatalf("Couldn't create Prometheus client: %s", err)
	}
	api := prometheus.NewAPI(client)

	metrics := make(statuspageMetrics)
	for metricID, query := range queryConfig {
		ctxlog := log.WithFields(log.Fields{
			"metric_id": metricID,
			"backfill":  backfill,
		})

		var (
			err          error
			warnings     prometheus.Warnings
			metricPoints []statuspageMetricPoint
		)

		if backfill == nil {
			metricPoints, warnings, err = queryInstant(api, query, decimal, ctxlog)
		} else {
			metricPoints, warnings, err = queryRange(api, query, decimal, backfill, ctxlog)
		}

		for _, w := range warnings {
			ctxlog.Warnf("Prometheus query warning: %s", w)
		}

		if err != nil {
			ctxlog.Error(err)
			continue
		}

		metrics[metricID] = metricPoints
	}

	return metrics
}

func queryInstant(api prometheus.API, query string, decimal uint, logger *log.Entry) ([]statuspageMetricPoint, prometheus.Warnings, error) {
	now := time.Now()
	response, warnings, err := api.Query(context.Background(), query, now)

	if err != nil {
		return nil, warnings, fmt.Errorf("Couldn't query Prometheus: %w", err)
	}

	if response.Type() != model.ValVector {
		return nil, warnings, fmt.Errorf("Expected result type %s, got %s", model.ValVector, response.Type())
	}

	vec := response.(model.Vector)
	if l := vec.Len(); l != 1 {
		return nil, warnings, fmt.Errorf("Expected single time serial, got %d", l)
	}

	value := vec[0].Value
	logger.Infof("Query result: %s", value)

	if math.IsNaN(float64(value)) {
		return nil, warnings, fmt.Errorf("Invalid metric value NaN")
	}

	return []statuspageMetricPoint{
		{
			Timestamp: int64(vec[0].Timestamp / 1000),
			Value:     json.Number(fmt.Sprintf("%.*f", decimal, value)),
		},
	}, warnings, nil
}

func queryRange(api prometheus.API, query string, decimal uint, backfill *time.Duration, logger *log.Entry) ([]statuspageMetricPoint, prometheus.Warnings, error) {
	now := time.Now()
	start := now.Add(-*backfill)
	var (
		end          time.Time
		promWarnings prometheus.Warnings
		metricPoints []statuspageMetricPoint
	)

	for start.Before(now) {
		end = start.Add(24 * time.Hour) // 24h as a step
		if end.After(now) {
			end = now
		}

		logger.Infof("Querying metrics from %s to %s with step %s", start.Format(time.RFC3339), end.Format(time.RFC3339), *metricInterval)
		response, warnings, err := api.QueryRange(context.Background(), query, prometheus.Range{
			Start: start,
			End:   end,
			Step:  *metricInterval,
		})
		promWarnings = append(promWarnings, warnings...)

		if err != nil {
			return nil, promWarnings, fmt.Errorf("Couldn't query Prometheus: %w", err)
		}

		if response.Type() != model.ValMatrix {
			return nil, promWarnings, fmt.Errorf("Expected result type %s, got %s", model.ValMatrix, response.Type())
		}

		mtx := response.(model.Matrix)
		if l := mtx.Len(); l != 1 {
			return nil, promWarnings, fmt.Errorf("Expected single time serial, got %d", l)
		}

		logger.Infof("Got %d samples", len(mtx[0].Values))
		logger.Debugf("Query result: %v", mtx[0].Values)

		for _, v := range mtx[0].Values {
			if math.IsNaN(float64(v.Value)) {
				logger.Warn("Invalid metric value NaN")
				continue
			}
			metricPoints = append(metricPoints, statuspageMetricPoint{
				Timestamp: int64(v.Timestamp / 1000),
				Value:     json.Number(fmt.Sprintf("%.*f", decimal, v.Value)),
			})
		}

		start = end.Add(1 * time.Millisecond)
	}

	return metricPoints, promWarnings, nil
}

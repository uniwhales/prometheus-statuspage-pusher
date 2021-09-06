package main

import (
	"encoding/json"
)

type statuspageMetricPoint struct {
	Timestamp int64       `json:"timestamp"`
	Value     json.Number `json:"value"`
}

type statuspageMetrics map[string][]statuspageMetricPoint

type statuspageMetricsPayload struct {
	Data statuspageMetrics `json:"data"`
}

const MAX_NUMBER_OF_METRIC_POINTS_PER_REQUEST = 3000

func chunkMetrics(metrics statuspageMetrics) []statuspageMetrics {
	chunkedMetrics := []statuspageMetrics{}

	chunk := statuspageMetrics{}
	capacity := MAX_NUMBER_OF_METRIC_POINTS_PER_REQUEST
	for metricID, points := range metrics {
		var start, end int
		length := len(points)
		for start < length {
			end = start + capacity
			if end > length {
				end = length
			}
			if _, ok := chunk[metricID]; ok {
				chunk[metricID] = append(chunk[metricID], points[start:end]...)
			} else {
				chunk[metricID] = points[start:end]
			}
			capacity = capacity - (end - start)
			if capacity == 0 {
				chunkedMetrics = append(chunkedMetrics, chunk)
				chunk = statuspageMetrics{}
				capacity = MAX_NUMBER_OF_METRIC_POINTS_PER_REQUEST
			}
			start = end
		}
	}

	chunkedMetrics = append(chunkedMetrics, chunk)

	return chunkedMetrics
}

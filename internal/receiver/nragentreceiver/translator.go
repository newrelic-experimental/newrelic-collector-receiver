package nragentreceiver

import (
	"fmt"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"strings"
)

func TranslateMetrics(otelMetrics pmetric.MetricSlice, nrMetricData []interface{}, startTime pcommon.Timestamp, endTime pcommon.Timestamp) {
	for i := 0; i < len(nrMetricData); i++ {
		/*
			[
				{
					“name”:”name of metric”,
					“scope”:”scope of metric”,
				},
				[count, total time, exclusive time, min time, max time, sum of squares]
			]
			Uses attributes to differentiate between total time and exclusive time
			Sum of squares is dropped due to lack of use in the UI
			Even the apdex special case does not use the sum of squares entry
		*/
		timesliceMetric := nrMetricData[i].([]interface{})
		timesliceMetricNameMap := timesliceMetric[0].(map[string]interface{})
		timesliceMetricName := timesliceMetricNameMap["name"].(string)
		timesliceMetricData := timesliceMetric[1].([]interface{})

		switch metricNameSlice := strings.Split(timesliceMetricName, "/"); metricNameSlice[0] {
		case "WebTransaction":
			mapWebTransactionMetric(&otelMetrics, timesliceMetricName, startTime, endTime, timesliceMetricData)
		case "External":
			mapExternalMetric(&otelMetrics, timesliceMetricName, startTime, endTime, timesliceMetricData)
		case "Memory":
			mapMemoryMetric(&otelMetrics, timesliceMetricName, startTime, endTime, timesliceMetricData)
		case "Jmx":
			mapJMXMetric(&otelMetrics, timesliceMetricName, startTime, endTime, timesliceMetricData)
		case "MemoryPool":
			mapMemoryPoolMetric(&otelMetrics, timesliceMetricName, startTime, endTime, timesliceMetricData)
		case "CPU":
			mapCPUMetric(&otelMetrics, timesliceMetricName, startTime, endTime, timesliceMetricData)
		}
	}
}

func mapMemoryMetric(metrics *pmetric.MetricSlice, timesliceMetricName string, startTime pcommon.Timestamp, endTime pcommon.Timestamp, timesliceMetricData []interface{}) {
	fmt.Println("mapMemoryMetrics")
	if timesliceMetricName != "Memory/Physical" {
		return
	}
	metric := metrics.AppendEmpty()
	metric.SetEmptySummary()
	metric.SetName("process.memory.usage")
	metric.SetDescription("Amount of memory used")
	metric.SetUnit("By")
	setCountValues(metric, timesliceMetricData, startTime, endTime)
}

func mapJMXMetric(metrics *pmetric.MetricSlice, timesliceMetricName string, startTime pcommon.Timestamp, endTime pcommon.Timestamp, timesliceMetricData []interface{}) {
	fmt.Println("mapJVXMetrics")
	segments := strings.Split(timesliceMetricName, "/")
	metric := metrics.AppendEmpty()
	metric.SetEmptySummary()
	switch memoryType := segments[len(segments)-1]; memoryType {
	case "Thread Count":
		metric.SetName("process.runtime.jvm.threads.count")
		metric.SetUnit("threads")
		metric.SetDescription("Number of executing threads")
	case "Loaded":
		metric.SetName("process.runtime.jvm.classes.loaded")
		metric.SetUnit("classes")
		metric.SetDescription("Number of classes loaded")
	case "Unloaded":
		metric.SetName("process.runtime.jvm.classes.unloaded")
		metric.SetUnit("classes")
		metric.SetDescription("Number of classes currently unloaded")
	}
	setCountValues(metric, timesliceMetricData, startTime, endTime)

}

func mapWebTransactionMetric(metrics *pmetric.MetricSlice, timesliceMetricName string, startTime pcommon.Timestamp, endTime pcommon.Timestamp, timesliceMetricData []interface{}) {
	fmt.Println("mapWebTransactionMetric")
	metric := metrics.AppendEmpty()
	metric.SetEmptySummary()
	metric.SetName("http.server.duration")
	metric.SetUnit("ms")
	metric.SetDescription(timesliceMetricName)

	inclusiveDataPoint := setTimeValues(metric, timesliceMetricData, startTime, endTime)

	segments := strings.SplitAfterN(timesliceMetricName, "/", 3)
	if len(segments) == 3 {
		route := segments[len(segments)-1]
		if route == "" {
			route = "/"
		}
		inclusiveDataPoint.Attributes().PutStr("http.route", route)
	} else {
		inclusiveDataPoint.Attributes().PutStr("http.route", "/")
	}
}

func mapCPUMetric(metrics *pmetric.MetricSlice, timesliceMetricName string, startTime pcommon.Timestamp, endTime pcommon.Timestamp, timesliceMetricData []interface{}) {
	fmt.Println("mapCPUMetrics")
	suffixes := []string{"User Time", "Utilization"}
	segments := strings.Split(timesliceMetricName, "/")

	if !contains(suffixes, segments[len(segments)-1]) {
		return
	}

	metric := metrics.AppendEmpty()
	metric.SetEmptySummary()

	switch CPUtype := segments[len(segments)-1]; CPUtype {
	case "User Time":
		metric.SetName("process.cpu.time")
		metric.SetUnit("ms")
		metric.SetDescription("Total CPU milliseconds")
		setTimeValues(metric, timesliceMetricData, startTime, endTime)
	case "Utilization":
		metric.SetName("process.cpu.utilization")
		metric.SetUnit("1")
		metric.SetDescription("Percentage of CPU utilized")
		setGuageValues(metric, timesliceMetricData, startTime, endTime)
	}
}

func mapMemoryPoolMetric(metrics *pmetric.MetricSlice, timesliceMetricName string, startTime pcommon.Timestamp, endTime pcommon.Timestamp, timesliceMetricData []interface{}) {
	fmt.Println("mapMemoryPoolMetrics")
	suffixes := []string{"Used", "Commited", "Max"}
	segments := strings.Split(timesliceMetricName, "/")

	if !contains(suffixes, segments[len(segments)-1]) {
		return
	}

	metric := metrics.AppendEmpty()
	metric.SetEmptySummary()

	switch memoryType := segments[len(segments)-1]; memoryType {
	case "Used":
		metric.SetName("process.runtime.jvm.memory.usage")
		metric.SetDescription("Amount of memory used")
	case "Commited":
		metric.SetName("process.runtime.jvm.memory.commited")
		metric.SetDescription("Amount of memory commited")
	case "Max":
		metric.SetName("process.runtime.jvm.memory.limit")
		metric.SetDescription("Maximum amount of memory")
	}
	metric.SetUnit("By")
	inclusiveDataPoint := setCountValues(metric, timesliceMetricData, startTime, endTime)

	if len(segments) > 3 {
		inclusiveDataPoint.Attributes().PutStr("type", strings.ToLower(segments[len(segments)-3]))
		inclusiveDataPoint.Attributes().PutStr("pool", segments[len(segments)-2])
	}
}

func mapExternalMetric(metrics *pmetric.MetricSlice, timesliceMetricName string, startTime pcommon.Timestamp, endTime pcommon.Timestamp, timesliceMetricData []interface{}) {
	fmt.Println("mapExternalMetric")
	if strings.HasSuffix(timesliceMetricName, "/all") || strings.HasSuffix(timesliceMetricName, "/allWeb") {
		return
	}

	metric := metrics.AppendEmpty()
	metric.SetEmptySummary()
	metric.SetName("http.client.duration")
	metric.SetUnit("ms")
	metric.SetDescription(timesliceMetricName)

	inclusiveDataPoint := setTimeValues(metric, timesliceMetricData, startTime, endTime)

	segments := strings.Split(timesliceMetricName, "/")
	inclusiveDataPoint.Attributes().PutStr("http.host", segments[1])
	if len(segments) > 2 {
		inclusiveDataPoint.Attributes().PutStr("http.method", segments[len(segments)-1])
	}
}

func setTimeValues(metric pmetric.Metric, timesliceMetricData []interface{}, startTime pcommon.Timestamp, endTime pcommon.Timestamp) pmetric.SummaryDataPoint {
	summary := metric.Summary()
	dataPoints := summary.DataPoints()
	dataPoints.EnsureCapacity(1)
	inclusiveDataPoint := dataPoints.AppendEmpty()

	inclusiveDataPoint.SetCount(uint64(timesliceMetricData[0].(float64)))
	inclusiveDataPoint.SetSum(timesliceMetricData[1].(float64) * 1000)

	inclusiveDataPoint.QuantileValues().EnsureCapacity(2)
	minQuantile := inclusiveDataPoint.QuantileValues().AppendEmpty()
	minQuantile.SetQuantile(0)
	minQuantile.SetValue(timesliceMetricData[3].(float64) * 1000)
	maxQuantile := inclusiveDataPoint.QuantileValues().AppendEmpty()
	maxQuantile.SetQuantile(1)
	maxQuantile.SetValue(timesliceMetricData[4].(float64) * 1000)

	inclusiveDataPoint.SetStartTimestamp(startTime)
	inclusiveDataPoint.SetTimestamp(endTime)

	return inclusiveDataPoint
}

func setGuageValues(metric pmetric.Metric, timesliceMetricData []interface{}, startTime pcommon.Timestamp, endTime pcommon.Timestamp) pmetric.SummaryDataPoint {
	summary := metric.Summary()
	dataPoints := summary.DataPoints()
	dataPoints.EnsureCapacity(1)
	inclusiveDataPoint := dataPoints.AppendEmpty()

	inclusiveDataPoint.SetCount(uint64(timesliceMetricData[0].(float64)))
	inclusiveDataPoint.SetSum(timesliceMetricData[1].(float64) * 100)

	inclusiveDataPoint.QuantileValues().EnsureCapacity(2)
	minQuantile := inclusiveDataPoint.QuantileValues().AppendEmpty()
	minQuantile.SetQuantile(0)
	minQuantile.SetValue(timesliceMetricData[3].(float64) * 100)
	maxQuantile := inclusiveDataPoint.QuantileValues().AppendEmpty()
	maxQuantile.SetQuantile(1)
	maxQuantile.SetValue(timesliceMetricData[4].(float64) * 100)

	inclusiveDataPoint.SetStartTimestamp(startTime)
	inclusiveDataPoint.SetTimestamp(endTime)

	return inclusiveDataPoint
}

func setCountValues(metric pmetric.Metric, timesliceMetricData []interface{}, startTime pcommon.Timestamp, endTime pcommon.Timestamp) pmetric.SummaryDataPoint {
	summary := metric.Summary()
	dataPoints := summary.DataPoints()
	dataPoints.EnsureCapacity(1)
	inclusiveDataPoint := dataPoints.AppendEmpty()

	inclusiveDataPoint.SetCount(uint64(timesliceMetricData[0].(float64)))
	inclusiveDataPoint.SetSum(timesliceMetricData[1].(float64))

	inclusiveDataPoint.QuantileValues().EnsureCapacity(2)
	minQuantile := inclusiveDataPoint.QuantileValues().AppendEmpty()
	minQuantile.SetQuantile(0)
	minQuantile.SetValue(timesliceMetricData[3].(float64))
	maxQuantile := inclusiveDataPoint.QuantileValues().AppendEmpty()
	maxQuantile.SetQuantile(1)
	maxQuantile.SetValue(timesliceMetricData[4].(float64))

	inclusiveDataPoint.SetStartTimestamp(startTime)
	inclusiveDataPoint.SetTimestamp(endTime)

	return inclusiveDataPoint
}

func contains(arr []string, val string) bool {
	for _, el := range arr {
		if el == val {
			return true
		}
	}
	return false
}

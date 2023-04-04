package nragentreceiver

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/obsreport"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/receiver"

	//"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/ptrace"

	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

var errNextConsumerRespBody = []byte(`"Internal Server Error"`)

// NewRelicAgentReceiver type is used to handle spans received in the NR Trace format.
type NewRelicAgentReceiver struct {
	// addr is the address onto which the HTTP server will be bound
	host            component.Host
	tracesConsumer  consumer.Traces
	metricsConsumer consumer.Metrics
	id              component.ID

	shutdownWG        sync.WaitGroup
	server            *http.Server
	config            *Config
	httpClient        http.Client
	redirectHost      string
	proxyToNR         bool
	useResourceHeader bool

	// per-agent state
	entityGuids sync.Map
	settings    receiver.CreateSettings
}

type agentMeta struct {
	EntityGuid string `json:"entity.guid"`
	EntityName string `json:"service.name"`
	processAttributes
}

type processAttributes struct {
	Pid            int           `json:"process.pid"`
	CommandArgs    []interface{} `json:"process.runtime.command_args"`
	RuntimeVersion string        `json:"process.runtime.version"`
	RuntimeName    string        `json:"process.runtime.name"`
}

//type procAttsReq struct {
//	Pid      int64           `json:"pid"`
//	Env      [][]interface{} `json:"environment"`
//	Language string          `json:"language"`
//}

var _ http.Handler = (*NewRelicAgentReceiver)(nil)

// New creates a new nragentreceiver.NewRelicAgentReceiver reference.
func New(config *Config, settings receiver.CreateSettings) *NewRelicAgentReceiver {
	fmt.Println("New Called")
	r := &NewRelicAgentReceiver{
		id:                settings.ID,
		config:            config,
		httpClient:        http.Client{},
		proxyToNR:         true,
		useResourceHeader: true,
		settings:          settings,
	}
	return r
}

func (nr *NewRelicAgentReceiver) registerTracesConsumer(c consumer.Traces) error {
	fmt.Println("registerTracesConsumer")
	if c == nil {
		return component.ErrNilNextConsumer
	}

	nr.tracesConsumer = c
	return nil
}

func (nr *NewRelicAgentReceiver) registerMetricsConsumer(c consumer.Metrics) error {

	if c == nil {
		return component.ErrNilNextConsumer
	}

	nr.metricsConsumer = c
	return nil
}

// Start spins up the receiver's HTTP server and makes the receiver start its processing.
func (nr *NewRelicAgentReceiver) Start(_ context.Context, host component.Host) error {
	if host == nil {
		return errors.New("nil host")
	}

	fmt.Println("nragentreceiver.Start called")

	var err error
	nr.host = host
	nr.server, err = nr.config.HTTPServerSettings.ToServer(nr.host, nr.settings.TelemetrySettings, nr)
	//var listener net.Listener
	listener, err := nr.config.HTTPServerSettings.ToListener()
	if err != nil {
		fmt.Printf("Got error %v", err)
		fmt.Println()
		return err
	}
	nr.shutdownWG.Add(1)
	go func() {
		defer nr.shutdownWG.Done()

		fmt.Println("Starting nragent listener")
		if errHTTP := nr.server.Serve(listener); errHTTP != http.ErrServerClosed {
			host.ReportFatalError(errHTTP)
		}
		fmt.Println("Stopped nragent listener")
	}()

	return nil
}

// Shutdown tells the receiver that should stop reception,
// giving it a chance to perform any necessary clean-up and shutting down
// its HTTP server.
func (zr *NewRelicAgentReceiver) Shutdown(context.Context) error {
	err := zr.server.Close()
	zr.shutdownWG.Wait()
	return err
}

// processBodyIfNecessary checks the "Content-Encoding" HTTP header and if
// a compression such as "gzip", "deflate", "zlib", is found, the body will
// be uncompressed accordingly or return the body untouched if otherwise.
// Clients such as Zipkin-Java do this behavior e.g.
//
//	send "Content-Encoding":"gzip" of the JSON content.
func processBodyIfNecessary(req *http.Request) io.Reader {
	switch req.Header.Get("Content-Encoding") {
	default:
		return req.Body

	case "gzip":
		return gunzippedBodyIfPossible(req.Body)

	case "deflate", "zlib":
		return zlibUncompressedbody(req.Body)
	}
}

func processResponseBodyIfNecessary(req *http.Response) io.Reader {
	fmt.Println("processRequestBody")
	switch req.Header.Get("Content-Encoding") {
	default:
		return req.Body

	case "gzip":
		return gunzippedBodyIfPossible(req.Body)

	case "deflate", "zlib":
		return zlibUncompressedbody(req.Body)
	}
}

func gunzippedBodyIfPossible(r io.Reader) io.Reader {
	fmt.Println("gunzippedBody")
	gzr, err := gzip.NewReader(r)
	if err != nil {
		// Just return the old body as was
		return r
	}
	return gzr
}

func zlibUncompressedbody(r io.Reader) io.Reader {
	fmt.Println("zlibUncompressed")
	zr, err := zlib.NewReader(r)
	if err != nil {
		// Just return the old body as was
		return r
	}
	return zr
}

// The NewRelicAgentReceiver receives telemetry data from New Relic agents as JSON,
// unmarshals them and sends them along to the nextConsumer.
func (nr *NewRelicAgentReceiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Println("ServeHTTP")
	fmt.Printf("-- got request %+v", r)
	fmt.Println()

	query := r.URL.Query()
	switch method := query.Get("method"); method {
	case "preconnect":
		fmt.Printf("preconnect")
		//fmt.Printf("-- got request %+v", r)
		if nr.proxyToNR {
			nr.proxyPreconnect(w, r)
		} else {
			nr.processPreconnect(w, r)
		}
	case "connect":
		fmt.Printf("connect")
		if nr.proxyToNR {
			nr.proxyConnect(w, r)
		} else {
			nr.processConnect(w, r)
		}
	case "metric_data":
		fmt.Printf("metric_data")
		if nr.proxyToNR {
			//nr.proxyRequest(w, r)
			nr.processMetricData(w, r, query)
		} else {
			nr.processMetricData(w, r, query)
		}
	case "span_event_data":
		fmt.Printf("span event data")
		// Always
		nr.processSpanEventRequest(w, r, nr.redirectHost, query)
	case "shutdown":
		fmt.Println("shutdown")
		nr.removeEntityGuid(w, r)
		nr.proxyRequest(w, r)
	case "":
		fmt.Println("Receiver not yet implemented")
		http.Error(w, errors.New("receiver not implemented yet").Error(), http.StatusBadRequest)
	default:
		fmt.Printf("default")
		if nr.proxyToNR {
			nr.proxyRequest(w, r)
		} else {
			fmt.Println("dropping data for " + method)
		}
	}
}

func (nr *NewRelicAgentReceiver) removeEntityGuid(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")
	nr.entityGuids.Delete(runID)
}

func transportType(query url.Values) string {
	fmt.Printf("transportType")
	if protocol := query.Get("protocol_version"); protocol != "" {
		return "http_p" + protocol + "_agent"
	}

	return "http_unknown_agent"
}

func (nr *NewRelicAgentReceiver) processPreconnect(w http.ResponseWriter, r *http.Request) {
	// we need to buffer the body if we want to read it here and send it
	// in the request.
	fmt.Printf("processPreconnect")
	_, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Encoding", "identity")
	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(`{"return_value":{"redirect_host":"localhost"}}`))
}

func (nr *NewRelicAgentReceiver) proxyPreconnect(w http.ResponseWriter, r *http.Request) {
	// we need to buffer the body if we want to read it here and send it
	// in the request.
	fmt.Printf("proxyPreconnect")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// create a new url from the raw RequestURI sent by the client
	url := fmt.Sprintf("https://collector.newrelic.com%s", r.RequestURI)

	proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, url, bytes.NewReader(body))

	// We may want to filter some headers, otherwise we could just use a shallow copy
	// proxyReq.Header = req.Header
	proxyReq.Header = make(http.Header)
	for h, val := range r.Header {
		proxyReq.Header[h] = val
	}
	proxyReq.Header.Set("Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	proxyReq.Header.Set("Content-Encoding", "identity")

	fmt.Printf("Found content-encoding %v", r.Header.Get("Content-Encoding"))
	fmt.Println()

	fmt.Printf("-- Forwarding request %+v", proxyReq)
	fmt.Println()

	resp, err := nr.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("-- got response %+v", resp)
	fmt.Println()

	responseBodyReader := processResponseBodyIfNecessary(resp)
	responseBytes, _ := io.ReadAll(responseBodyReader)
	if c, ok := responseBodyReader.(io.Closer); ok {
		_ = c.Close()
	}

	var tmp struct {
		ReturnValue struct {
			RedirectHost string `json:"redirect_host"`
		} `json:"return_value"`
	}

	if err := json.Unmarshal(responseBytes, &tmp); err == nil {
		nr.redirectHost = tmp.ReturnValue.RedirectHost
		fmt.Printf("redirect_host: %s\n", nr.redirectHost)
	}

	w.WriteHeader(200)
	w.Header().Add("Content-Encoding", "identity")
	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(`{"return_value":{"redirect_host":"localhost"}}`))
}

func (nr *NewRelicAgentReceiver) processConnect(w http.ResponseWriter, r *http.Request) {
	fmt.Println("processConnect")
	bodyReader := processBodyIfNecessary(r)
	body, err := io.ReadAll(bodyReader)
	if c, ok := bodyReader.(io.Closer); ok {
		_ = c.Close()
	}
	if err != nil {
		fmt.Printf("Error found")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var connectInfo []ConnectInfo
	if err := json.Unmarshal(body, &connectInfo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	fmt.Printf("Trigger process")
	resourceAttributes := map[string]interface{}{}
	resourceAttributes["service.name"] = connectInfo[0].AppName[0]
	// language -> telemetry.sdk.language?
	if connectInfo[0].HostName != "" {
		resourceAttributes["host.name"] = connectInfo[0].HostName
	}
	if connectInfo[0].ProcessPid > 0 {
		resourceAttributes["process.pid"] = connectInfo[0].ProcessPid
	}
	for k, v := range connectInfo[0].Metadata {
		resourceAttributes[strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(k, "NEW_RELIC_METADATA_"), "_", "."))] = v
	}

	resourceBytes, _ := json.Marshal(&resourceAttributes)

	reply := map[string]*ConnectReply{
		"return_value": {
			RunID:            "-1",
			DataReportPeriod: 60,
			RequestHeadersMap: map[string]string{
				"new-relic-resource": base64.URLEncoding.EncodeToString(resourceBytes),
			},
			MaxPayloadSizeInBytes: 1000000,
			EntityGUID:            "",

			// Transaction Name Modifiers
			SegmentTerms: make([]interface{}, 0),
			TxnNameRules: make([]interface{}, 0),
			URLRules:     make([]interface{}, 0),
			MetricRules:  make([]interface{}, 0),

			// Cross Process
			EncodingKey:     "",
			CrossProcessID:  "",
			TrustedAccounts: make([]int, 0),

			// Settings
			KeyTxnApdex:            make(map[string]float64),
			ApdexThresholdSeconds:  0.5,
			CollectAnalyticsEvents: true,
			CollectCustomEvents:    true,
			CollectTraces:          true,
			CollectErrors:          true,
			CollectErrorEvents:     true,
			CollectSpanEvents:      true,

			// RUM
			AgentLoader: "",
			Beacon:      "",
			BrowserKey:  "",
			AppID:       "",
			ErrorBeacon: "",
			JSAgentFile: "",

			// BetterCAT/Distributed Tracing
			AccountID:                     "",
			TrustedAccountKey:             "",
			PrimaryAppID:                  "",
			SamplingTarget:                10,
			SamplingTargetPeriodInSeconds: 60,

			EventData: EventHarvestConfig{
				ReportPeriodMs: 5000,
			},
		},
	}

	replyBytes, err := json.Marshal(&reply)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Add("Content-Encoding", "identity")
	w.Header().Add("Content-Type", "application/json")
	w.Write(replyBytes)
}

func (nr *NewRelicAgentReceiver) proxyConnect(w http.ResponseWriter, r *http.Request) {
	fmt.Println("proxyConnect")
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// create a new url from the raw RequestURI sent by the client
	url := fmt.Sprintf("https://%s%s", nr.redirectHost, r.RequestURI)

	fmt.Printf("url is : %v", url)
	fmt.Println()
	proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, url, bytes.NewReader(body))

	// We may want to filter some headers, otherwise we could just use a shallow copy
	// proxyReq.Header = req.Header
	proxyReq.Header = make(http.Header)
	for h, val := range r.Header {
		proxyReq.Header[h] = val
	}
	proxyReq.Header.Set("Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	proxyReq.Header.Set("Content-Encoding", "identity")

	fmt.Printf("Found content-encoding %v", r.Header.Get("Content-Encoding"))
	fmt.Println()

	fmt.Printf("-- Forwarding request %+v", proxyReq)
	fmt.Println()

	resp, err := nr.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("-- got response %+v", resp)
	fmt.Println()

	responseBodyReader := processResponseBodyIfNecessary(resp)
	blob, _ := io.ReadAll(responseBodyReader)
	if c, ok := responseBodyReader.(io.Closer); ok {
		_ = c.Close()
	}

	var connectStruct ReplyReturnValue

	if err := json.Unmarshal(blob, &connectStruct); err != nil {
		fmt.Printf("Unmarshall Error: %v\n", err)
	}

	var connectInfo []ConnectInfo
	if err := json.Unmarshal(body, &connectInfo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var tmp struct {
		ReturnValue struct {
			RunID      string `json:"agent_run_id"`
			EntityGuid string `json:"entity_guid"`
		} `json:"return_value"`
	}
	procAttributes := nr.extractProcessAttributes(connectInfo[0])

	if err := json.Unmarshal(blob, &tmp); err == nil {
		metadata := agentMeta{
			EntityGuid:        tmp.ReturnValue.EntityGuid,
			EntityName:        connectInfo[0].AppName[0],
			processAttributes: procAttributes,
		}
		if nr.useResourceHeader {
			metadataBytes, _ := json.Marshal(metadata)
			connectStruct.ReturnValue.RequestHeadersMap["new-relic-resource"] = base64.URLEncoding.EncodeToString(metadataBytes)
		} else {
			nr.entityGuids.Store(tmp.ReturnValue.RunID, &metadata)
		}

	} else {
		fmt.Println(err)
	}

	newBlob, err := json.Marshal(&connectStruct)
	if err != nil {
		fmt.Printf("Marshall Error: %v\n", err)
	}

	responseHeaders := w.Header()
	for headerKey, headerValues := range resp.Header {
		for _, headerValue := range headerValues {
			responseHeaders.Add(headerKey, headerValue)
		}
	}
	responseHeaders.Set("Content-Encoding", "identity")
	responseHeaders.Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(newBlob)
}

func (nr *NewRelicAgentReceiver) extractProcessAttributes(req ConnectInfo) processAttributes {
	processAttributes := processAttributes{}
	processAttributes.Pid = req.ProcessPid
	if req.Language == "java" {
		for _, att := range req.Env {
			switch att[0] = strings.ToLower(att[0].(string)); att[0] {
			case "java vm":
				processAttributes.RuntimeName = att[1].(string)
			case "java vm version":
				processAttributes.RuntimeVersion = att[1].(string)
			case "jvm arguments":
				processAttributes.CommandArgs = att[1].([]interface{})
			}
		}
	}
	return processAttributes
}

func (nr *NewRelicAgentReceiver) processMetricData(w http.ResponseWriter, r *http.Request, query url.Values) {
	fmt.Println("processMetricData")
	if nr.metricsConsumer == nil {
		return
	}

	ctx := r.Context()
	transportTag := transportType(query)
	obsrecv, _ := obsreport.NewReceiver(obsreport.ReceiverSettings{ReceiverID: nr.id, Transport: transportTag, ReceiverCreateSettings: nr.settings})
	ctx = obsrecv.StartTracesOp(ctx)

	requestBodyReader := processBodyIfNecessary(r)
	bodyBytes, _ := io.ReadAll(requestBodyReader)
	if c, ok := requestBodyReader.(io.Closer); ok {
		_ = c.Close()
	}
	var jsonBody []interface{}
	json.Unmarshal(bodyBytes, &jsonBody)
	//nrResourceHeader, _ := base64.URLEncoding.DecodeString(r.Header.Get("new-relic-resource"))
	//var agentResource map[string]interface{}
	//json.Unmarshal(nrResourceHeader, &agentResource)
	//fmt.Println("new-relic-resource")
	//fmt.Printf("AgentResource: %v\n", agentResource)
	//fmt.Println(jsonBody[0].(string))
	startTimeSeconds := jsonBody[1].(float64)
	endTimeSeconds := jsonBody[2].(float64)
	nrMetricData := jsonBody[3].([]interface{})

	metrics := pmetric.NewMetrics()
	startTime := pcommon.Timestamp(startTimeSeconds * 1000 * 1000 * 1000)
	endTime := pcommon.Timestamp(endTimeSeconds * 1000 * 1000 * 1000)

	resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
	resourceAttributes := resourceMetrics.Resource().Attributes()

	if nr.useResourceHeader {
		extractResourcesFromHeader(r, resourceAttributes)
	} else {
		nr.extractResourcesFromMap(jsonBody, resourceAttributes)
	}

	ilMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()
	userAgent := r.Header.Get("User-Agent")
	splitUserAgent := strings.SplitN(userAgent, "/", 2)
	ilMetrics.Scope().SetName(splitUserAgent[0])
	ilMetrics.Scope().SetVersion(splitUserAgent[1])

	otelMetrics := ilMetrics.Metrics()
	otelMetrics.EnsureCapacity(len(nrMetricData))

	TranslateMetrics(otelMetrics, nrMetricData, startTime, endTime)

	consumerErr := nr.metricsConsumer.ConsumeMetrics(ctx, metrics)
	obsrecv.EndTracesOp(ctx, "nragent", metrics.MetricCount(), consumerErr)

	if consumerErr != nil {
		// Transient error, due to some internal condition.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(errNextConsumerRespBody) // nolint:errcheck
		return
	}

	w.Header().Add("Content-Encoding", "identity")
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{"return_value":[]}`))
}

func (nr *NewRelicAgentReceiver) proxyRequest(w http.ResponseWriter, r *http.Request) {
	fmt.Println("proxyRequest")
	// we need to buffer the body if we want to read it here and send it
	// in the request.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// create a new url from the raw RequestURI sent by the client
	url := fmt.Sprintf("https://%s%s", nr.redirectHost, r.RequestURI)

	proxyReq, _ := http.NewRequestWithContext(r.Context(), r.Method, url, bytes.NewReader(body))

	// We may want to filter some headers, otherwise we could just use a shallow copy
	// proxyReq.Header = req.Header
	proxyReq.Header = make(http.Header)
	for h, val := range r.Header {
		proxyReq.Header[h] = val
	}
	proxyReq.Header.Set("Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	proxyReq.Header.Set("Content-Encoding", "identity")

	fmt.Printf("Found content-encoding %v", r.Header.Get("Content-Encoding"))
	fmt.Println()

	fmt.Printf("-- Forwarding request %+v", proxyReq)
	fmt.Println()

	resp, err := nr.httpClient.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	fmt.Printf("-- got response %+v", resp)
	fmt.Println()

	responseHeaders := w.Header()
	for headerKey, headerValues := range resp.Header {
		for _, headerValue := range headerValues {
			responseHeaders.Add(headerKey, headerValue)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (nr *NewRelicAgentReceiver) processSpanEventRequest(w http.ResponseWriter, r *http.Request, collectorHost string, query url.Values) {
	fmt.Println("processSpanEventRequest")
	if nr.tracesConsumer == nil {
		return
	}
	ctx := r.Context()
	transportTag := transportType(query)
	obsrecv, _ := obsreport.NewReceiver(obsreport.ReceiverSettings{ReceiverID: nr.id, Transport: transportTag, ReceiverCreateSettings: nr.settings})
	ctx = obsrecv.StartTracesOp(ctx)
	requestBodyReader := processBodyIfNecessary(r)
	bodyBytes, _ := io.ReadAll(requestBodyReader)
	if c, ok := requestBodyReader.(io.Closer); ok {
		_ = c.Close()
	}
	var jsonBody []interface{}
	json.Unmarshal(bodyBytes, &jsonBody)

	nrSpanEvents := jsonBody[2].([]interface{}) //[][]map[string]interface{}]

	traces := ptrace.NewTraces()

	resourceSpans := traces.ResourceSpans().AppendEmpty()
	resourceAttributes := resourceSpans.Resource().Attributes()

	if nr.useResourceHeader {
		extractResourcesFromHeader(r, resourceAttributes)
	} else {
		nr.extractResourcesFromMap(jsonBody, resourceAttributes)
	}

	ilSpans := resourceSpans.ScopeSpans().AppendEmpty()
	userAgent := r.Header.Get("User-Agent")
	splitUserAgent := strings.SplitN(userAgent, "/", 2)
	ilSpans.Scope().SetName(splitUserAgent[0])
	ilSpans.Scope().SetVersion(splitUserAgent[1])

	otelSpans := ilSpans.Spans()
	otelSpans.EnsureCapacity(len(nrSpanEvents))
	for i := 0; i < len(nrSpanEvents); i++ {
		span := otelSpans.AppendEmpty()

		spanAttributes := span.Attributes()
		nrAttributeGroup := nrSpanEvents[i].([]interface{})
		AddNRAttributesToOTelSpan(nrAttributeGroup[0].(map[string]interface{}), spanAttributes)
		AddNRAttributesToOTelSpan(nrAttributeGroup[1].(map[string]interface{}), spanAttributes)
		AddNRAttributesToOTelSpan(nrAttributeGroup[2].(map[string]interface{}), spanAttributes)

		// TODO: Delete NR attributes when rewritten to OTel equivalents.

		// TODO: Detect error.* attributes and rewrite to exception.*
		// TODO: Set otel.status_code to ERROR

		spanIdString, _ := getAndRemove(&spanAttributes, "guid")
		spanIdBytes, _ := hex.DecodeString(spanIdString.Str())
		var spanIdByteArray pcommon.SpanID
		copy(spanIdByteArray[:], spanIdBytes)
		span.SetSpanID(spanIdByteArray)

		if parentIdString, found := getAndRemove(&spanAttributes, "parentId"); found {
			parentIdBytes, _ := hex.DecodeString(parentIdString.Str())
			var parentIdByteArray pcommon.SpanID
			copy(parentIdByteArray[:], parentIdBytes)
			span.SetParentSpanID(parentIdByteArray)
		}

		traceIdString, _ := getAndRemove(&spanAttributes, "traceId")
		traceIdBytes, _ := hex.DecodeString(traceIdString.Str())
		var traceIdByteArray pcommon.TraceID
		copy(traceIdByteArray[:], traceIdBytes)
		span.SetTraceID(traceIdByteArray)

		name, ok := getAndRemove(&spanAttributes, "name")
		if ok {
			span.SetName(name.Str())
		} else {
			span.SetName("Unknown")
		}

		categoryAttribute, _ := getAndRemove(&spanAttributes, "category")
		switch categoryAttribute.Str() {
		case "generic":
			if _, found := spanAttributes.Get("nr.entryPoint"); found {
				span.SetKind(ptrace.SpanKindServer)
			} else {
				span.SetKind(ptrace.SpanKindInternal)
			}
		case "http":
			span.SetKind(ptrace.SpanKindClient)
		default:
			span.SetKind(ptrace.SpanKindInternal)
		}

		// TODO: Maybe use this if category is not present.
		spanAttributes.Remove("span.kind")

		// HTTP translations
		// httpResponseCode -> http.status_code
		// http.statusCode  -> http.status_code
		// request.method   -> http.method
		// request.headers.contentLength -> http.request_content_length
		// request.headers.userAgent -> http.user_agent
		// request.uri -> http.target
		// request.headers.host -> http.host

		if statusCode, found := getAndRemove(&spanAttributes, "httpResponseCode"); found {
			spanAttributes.PutStr("http.status_code", statusCode.Str())
		}

		if statusCode, found := getAndRemove(&spanAttributes, "http.statusCode"); found {
			spanAttributes.PutStr("http.status_code", statusCode.Str())
		}

		if method, found := getAndRemove(&spanAttributes, "request.method"); found {
			spanAttributes.PutStr("http.method", method.Str())
		}

		// TODO: Must also provide http.scheme and http.host.
		if uri, found := getAndRemove(&spanAttributes, "request.uri"); found {
			spanAttributes.PutStr("http.target", uri.Str())
		}

		if host, found := getAndRemove(&spanAttributes, "request.headers.host"); found {
			spanAttributes.PutStr("http.host", host.Str())
		}

		if contentLen, found := getAndRemove(&spanAttributes, "request.headers.contentLength"); found {
			spanAttributes.PutInt("http.request_content_length", contentLen.Int())
		}

		if userAgent, found := getAndRemove(&spanAttributes, "request.headers.userAgent"); found {
			spanAttributes.PutStr("http.user_agent", userAgent.Str())
		}

		startTime, _ := getAndRemove(&spanAttributes, "timestamp")
		span.SetStartTimestamp(pcommon.Timestamp(startTime.Int() * 1000 * 1000)) //convert from ms to ns
		duration, _ := getAndRemove(&spanAttributes, "duration")
		endTime := startTime.Int() + int64(duration.Double()*1000)
		span.SetEndTimestamp(pcommon.Timestamp(endTime * 1000 * 1000)) //convert ms to ns
	}
	consumerErr := nr.tracesConsumer.ConsumeTraces(ctx, traces)
	obsrecv.EndTracesOp(ctx, "nragent", traces.SpanCount(), consumerErr)
	if consumerErr != nil {
		// Transient error, due to some internal condition.
		w.WriteHeader(http.StatusInternalServerError)
		w.Write(errNextConsumerRespBody) // nolint:errcheck
		return
	}

	w.Header().Add("Content-Encoding", "identity")
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	w.Write([]byte(`{}`))

}

func AddNRAttributesToOTelSpan(nrSpanEventAttributeMap map[string]interface{}, spanAttributes pcommon.Map) {
	fmt.Println("AddNRAttributesToOTelSpan")
	for nrAttributeKey, nrAttributeValue := range nrSpanEventAttributeMap {
		switch attributeValue := nrAttributeValue.(type) {
		case string:
			spanAttributes.PutStr(nrAttributeKey, attributeValue)
		case int64:
			spanAttributes.PutInt(nrAttributeKey, attributeValue)
		case float64:
			if math.Floor(attributeValue) == attributeValue {
				spanAttributes.PutInt(nrAttributeKey, int64(attributeValue))
			} else {
				spanAttributes.PutDouble(nrAttributeKey, attributeValue)
			}
		case bool:
			spanAttributes.PutBool(nrAttributeKey, attributeValue)
		default:
			fmt.Printf("Got unexpected type %T for key %v and value %v", nrAttributeValue, nrAttributeKey, nrAttributeValue)
			fmt.Println()
		}
	}
}

func getAndRemove(spanAttributes *pcommon.Map, key string) (pcommon.Value, bool) {
	fmt.Println("getAndRemove")
	value, ok := spanAttributes.Get(key)
	if ok {
		tmp := pcommon.NewValueEmpty()
		value.CopyTo(tmp)
		value = tmp
		spanAttributes.Remove(key)
	}
	return value, ok
}

func extractResourcesFromHeader(r *http.Request, resourceAttributes pcommon.Map) {
	agentMetadata, _ := base64.URLEncoding.DecodeString(r.Header.Get("new-relic-resource"))
	var agentResource map[string]interface{}
	json.Unmarshal(agentMetadata, &agentResource)

	for k, v := range agentResource {
		switch attributeValue := v.(type) {
		case string:
			resourceAttributes.PutStr(k, attributeValue)
		case int64:
			resourceAttributes.PutInt(k, attributeValue)
		case float64:
			if math.Floor(attributeValue) == attributeValue {
				resourceAttributes.PutInt(k, int64(attributeValue))
			} else {
				resourceAttributes.PutDouble(k, attributeValue)
			}
		case bool:
			resourceAttributes.PutBool(k, attributeValue)
		case []interface{}:
			slice := resourceAttributes.PutEmptySlice(k)
			slice.FromRaw(attributeValue)
		default:
			fmt.Printf("Got unexpected type %T for key %v and value %v", v, k, v)
			fmt.Println()
		}
	}
}

func (nr *NewRelicAgentReceiver) extractResourcesFromMap(jsonBody []interface{}, resourceAttributes pcommon.Map) {
	runToken := jsonBody[0]
	if v, ok := nr.entityGuids.Load(runToken); ok {
		meta := v.(*agentMeta)
		resourceAttributes.PutStr("entity.guid", meta.EntityGuid)
		resourceAttributes.PutStr("service.name", meta.EntityName)
		resourceAttributes.PutInt("process.pid", int64(meta.Pid))
		resourceAttributes.PutStr("process.runtime.name", meta.RuntimeName)
		resourceAttributes.PutStr("process.runtime.version", meta.RuntimeVersion)
		commandArgSlice := resourceAttributes.PutEmptySlice("process.command_args")
		commandArgSlice.FromRaw(meta.CommandArgs)
	}
}

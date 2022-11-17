package nragentreceiver

type ConnectInfo struct {
	AppName    []string          `json:"app_name"`
	HostName   string            `json:"host"`
	Metadata   map[string]string `json:"metadata"`
	Language   string            `json:"language"`
	ProcessPid int               `json:"pid"`
	Env        [][]interface{}   `json:"environment"`
	Settings   struct {
		AppName string `json:"app_name"`
	}
}

// ConnectReply contains all of the settings and state send down from the
// collector.  It should not be modified after creation.
type ConnectReply struct {
	RunID                 string            `json:"agent_run_id"`
	DataReportPeriod      int               `json:"data_report_period"`
	RequestHeadersMap     map[string]string `json:"request_headers_map"`
	MaxPayloadSizeInBytes int               `json:"max_payload_size_in_bytes"`
	EntityGUID            string            `json:"entity_guid"`

	// Transaction Name Modifiers
	SegmentTerms []interface{} `json:"transaction_segment_terms"`
	TxnNameRules []interface{} `json:"transaction_name_rules"`
	URLRules     []interface{} `json:"url_rules"`
	MetricRules  []interface{} `json:"metric_name_rules"`

	// Cross Process
	EncodingKey     string `json:"encoding_key"`
	CrossProcessID  string `json:"cross_process_id"`
	TrustedAccounts []int  `json:"trusted_account_ids"`

	// Settings
	KeyTxnApdex            map[string]float64 `json:"web_transactions_apdex"`
	ApdexThresholdSeconds  float64            `json:"apdex_t"`
	CollectAnalyticsEvents bool               `json:"collect_analytics_events"`
	CollectCustomEvents    bool               `json:"collect_custom_events"`
	CollectTraces          bool               `json:"collect_traces"`
	CollectErrors          bool               `json:"collect_errors"`
	CollectErrorEvents     bool               `json:"collect_error_events"`
	CollectSpanEvents      bool               `json:"collect_span_events"`

	// RUM
	AgentLoader string `json:"js_agent_loader"`
	Beacon      string `json:"beacon"`
	BrowserKey  string `json:"browser_key"`
	AppID       string `json:"application_id"`
	ErrorBeacon string `json:"error_beacon"`
	JSAgentFile string `json:"js_agent_file"`

	Messages []struct {
		Message string `json:"message"`
		Level   string `json:"level"`
	} `json:"messages"`

	// BetterCAT/Distributed Tracing
	AccountID                     string `json:"account_id"`
	TrustedAccountKey             string `json:"trusted_account_key"`
	PrimaryAppID                  string `json:"primary_application_id"`
	SamplingTarget                uint64 `json:"sampling_target"`
	SamplingTargetPeriodInSeconds int    `json:"sampling_target_period_in_seconds"`

	ServerSideConfig struct {
		TransactionTracerEnabled *bool `json:"transaction_tracer.enabled"`
		// TransactionTracerThreshold should contain either a number or
		// "apdex_f" if it is non-nil.
		TransactionTracerThreshold           interface{} `json:"transaction_tracer.transaction_threshold"`
		TransactionTracerStackTraceThreshold *float64    `json:"transaction_tracer.stack_trace_threshold"`
		ErrorCollectorEnabled                *bool       `json:"error_collector.enabled"`
		ErrorCollectorIgnoreStatusCodes      []int       `json:"error_collector.ignore_status_codes"`
		CrossApplicationTracerEnabled        *bool       `json:"cross_application_tracer.enabled"`
	} `json:"agent_config"`

	// Faster Event Harvest
	EventData EventHarvestConfig `json:"event_harvest_config"`
}
type ReplyReturnValue struct {
	ReturnValue struct {
		ConnectReply
	} `json:"return_value"`
}

// EventHarvestConfig contains fields relating to faster event harvest.
// This structure is used in the connect request (to send up defaults)
// and in the connect response (to get the server values).
//
// https://source.datanerd.us/agents/agent-specs/blob/master/Connect-LEGACY.md#event_harvest_config-hash
// https://source.datanerd.us/agents/agent-specs/blob/master/Connect-LEGACY.md#event-harvest-config
type EventHarvestConfig struct {
	ReportPeriodMs int `json:"report_period_ms,omitempty"`
	Limits         struct {
		TxnEvents    *uint `json:"analytic_event_data,omitempty"`
		CustomEvents *uint `json:"custom_event_data,omitempty"`
		ErrorEvents  *uint `json:"error_event_data,omitempty"`
		SpanEvents   *uint `json:"span_event_data,omitempty"`
	} `json:"harvest_limits"`
}

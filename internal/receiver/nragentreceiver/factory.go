package nragentreceiver

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	//"me.localhost/newrelic-ot-collector/internal/sharedcomponent"
)

// This file implements factory for Zipkin receiver.

const (
	typeStr   = "nragent"
	stability = component.StabilityLevelBeta

	defaultBindEndpoint = "0.0.0.0:9411"
)

// NewFactory creates a new New Relic agent receiver factory
func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		typeStr,
		createDefaultConfig,
		receiver.WithTraces(createTracesReceiver, stability),
		receiver.WithMetrics(createMetricsReceiver, stability),
	)
}

// createDefaultConfig creates the default configuration for Zipkin receiver.
func createDefaultConfig() component.Config {
	return &Config{
		//ReceiverSettings: config.NewReceiverSettings(config.NewComponentID(typeStr)),
		HTTPServerSettings: confighttp.HTTPServerSettings{
			Endpoint: defaultBindEndpoint,
		},
	}
}

// createTracesReceiver creates a trace receiver based on provided config.
func createTracesReceiver(
	_ context.Context,
	set receiver.CreateSettings,
	cfg component.Config,
	nextConsumer consumer.Traces,
) (receiver.Traces, error) {
	r := receivers.GetOrAdd(cfg, func() component.Component {
		return New(cfg.(*Config), set)
	})

	if err := r.Unwrap().(*NewRelicAgentReceiver).registerTracesConsumer(nextConsumer); err != nil {
		return nil, err
	}
	return r, nil
}

// CreateMetricsReceiver creates a metrics receiver based on provided config.
func createMetricsReceiver(
	_ context.Context,
	set receiver.CreateSettings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (receiver.Metrics, error) {
	r := receivers.GetOrAdd(cfg, func() component.Component {
		return New(cfg.(*Config), set)
	})

	if err := r.Unwrap().(*NewRelicAgentReceiver).registerMetricsConsumer(nextConsumer); err != nil {
		return nil, err
	}
	return r, nil
}

// This is the map of already created OTLP receivers for particular configurations.
// We maintain this map because the Factory is asked trace and metric receivers separately
// when it gets CreateTracesReceiver() and CreateMetricsReceiver() but they must not
// create separate objects, they must use one otlpReceiver object per configuration.
// When the receiver is shutdown it should be removed from this map so the same configuration
// can be recreated successfully.
var receivers = NewSharedComponents()

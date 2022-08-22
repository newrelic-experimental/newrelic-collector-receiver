package main

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer/consumererror"
	"go.opentelemetry.io/collector/exporter/loggingexporter"
	"go.opentelemetry.io/collector/exporter/otlpexporter"
	"go.opentelemetry.io/collector/extension/pprofextension"
	"go.opentelemetry.io/collector/extension/zpagesextension"
	"go.opentelemetry.io/collector/receiver/otlpreceiver"
	"me.localhost/newrelic-ot-collector/internal/receiver/nragentreceiver"
)

func components() (component.Factories, error) {
	var errs []error
	extensions, err := component.MakeExtensionFactoryMap(
		pprofextension.NewFactory(),
		zpagesextension.NewFactory(),
	)
	if err != nil {
		errs = append(errs, err)
	}
	receivers, err := component.MakeReceiverFactoryMap(
		otlpreceiver.NewFactory(),
		nragentreceiver.NewFactory(),
	)
	if err != nil {
		errs = append(errs, err)
	}
	exporters, err := component.MakeExporterFactoryMap(
		loggingexporter.NewFactory(),
		otlpexporter.NewFactory(),
	)
	if err != nil {
		errs = append(errs, err)
	}
	factories := component.Factories{
		Extensions: extensions,
		Receivers:  receivers,
		Processors: nil,
		Exporters:  exporters,
	}

	return factories, consumererror.Combine(errs)
}

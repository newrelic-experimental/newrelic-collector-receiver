package main

import (
	"log"
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/service"
	"me.localhost/newrelic-ot-collector/internal/version"
)

func main() {
	factories, err := components()
	if err != nil {
		log.Fatalf("failed to build components: %v", err)
	}

	info := component.BuildInfo{
		Command:     "newrelic-otel-ingest",
		Description: "OpenTelemetry Ingest -- New Relic",
		Version:     version.Version,
	}

	var level zapcore.Level
	logLevel := os.Getenv("LOG_LEVEL")
	if err := (&level).UnmarshalText([]byte(logLevel)); err != nil {
		log.Fatal(err)
	}

	if err := run(service.CollectorSettings{
		BuildInfo:      info,
		Factories:      factories,
		LoggingOptions: []zap.Option{zap.IncreaseLevel(level)},
	}); err != nil {
		log.Fatal(err)
	}
}
func run(params service.CollectorSettings) error {
	cmd := service.NewCommand(params)
	if err := cmd.Execute(); err != nil {
		log.Fatalf("collector server run finished with error: %v", err)
	}
	//app, err := service.New(params)
	//if err != nil {
	//	return fmt.Errorf("failed to construct the application: %w", err)
	//}
	//
	//err = app.Run()
	//if err != nil {
	//	return fmt.Errorf("application run finished with error: %w", err)
	//}

	return nil
}

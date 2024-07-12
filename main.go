package main

import (
	"context"
	"github.com/danthegoodman1/EpicEpoch/gologger"
	"github.com/danthegoodman1/EpicEpoch/http_server"
	"github.com/danthegoodman1/EpicEpoch/raft"
	"os"
	"os/signal"
	"syscall"
	"time"
)

var logger = gologger.NewLogger()

func main() {
	logger.Debug().Msg("starting epic epoch api")

	// start raft
	nodeHost, err := raft.StartRaft()
	if err != nil {
		logger.Error().Err(err).Msg("raft couldn't start")
		os.Exit(1)
	}

	// prometheusReporter := observability.NewPrometheusReporter()
	// go func() {
	// 	err := observability.StartInternalHTTPServer(":8042", prometheusReporter)
	// 	if err != nil && !errors.Is(err, http.ErrServerClosed) {
	// 		logger.Error().Err(err).Msg("api server couldn't start")
	// 		os.Exit(1)
	// 	}
	// }()

	httpServer := http_server.StartHTTPServer(nodeHost)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Warn().Msg("received shutdown signal!")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		logger.Error().Err(err).Msg("failed to shutdown HTTP server")
	} else {
		logger.Info().Msg("successfully shutdown HTTP server")
	}

	nodeHost.Stop()
}

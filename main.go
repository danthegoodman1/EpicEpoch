package main

import (
	"github.com/danthegoodman1/EpicEpoch/gologger"
	"github.com/danthegoodman1/EpicEpoch/raft"
	"os"
	"os/signal"
	"syscall"
)

var logger = gologger.NewLogger()

func main() {
	logger.Debug().Msg("starting epic epoch api")

	// prometheusReporter := observability.NewPrometheusReporter()
	// go func() {
	// 	err := observability.StartInternalHTTPServer(":8042", prometheusReporter)
	// 	if err != nil && !errors.Is(err, http.ErrServerClosed) {
	// 		logger.Error().Err(err).Msg("api server couldn't start")
	// 		os.Exit(1)
	// 	}
	// }()

	// httpServer := http_server.StartHTTPServer()
	nodeHost, err := raft.StartRaft()
	if err != nil {
		logger.Error().Err(err).Msg("raft couldn't start")
		os.Exit(1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	logger.Warn().Msg("received shutdown signal!")

	nodeHost.Stop()

	// ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	// defer cancel()
	// if err := httpServer.Shutdown(ctx); err != nil {
	// 	logger.Error().Err(err).Msg("failed to shutdown HTTP server")
	// } else {
	// 	logger.Info().Msg("successfully shutdown HTTP server")
	// }
}

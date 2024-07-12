package utils

import "os"

var (
	Env                    = os.Getenv("ENV")
	Env_TracingServiceName = os.Getenv("TRACING_SERVICE_NAME")
	Env_OLTPEndpoint       = os.Getenv("OLTP_ENDPOINT")

	NodeID = uint64(GetEnvOrDefaultInt("NODE_ID", 0))

	TimestampRequestBuffer = uint64(GetEnvOrDefaultInt("TIMESTAMP_REQUEST_BUFFER", 10000))
	EpochIntervalMS        = uint64(GetEnvOrDefaultInt("EPOCH_INTERVAL_MS", 100))

	EpochIntervalDeadlineLimit = GetEnvOrDefaultInt("EPOCH_DEADLINE_LIMIT", 100)
)

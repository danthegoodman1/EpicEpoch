package utils

import "os"

var (
	Env                    = os.Getenv("ENV")
	Env_TracingServiceName = os.Getenv("TRACING_SERVICE_NAME")
	Env_OLTPEndpoint       = os.Getenv("OLTP_ENDPOINT")

	NodeID = uint64(GetEnvOrDefaultInt("NODE_ID", 0))
)

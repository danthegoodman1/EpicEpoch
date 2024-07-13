package http_server

import (
	"context"
	"errors"
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/raft"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/danthegoodman1/EpicEpoch/gologger"
	"github.com/danthegoodman1/EpicEpoch/utils"
	"github.com/go-playground/validator/v10"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/rs/zerolog"
	"golang.org/x/net/http2"
)

var logger = gologger.NewLogger()

type HTTPServer struct {
	Echo      *echo.Echo
	EpochHost *raft.EpochHost
}

type CustomValidator struct {
	validator *validator.Validate
}

func StartHTTPServer(epochHost *raft.EpochHost) *HTTPServer {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", utils.GetEnvOrDefault("HTTP_PORT", "8080")))
	if err != nil {
		logger.Error().Err(err).Msg("error creating tcp listener, exiting")
		os.Exit(1)
	}
	s := &HTTPServer{
		Echo:      echo.New(),
		EpochHost: epochHost,
	}
	s.Echo.HideBanner = true
	s.Echo.HidePort = true
	s.Echo.JSONSerializer = &utils.NoEscapeJSONSerializer{}
	s.Echo.HTTPErrorHandler = customHTTPErrorHandler

	s.Echo.Use(CreateReqContext)
	s.Echo.Use(LoggerMiddleware)
	s.Echo.Use(middleware.CORS())
	s.Echo.Validator = &CustomValidator{validator: validator.New()}

	s.Echo.GET("/up", s.UpCheck)
	s.Echo.GET("/ready", s.ReadyCheck)
	s.Echo.GET("/timestamp", s.GetTimestamp)
	s.Echo.GET("/membership", s.GetMembership)

	s.Echo.Listener = listener
	go func() {
		logger.Info().Msg("starting h2c server on " + listener.Addr().String())
		err := s.Echo.StartH2CServer("", &http2.Server{})
		// stop the broker
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error().Err(err).Msg("failed to start h2c server, exiting")
			os.Exit(1)
		}
	}()

	return s
}

func (cv *CustomValidator) Validate(i interface{}) error {
	if err := cv.validator.Struct(i); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return nil
}

func ValidateRequest(c echo.Context, s interface{}) error {
	if err := c.Bind(s); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	// needed because POST doesn't have query param binding (https://echo.labstack.com/docs/binding#multiple-sources)
	if err := (&echo.DefaultBinder{}).BindQueryParams(c, s); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if err := c.Validate(s); err != nil {
		return err
	}
	return nil
}

// UpCheck is just whether the HTTP server is running,
// not necessarily that it's able to serve requests
func (s *HTTPServer) UpCheck(c echo.Context) error {
	return c.String(http.StatusOK, "ok")
}

// ReadyCheck checks whether everything is ready to start serving requests
func (s *HTTPServer) ReadyCheck(c echo.Context) error {
	ctx := c.Request().Context()
	logger := zerolog.Ctx(ctx)
	// Verify that raft leadership information is available
	leader, available, err := s.EpochHost.GetLeader()
	if err != nil {
		return fmt.Errorf("error in NodeHost.GetLeaderID: %w", err)
	}

	if !available {
		return c.String(http.StatusInternalServerError, "raft leadership not ready")
	}

	if leader == utils.NodeID {
		logger.Debug().Msgf("Is leader (%d)", leader)
	}

	return c.String(http.StatusOK, fmt.Sprintf("leader=%d nodeID=%d raftAvailable=%t\n", leader, utils.NodeID, available))
}

func (s *HTTPServer) GetTimestamp(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), time.Second)
	defer cancel()
	// Verify that this is the raft leader
	leader, available, err := s.EpochHost.GetLeader()
	if err != nil {
		return fmt.Errorf("error in NodeHost.GetLeaderID: %w", err)
	}

	if !available {
		return c.String(http.StatusInternalServerError, "raft leadership not ready")
	}

	if leader != utils.NodeID {
		// TODO: redirect
		return c.String(http.StatusConflict, fmt.Sprintf("This (%d) is not the leader (%d)", utils.NodeID, leader))
	}

	// Get a timestamp
	timestamp, err := s.EpochHost.GetUniqueTimestamp(ctx)
	if err != nil {
		return fmt.Errorf("error in EpochHost.GetUniqueTimestamp: %w", err)
	}

	return c.Blob(http.StatusOK, "application/octet-stream", timestamp)
}

func (s *HTTPServer) GetMembership(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), time.Second)
	defer cancel()

	membership, err := s.EpochHost.GetMembership(ctx)
	if err != nil {
		return fmt.Errorf("error in EpochHost.GetMembership: %w", err)
	}

	return c.JSON(http.StatusOK, membership)
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	err := s.Echo.Shutdown(ctx)
	return err
}

func LoggerMiddleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		start := time.Now()
		if err := next(c); err != nil {
			// default handler
			c.Error(err)
		}
		stop := time.Since(start)
		// Log otherwise
		logger := zerolog.Ctx(c.Request().Context())
		req := c.Request()
		res := c.Response()

		p := req.URL.Path
		if p == "" {
			p = "/"
		}

		cl := req.Header.Get(echo.HeaderContentLength)
		if cl == "" {
			cl = "0"
		}
		logger.Debug().Str("method", req.Method).Str("remote_ip", c.RealIP()).Str("req_uri", req.RequestURI).Str("handler_path", c.Path()).Str("path", p).Int("status", res.Status).Int64("latency_ns", int64(stop)).Str("protocol", req.Proto).Str("bytes_in", cl).Int64("bytes_out", res.Size).Msg("req recived")
		return nil
	}
}

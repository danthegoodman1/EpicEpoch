package http_server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"github.com/danthegoodman1/EpicEpoch/raft"
	"github.com/quic-go/quic-go/http3"
	"math/big"
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
	Echo       *echo.Echo
	EpochHost  *raft.EpochHost
	quicServer *http3.Server
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
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error().Err(err).Msg("failed to start h2c server, exiting")
			os.Exit(1)
		}
	}()

	// Start http/3 server
	go func() {
		tlsCert, err := generateTLSCert()
		if err != nil {
			logger.Fatal().Err(err).Msg("failed to generate self-signed cert")
		}

		// TLS configuration
		tlsConfig := &tls.Config{
			Certificates: []tls.Certificate{tlsCert},
			NextProtos:   []string{"h3"},
		}

		// Create HTTP/3 server
		s.quicServer = &http3.Server{
			Addr:      ":443",
			Handler:   s.Echo,
			TLSConfig: tlsConfig,
		}

		logger.Info().Msg("starting h3 server on " + listener.Addr().String())
		err = s.quicServer.ListenAndServe()

		// Start the server
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
		membership, err := s.EpochHost.GetMembership(ctx)
		if err != nil {
			return fmt.Errorf("error in EpochHost.GetMembership: %w", err)
		}

		return c.Redirect(http.StatusPermanentRedirect, membership.Leader.Addr)
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
	err := s.quicServer.Close()
	if err != nil {
		return fmt.Errorf("error in quicServer.Close: %w", err)
	}

	err = s.Echo.Shutdown(ctx)
	if err != nil {
		return fmt.Errorf("error shutting down echo: %w", err)
	}

	return nil
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

const (
	certFile = "cert.pem"
	keyFile  = "key.pem"
)

func generateTLSCert() (tls.Certificate, error) {
	// Check if certificate and key files exist
	if fileExists(certFile) && fileExists(keyFile) {
		// Load existing certificate and key
		return tls.LoadX509KeyPair(certFile, keyFile)
	}

	// Generate a new certificate and key
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Example Co"},
		},
		NotBefore: time.Now(),
		NotAfter:  time.Now().Add(time.Hour * 24 * 180), // Valid for 180 days
		KeyUsage:  x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Save the certificate
	certOut, err := os.Create(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	certOut.Close()

	// Save the key
	keyOut, err := os.Create(keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyBytes, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	keyOut.Close()

	// Load the newly created certificate and key
	return tls.LoadX509KeyPair(certFile, keyFile)
}

func fileExists(filename string) bool {
	_, err := os.Stat(filename)
	return !os.IsNotExist(err)
}

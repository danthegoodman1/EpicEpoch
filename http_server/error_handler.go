package http_server

import (
	"errors"
	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"net/http"
)

func customHTTPErrorHandler(err error, c echo.Context) {
	var he *echo.HTTPError
	if errors.As(err, &he) {
		c.String(he.Code, he.Message.(string))
		return
	}

	logger := zerolog.Ctx(c.Request().Context())
	logger.Error().Err(err).Msg("unhandled internal error")

	c.String(http.StatusInternalServerError, "Something went wrong internally, an error has been logged")
}

package v1

import (
	"strings"

	"github.com/labstack/echo/v5"
)

func bindBody(c *echo.Context, target any) error {
	req := c.Request()
	contentType := req.Header.Get(echo.HeaderContentType)
	base, _, _ := strings.Cut(contentType, ";")
	mediaType := strings.ToLower(strings.TrimSpace(base))
	normalizedType := mediaType + contentType[len(base):]

	if strings.HasSuffix(mediaType, "json") {
		normalizedType = echo.MIMEApplicationJSON + contentType[len(base):]
	}

	if normalizedType != contentType {
		req.Header.Set(echo.HeaderContentType, normalizedType)
		defer req.Header.Set(echo.HeaderContentType, contentType)
	}

	if mediaType == echo.MIMEApplicationForm || mediaType == echo.MIMEMultipartForm {
		// Form binding reads only the body, even when the query is malformed.
		requestURL := req.URL
		bodyURL := *requestURL
		bodyURL.RawQuery = ""
		req.URL = &bodyURL

		defer func() { req.URL = requestURL }()
	}

	return echo.BindBody(c, target)
}

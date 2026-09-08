package httpserver

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/labstack/echo/v5"
)

// Wrap the resolved handler so global logging and metrics also see rejected bodies.
type bodyLimitRouter struct{ echo.Router }

func (r bodyLimitRouter) Route(c *echo.Context) echo.HandlerFunc {
	return bodyLimit(r.Router.Route(c))
}

func bodyLimit(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		req := c.Request()
		if req.ContentLength > _defaultBodyLimit {
			return echo.ErrStatusRequestEntityTooLarge
		}

		defer req.Body.Close()

		body, err := readBody(req.Body)
		if err != nil {
			return err
		}

		// Preserve the existing decoding order for stacked content encodings.
		for encoding := range strings.SplitSeq(req.Header.Get(echo.HeaderContentEncoding), ",") {
			body, err = decodeBody(body, strings.TrimSpace(encoding))
			if err != nil {
				return err
			}
		}

		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))

		return next(c)
	}
}

// Read through the end before handlers can accept a partial document, bounding
// both the wire body and each decompression stage to prevent expansion attacks.
func readBody(reader io.Reader) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, _defaultBodyLimit+1))
	if len(body) > _defaultBodyLimit {
		return nil, echo.ErrStatusRequestEntityTooLarge
	}

	if err != nil {
		return nil, echo.ErrBadRequest.Wrap(err)
	}

	return body, nil
}

func decodeBody(body []byte, encoding string) ([]byte, error) {
	var (
		reader io.ReadCloser
		err    error
	)

	switch encoding {
	case "gzip":
		reader, err = gzip.NewReader(bytes.NewReader(body))
	case "deflate":
		reader, err = zlib.NewReader(bytes.NewReader(body))
	case "br", "brotli":
		reader = io.NopCloser(brotli.NewReader(bytes.NewReader(body)))
	default:
		return body, nil
	}

	if err != nil {
		return nil, echo.ErrBadRequest.Wrap(err)
	}

	defer reader.Close()

	return readBody(reader)
}

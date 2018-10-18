package apm

import (
	"fmt"
	"net/http"
	"strings"

	"go.elastic.co/apm/internal/apmhttputil"
	"go.elastic.co/apm/model"
)

// Context provides methods for setting transaction and error context.
type Context struct {
	model            model.Context
	request          model.Request
	requestBody      model.RequestBody
	requestHeaders   model.RequestHeaders
	requestSocket    model.RequestSocket
	response         model.Response
	responseHeaders  model.ResponseHeaders
	user             model.User
	service          model.Service
	serviceFramework model.Framework
	captureBodyMask  CaptureBodyMode
}

func (c *Context) build() *model.Context {
	switch {
	case c.model.Request != nil:
	case c.model.Response != nil:
	case c.model.User != nil:
	case c.model.Service != nil:
	case len(c.model.Custom) != 0:
	case len(c.model.Tags) != 0:
	default:
		return nil
	}
	return &c.model
}

func (c *Context) reset() {
	modelContext := model.Context{
		// TODO(axw) reuse space for tags
		Custom: c.model.Custom[:0],
	}
	*c = Context{
		model:           modelContext,
		captureBodyMask: c.captureBodyMask,
	}
}

// SetCustom sets a custom context key/value pair. If the key is invalid
// (contains '.', '*', or '"'), the call is a no-op. The value must be
// JSON-encodable.
//
// If value implements interface{AppendJSON([]byte) []byte}, that will be
// used to encode the value. Otherwise, value will be encoded using
// json.Marshal. As a special case, values of type map[string]interface{}
// will be traversed and values encoded according to the same rules.
func (c *Context) SetCustom(key string, value interface{}) {
	if !validTagKey(key) {
		return
	}
	c.model.Custom.Set(key, value)
}

// SetTag sets a tag in the context. If the key is invalid
// (contains '.', '*', or '"'), the call is a no-op.
func (c *Context) SetTag(key, value string) {
	if !validTagKey(key) {
		return
	}
	value = truncateKeyword(value)
	if c.model.Tags == nil {
		c.model.Tags = map[string]string{key: value}
	} else {
		c.model.Tags[key] = value
	}
}

// SetFramework sets the framework name and version in the context.
//
// This is used for identifying the framework in which the context
// was created, such as Gin or Echo.
//
// If the name is empty, this is a no-op. If version is empty, then
// it will be set to "unspecified".
func (c *Context) SetFramework(name, version string) {
	if name == "" {
		return
	}
	if version == "" {
		// Framework version is required.
		version = "unspecified"
	}
	c.serviceFramework = model.Framework{
		Name:    truncateKeyword(name),
		Version: truncateKeyword(version),
	}
	c.service.Framework = &c.serviceFramework
	c.model.Service = &c.service
}

// SetHTTPRequest sets details of the HTTP request in the context.
//
// This function relates to server-side requests. Various proxy
// forwarding headers are taken into account to reconstruct the URL,
// and determining the client address.
//
// If the request URL contains user info, it will be removed and
// excluded from the URL's "full" field.
//
// If the request contains HTTP Basic Authentication, the username
// from that will be recorded in the context. Otherwise, if the
// request contains user info in the URL (i.e. a client-side URL),
// that will be used.
func (c *Context) SetHTTPRequest(req *http.Request) {
	// Special cases to avoid calling into fmt.Sprintf in most cases.
	var httpVersion string
	switch {
	case req.ProtoMajor == 1 && req.ProtoMinor == 1:
		httpVersion = "1.1"
	case req.ProtoMajor == 2 && req.ProtoMinor == 0:
		httpVersion = "2.0"
	default:
		httpVersion = fmt.Sprintf("%d.%d", req.ProtoMajor, req.ProtoMinor)
	}

	var forwarded *apmhttputil.ForwardedHeader
	if fwd := req.Header.Get("Forwarded"); fwd != "" {
		parsed := apmhttputil.ParseForwarded(fwd)
		forwarded = &parsed
	}
	c.request = model.Request{
		Body:        c.request.Body,
		URL:         apmhttputil.RequestURL(req, forwarded),
		Method:      truncateKeyword(req.Method),
		HTTPVersion: httpVersion,
		Cookies:     req.Cookies(),
	}
	c.model.Request = &c.request

	c.requestHeaders = model.RequestHeaders{
		ContentType: req.Header.Get("Content-Type"),
		Cookie:      truncateText(strings.Join(req.Header["Cookie"], ";")),
		UserAgent:   req.UserAgent(),
	}
	if c.requestHeaders != (model.RequestHeaders{}) {
		c.request.Headers = &c.requestHeaders
	}

	c.requestSocket = model.RequestSocket{
		Encrypted:     req.TLS != nil,
		RemoteAddress: apmhttputil.RemoteAddr(req, forwarded),
	}
	if c.requestSocket != (model.RequestSocket{}) {
		c.request.Socket = &c.requestSocket
	}

	username, _, ok := req.BasicAuth()
	if !ok && req.URL.User != nil {
		username = req.URL.User.Username()
	}
	c.user.Username = truncateKeyword(username)
	if c.user.Username != "" {
		c.model.User = &c.user
	}
}

// SetHTTPRequestBody sets the request body in context given a (possibly nil)
// BodyCapturer returned by Tracer.CaptureHTTPRequestBody.
func (c *Context) SetHTTPRequestBody(bc *BodyCapturer) {
	if bc == nil || bc.captureBody&c.captureBodyMask == 0 {
		return
	}
	if bc.setContext(&c.requestBody) {
		c.request.Body = &c.requestBody
	}
}

// SetHTTPResponseHeaders sets the HTTP response headers in the context.
func (c *Context) SetHTTPResponseHeaders(h http.Header) {
	c.responseHeaders.ContentType = h.Get("Content-Type")
	if c.responseHeaders.ContentType != "" {
		c.response.Headers = &c.responseHeaders
		c.model.Response = &c.response
	}
}

// SetHTTPStatusCode records the HTTP response status code.
func (c *Context) SetHTTPStatusCode(statusCode int) {
	c.response.StatusCode = statusCode
	c.model.Response = &c.response
}

// SetUserID sets the ID of the authenticated user.
func (c *Context) SetUserID(id string) {
	c.user.ID = truncateKeyword(id)
	if c.user.ID != "" {
		c.model.User = &c.user
	}
}

// SetUserEmail sets the email for the authenticated user.
func (c *Context) SetUserEmail(email string) {
	c.user.Email = truncateKeyword(email)
	if c.user.Email != "" {
		c.model.User = &c.user
	}
}

// SetUsername sets the username of the authenticated user.
func (c *Context) SetUsername(username string) {
	c.user.Username = truncateKeyword(username)
	if c.user.Username != "" {
		c.model.User = &c.user
	}
}

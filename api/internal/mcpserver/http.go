package mcpserver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/Learistocats/LookupKB/api/internal/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaxBodyBytes caps one JSON-RPC request (enforced by the SDK transport).
const MaxBodyBytes = 64 << 10

// Handler serves the MCP endpoint as a Fiber handler.
type Handler struct {
	http           http.Handler
	allowedOrigins map[string]struct{}
}

// Options configure the endpoint.
type Options struct {
	// Version is reported as serverInfo.version.
	Version string
	// AllowedOrigins are the browser origins (scheme://host[:port]) allowed to
	// call the endpoint. A request carrying any other Origin header is refused
	// with 403 (the transport spec's DNS-rebinding guidance). Native MCP clients
	// send no Origin and are unaffected. Empty ⇒ every browser origin is refused.
	AllowedOrigins []string
	Logger         *slog.Logger
}

// New builds the MCP endpoint over the read services.
func New(cat Catalog, srch Searcher, tagStore Tags, opts Options) (*Handler, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	srv, err := newMCPServer(&tools{catalog: cat, search: srch, tags: tagStore, log: log}, opts.Version)
	if err != nil {
		return nil, err
	}
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, &mcp.StreamableHTTPOptions{
		// Stateless + JSON: one application/json answer per POST; no SSE (GET → 405).
		Stateless:           true,
		JSONResponse:        true,
		MaxRequestBodyBytes: MaxBodyBytes,
		Logger:              log,
	})
	allowed := make(map[string]struct{}, len(opts.AllowedOrigins))
	for _, o := range opts.AllowedOrigins {
		if o = strings.TrimRight(strings.TrimSpace(o), "/"); o != "" {
			allowed[strings.ToLower(o)] = struct{}{}
		}
	}
	return &Handler{http: h, allowedOrigins: allowed}, nil
}

// Handle bridges a Fiber request to the SDK's net/http handler. It must run after
// auth.Middleware.Handle: the verified Identity is moved onto the converted
// request's context explicitly (adaptor does not carry Locals or UserContext
// across), so every tool runs as the caller. The response is buffered and copied
// back; nothing here streams.
func (h *Handler) Handle(c *fiber.Ctx) error {
	if origin := c.Get(fiber.HeaderOrigin); origin != "" {
		if _, ok := h.allowedOrigins[strings.ToLower(strings.TrimRight(origin, "/"))]; !ok {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{"error": "origin not allowed"})
		}
	}
	id, ok := auth.IdentityFromCtx(c)
	if !ok {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "unauthorized"})
	}
	req, err := adaptor.ConvertRequest(c, true)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid request"})
	}
	req = req.WithContext(auth.ContextWithIdentity(c.UserContext(), id))

	w := &bufferedResponse{header: http.Header{}, status: http.StatusOK}
	h.http.ServeHTTP(w, req)
	w.jsonRPCErrorBody()

	for k, vs := range w.header {
		if strings.EqualFold(k, fiber.HeaderContentLength) {
			continue // fasthttp sets it from the body
		}
		for i, v := range vs {
			if i == 0 {
				c.Set(k, v)
			} else {
				c.Append(k, v)
			}
		}
	}
	return c.Status(w.status).Send(w.body.Bytes())
}

// bufferedResponse is a minimal http.ResponseWriter collecting the SDK's answer.
type bufferedResponse struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (w *bufferedResponse) Header() http.Header { return w.header }

func (w *bufferedResponse) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
}

func (w *bufferedResponse) Write(b []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.body.Write(b)
}

// codeInvalidRequest is the JSON-RPC 2.0 "Invalid Request" error code.
const codeInvalidRequest = -32600

// jsonRPCErrorBody turns the SDK's plain-text 400 (a request it refused before
// dispatch) into a JSON-RPC error response with a null id. The status stays 400,
// as the transport spec requires. Any other response passes through untouched.
func (w *bufferedResponse) jsonRPCErrorBody() {
	if w.status != http.StatusBadRequest || !strings.HasPrefix(w.header.Get("Content-Type"), "text/plain") {
		return
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      nil,
		"error":   map[string]any{"code": codeInvalidRequest, "message": strings.TrimSpace(w.body.String())},
	})
	if err != nil {
		return
	}
	w.body.Reset()
	w.body.Write(body)
	w.header.Set("Content-Type", "application/json")
	w.header.Del("X-Content-Type-Options")
}

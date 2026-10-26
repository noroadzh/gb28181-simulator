package app

// Small SIP-layer helpers shared by the REGISTER handler, the INVITE
// handler, and the catalog/subscribe flows.
//
// They were originally inlined in acceptor.go but were promoted here when
// that file outgrew a single screenful; keeping them in one place makes it
// easier to reason about how the Allow / Date headers interact with the
// profile types declared in the same package.

import (
	"strings"
	"time"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// allowedMethods is the Allow header value this platform emits. RFC 3261 §8.2.2
// requires a 405 response to carry one so the client knows what to retry.
var allowedMethods = []string{
	"REGISTER", "MESSAGE", "INVITE", "ACK", "BYE", "OPTIONS", "SUBSCRIBE", "INFO",
}

// httpDate renders a Date header the way SIP expects it: GMT, in the
// IMF-fixdate form RFC 3261 borrows from HTTP.
func httpDate(t time.Time) string {
	return t.UTC().Format("Mon, 02 Jan 2006 15:04:05 GMT")
}

// allowHeader returns the Allow header the Acceptor advertises in
// non-REGISTER responses. The full set covers everything a child
// platform, a parent registrar, or a peer MANSCDP+ sender might send.
func allowHeader() model.Header {
	return model.NewHeader("Allow", strings.Join(allowedMethods, ", "))
}

// deviceAllowHeader returns the Allow header for a device node, which only
// accepts MESSAGE.
func deviceAllowHeader() model.Header {
	return model.NewHeader("Allow", "MESSAGE")
}

// buildResponse echoes the transaction headers a response must carry and
// adds the ones the answer itself needs. Via, From, To (tagged), Call-ID
// and CSeq are what let the peer match the answer to its transaction. body
// is the message body, "" for answers that carry none.
func buildResponse(
	status int,
	reason string,
	req model.Message,
	extra []model.Header,
	body string,
) (model.Message, error) {
	hdrs := make([]model.Header, 0, 6+len(extra))
	// RFC 3261 §8.2.6: a response copies every Via the request carried, in
	// order. A relayed request carries one Via per hop, and dropping all but
	// the first would leave the last relay with nothing to strip.
	for _, h := range req.Headers() {
		if strings.EqualFold(h.Name(), "Via") {
			hdrs = append(hdrs, h)
		}
	}
	for _, name := range []string{"From", "To", "Call-ID", "CSeq"} {
		h, ok := req.Header(name)
		if !ok {
			continue
		}
		if name == "To" && !strings.Contains(strings.ToLower(h.Value()), "tag=") {
			hdrs = append(hdrs, model.NewHeader("To", h.Value()+";tag="+randomTag()))
			continue
		}
		hdrs = append(hdrs, h)
	}
	hdrs = append(hdrs, extra...)
	return model.NewResponse(status, reason, hdrs, body)
}

// withExtraHeaders returns a copy of msg that carries the extra headers
// appended after the existing ones. The message kind (request vs response)
// is preserved.
func withExtraHeaders(msg model.Message, extra []model.Header) (model.Message, error) {
	if len(extra) == 0 {
		return msg, nil
	}
	all := make([]model.Header, 0, len(msg.Headers())+len(extra))
	all = append(all, msg.Headers()...)
	all = append(all, extra...)
	if msg.StatusCode() != 0 {
		return model.NewResponse(msg.StatusCode(), msg.StatusText(), all, msg.Body())
	}
	uri := ""
	if u := msg.URI(); u != nil {
		uri = u.String()
	}
	return model.NewRequest(msg.Method(), uri, all, msg.Body())
}

// headerValue returns the value of a header by name, or "" when absent.
func headerValue(req model.Message, name string) string {
	h, ok := req.Header(name)
	if !ok {
		return ""
	}
	return h.Value()
}

// requestUser returns the user part of the request's From URI — the device
// id a GB/T 28181 device both calls itself and authenticates as. It falls
// back to To, and to "" when neither carries one.
func requestUser(req model.Message) string {
	for _, name := range []string{"From", "To"} {
		h, ok := req.Header(name)
		if !ok {
			continue
		}
		if user := sipUser(h.Value()); user != "" {
			return user
		}
	}
	return ""
}

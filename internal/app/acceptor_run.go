package app

import (
	"context"

	"github.com/your-org/gb28181-simulator/internal/domain/model"
)

// routeOutbound consults the cascade handler before a message leaves the node.
// The destination is always the original peer: responses go back to the
// requester and NOTIFYs go to the subscriber, so the handler's next-hop advice
// only decides which cascade headers to attach. On a handler error (e.g. a
// loop detected) the message is dropped rather than forwarded further, as
// required by the loop-prevention spec for simulation mode.
func (a *Acceptor) routeOutbound(p *platform, msg model.Message, peer string) (model.Message, string, bool) {
	if a.cascade == nil {
		return msg, peer, true
	}
	_, extra, _, err := a.cascade.Forward(p.id, msg, peer)
	if err != nil {
		a.log.Warn("cascade forward rejected the message; dropping",
			"node_id", p.id.String(), "peer", peer, "error", err.Error())
		return model.Message{}, peer, false
	}
	if len(extra) == 0 {
		return msg, peer, true
	}
	out, bErr := withExtraHeaders(msg, extra)
	if bErr != nil {
		a.log.Warn("cascade headers rejected; sending direct",
			"node_id", p.id.String(), "peer", peer, "error", bErr.Error())
		return msg, peer, true
	}
	return out, peer, true
}

// run is one node's serving loop. It reads until the transport or the
// context ends; every REGISTER and every understood MESSAGE gets exactly
// one answer.
//
// Besides serving it runs the sweeper that throws out devices whose granted
// lifetime lapsed. The sweeper is a goroutine of its own because reading
// from the transport blocks, and it is joined before run returns so a
// stopped node owns no goroutine and touches no table.
func (a *Acceptor) run(ctx context.Context, p *platform) {
	defer close(p.done)
	// Leaving the loop for any reason — context cancelled, transport
	// closed, read failed — must end the sweeper with it.
	defer p.cancel()
	swept := make(chan struct{})
	go func() {
		defer close(swept)
		a.sweepLoop(ctx, p)
	}()
	defer func() { <-swept }()
	// The catalog poller shares the node's context so a stopped node leaves
	// no polling goroutine behind. It only sends; the read loop below answers
	// the device's query and the device's catalog Response is dispatched by
	// handleMessage once it arrives.
	if p.catalogPoller != nil {
		p.catalogPoller.start(ctx)
		defer p.catalogPoller.stop()
	}

	for {
		msg, peer, err := p.tr.Receive(ctx)
		if err != nil {
			if ctx.Err() == nil {
				a.log.Debug("platform stopped receiving",
					"node_id", p.id.String(), "error", err.Error())
			}
			return
		}
		if !msg.IsRequest() {
			// A relayed answer comes home first: it belongs to a request this
			// node handed to a cascade neighbour and must reach whoever asked,
			// not the local catalog logic.
			if a.relay != nil {
				if entry, ok := a.relay.take(p.id, msg); ok {
					a.forwardRelayResponse(ctx, p, msg, entry)
					continue
				}

			}
			// A platform sends no unprompted SIP requests; any Response that
			// arrives here is the device's answer to a catalog Query we sent.
			// Only a 2xx means the device answered the catalog successfully.
			if msg.StatusCode() >= 200 && msg.StatusCode() < 300 {
				a.handleCatalogResponse(ctx, p, msg)
			}
			continue
		}
		// Fault gate (Change 13, design D6): consult the node's armed
		// fault profile before any handler runs. A skip swallows the
		// request, an answer short-circuits the dispatch with the canned
		// response, and a serve verdict falls through to the normal
		// handlers unchanged. With no profile the byte path is identical.
		var resp model.Message
		handled := false
		if a.faults != nil {
			canned, decision := a.faultGate(p, msg, peer)
			switch decision {
			case faultSkip:
				continue
			case faultAnswer:
				resp = canned
				handled = true
			}
		}
		// The dispatch below has three shapes, in this order:
		//
		//   1. the fault gate already produced an answer — send it as-is;
		//   2. a device node asked for something other than MESSAGE —
		//      refuse with 405 and an Allow that names what it does take;
		//   3. everything else goes to the method switch below, byte for
		//      byte as it was before device serving existed.
		//
		// The device gate sits after the fault gate on purpose: an armed
		// fault profile must be able to swallow or answer any request,
		// device nodes included, so a device can still be driven into a
		// fault for testing. It also sits after the Response branch above,
		// which needs no gate — a device issues no unprompted requests, so
		// the only Response it can meet is to something it asked for.
		switch {
		case handled:
			// The fault gate returned a canned response; nothing else runs.
		case p.isDevice && msg.Method() != "MESSAGE":
			// A device is a UAS that answers one thing: the catalog query
			// MESSAGE an upstream sends to learn its channels and their
			// MediaSource extension. Everything else is refused loudly
			// rather than dropped, so a probing peer sees the node is
			// alive and learns which method it must actually use — a silent
			// drop would read as an unreachable device.
			//
			// ACK is the one exception, and it is not a simplification: SIP
			// forbids a UAS from answering an ACK (RFC 3261 §13.2.2.4), and
			// a 405 to it would be a protocol error the peer cannot parse
			// as a transaction outcome. ACK is acknowledged by its silence.
			if msg.Method() == "ACK" {
				a.log.Debug("dropping an ACK addressed to a device node",
					"node_id", p.id.String(), "peer", peer)
				continue
			}
			resp, _ = buildResponse(405, "Method Not Allowed", msg,
				[]model.Header{deviceAllowHeader()}, "")
			a.log.Debug("device node refuses a method",
				"node_id", p.id.String(), "peer", peer,
				"method", msg.Method())
		default:
			switch msg.Method() {
			case "REGISTER":
				resp, err = a.handle(ctx, p, msg, peer)
				if err != nil {
					a.log.Error("cannot answer a REGISTER",
						"node_id", p.id.String(), "peer", peer, "error", err.Error())
					continue
				}
			case "MESSAGE":
				// A request aimed at a cascade child belongs to the child, not
				// to this node's own command dispatch.
				if a.relayToChild(ctx, p, msg, peer) {
					continue
				}
				var answered bool
				resp, answered = a.handleMessage(ctx, p, msg, peer)
				if !answered {
					continue
				}
			case "INVITE":
				resp, err = a.handleInvite(ctx, p, msg, peer)
				if err != nil {
					a.log.Warn("cannot answer an INVITE",
						"node_id", p.id.String(), "peer", peer, "error", err.Error())
					continue
				}
			case "ACK":
				a.handleAck(ctx, p, msg, peer)
				continue
			case "BYE":
				resp, err = a.handleBye(ctx, p, msg, peer)
				if err != nil {
					a.log.Warn("cannot answer a BYE",
						"node_id", p.id.String(), "peer", peer, "error", err.Error())
					continue
				}
			case "OPTIONS":
				resp, err = a.handleOptions(ctx, p, msg, peer)
				if err != nil {
					a.log.Warn("cannot answer OPTIONS",
						"node_id", p.id.String(), "peer", peer, "error", err.Error())
					continue
				}
			case "SUBSCRIBE":
				resp, err = a.handleSubscribe(ctx, p, msg, peer)
				if err != nil {
					a.log.Warn("cannot answer a SUBSCRIBE",
						"node_id", p.id.String(), "peer", peer, "error", err.Error())
					continue
				}
			case "INFO":
				resp, err = a.handleInfo(ctx, p, msg, peer)
				if err != nil {
					a.log.Warn("cannot answer an INFO",
						"node_id", p.id.String(), "peer", peer, "error", err.Error())
					continue
				}
			default:
				// An unsupported method gets a 405 with Allow — RFC 3261 §8.2.2.
				// The fault profile may override this with a custom status (task 6).
				canned, answer := a.faultUnsupported(p, msg)
				if !answer {
					resp, _ = buildResponse(405, "Method Not Allowed", msg,
						[]model.Header{allowHeader()}, "")
				} else {
					resp = canned
				}
			}
		}
		resp, dst, ok := a.routeOutbound(p, resp, peer)
		var sendErr error
		if ok {
			sendErr = p.tr.Send(ctx, resp, dst)
		}
		if sendErr != nil {
			a.log.Warn("cannot send the answer",
				"node_id", p.id.String(), "peer", dst,
				"method", msg.Method(), "error", sendErr.Error())
		}
		// Flush the initial SUBSCRIBE NOTIFY after the 200 OK so the client
		// receives responses in the order the RFC requires.
		if msg.Method() == "SUBSCRIBE" && p.pendingNotify != nil {
			a.flushPendingNotify(p)
		}
	}
}

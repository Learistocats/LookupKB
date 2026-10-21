// PAT pass-through for the MCP endpoint (LOO-37; the API side lands with LOO-59).
// Forwards to the Go API with only a `Bearer lkb_pat_…` credential and streams
// the response (SSE included) back. See $lib/server/patProxy for the rules.
import type { RequestHandler } from './$types';
import { forwardPatRequest } from '$lib/server/patForward';

export const fallback: RequestHandler = (event) => forwardPatRequest(event);

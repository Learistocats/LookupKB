// Liveness probe for the web (BFF) container. Unauthenticated by design (see the
// exemption in hooks.server.ts) so the Docker healthcheck works before/without a
// session. This reports only that the Node server is up — it does not touch the
// API.
import { json, type RequestHandler } from '@sveltejs/kit';

export const GET: RequestHandler = () => json({ status: 'ok', service: 'lookupkb-web' });

// The editor moved from /drafts/{id} to /entries/{id}; keep old links working.
import { redirect, type RequestHandler } from '@sveltejs/kit';

export const GET: RequestHandler = ({ params }) => redirect(308, `/entries/${params.id}`);

import { redirect, type RequestHandler } from '@sveltejs/kit';
import { clearSessionCookie } from '$lib/server/session';

// The API holds no session, so dropping the cookie is the whole logout. POST only.
export const POST: RequestHandler = ({ cookies }) => {
	clearSessionCookie(cookies);
	redirect(302, '/login');
};

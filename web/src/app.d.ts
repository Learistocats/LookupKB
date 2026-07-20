// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces.
declare global {
	namespace App {
		// interface Error {}
		interface Locals {
			/**
			 * The verified signed-in user for this request, populated by
			 * hooks.server.ts from the encrypted session cookie. Undefined for
			 * unauthenticated requests (e.g. the /auth/* handshake routes).
			 */
			user?: {
				sub: string;
				email: string;
				groups: string[];
			};
			/**
			 * The verified access token for this request, for server-side proxying
			 * to the Go API. Never exposed to client-side code.
			 */
			accessToken?: string;
		}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};

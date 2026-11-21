import { paraglideMiddleware } from '#lib/paraglide/server.js';
import type { Handle } from '@sveltejs/kit/hooks';

export const handle: Handle = ({ event, resolve }) =>
	paraglideMiddleware(event.request, ({ request }) => resolve({ ...event, request }));
